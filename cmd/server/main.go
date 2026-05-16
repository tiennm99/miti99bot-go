package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/tiennm99/miti99bot/internal/ai"
	"github.com/tiennm99/miti99bot/internal/log"
	"github.com/tiennm99/miti99bot/internal/metrics"
	"github.com/tiennm99/miti99bot/internal/modules"
	"github.com/tiennm99/miti99bot/internal/modules/loldle"
	"github.com/tiennm99/miti99bot/internal/modules/lolschedule"
	"github.com/tiennm99/miti99bot/internal/modules/misc"
	"github.com/tiennm99/miti99bot/internal/modules/trading"
	"github.com/tiennm99/miti99bot/internal/modules/twentyq"
	"github.com/tiennm99/miti99bot/internal/modules/util"
	"github.com/tiennm99/miti99bot/internal/modules/wordle"
	"github.com/tiennm99/miti99bot/internal/server"
	"github.com/tiennm99/miti99bot/internal/storage"
	"github.com/tiennm99/miti99bot/internal/telegram"
)

// factories is the static module catalog. Adding a new module is a one-line
// change here. Lives in main rather than the modules package to avoid an
// import cycle (modules → util → modules).
func factories() map[string]modules.Factory {
	return map[string]modules.Factory{
		"util":        util.New,
		"misc":        misc.New,
		"wordle":      wordle.New,
		"loldle":      loldle.New,
		"lolschedule": lolschedule.New,
		"twentyq":     twentyq.New,
		"trading":     trading.New,
	}
}

// firestoreInitTimeout caps Firestore client construction at startup. Cloud
// Run cold start budget is 500ms target; firestore.NewClient is normally fast
// but network blips can make it hang. Fail fast and let Lambda restart us.
const firestoreInitTimeout = 10 * time.Second

// dynamodbInitTimeout caps DynamoDB client construction at startup. Lambda
// has a 10s init phase; we want to leave headroom for module wiring.
const dynamodbInitTimeout = 5 * time.Second

// ssmInitTimeout caps cold-start secret resolution. Secrets are fetched once
// at startup from Parameter Store when *_PARAMETER_NAME env vars are set.
const ssmInitTimeout = 5 * time.Second

func main() {
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := loadConfig()
	if err := resolveSSMSecrets(rootCtx, &cfg); err != nil {
		log.Fatal("ssm secret resolution failed", "err", err)
	}
	if cfg.TelegramBotToken == "" {
		log.Fatal("missing required env", "key", "TELEGRAM_BOT_TOKEN")
	}
	if cfg.WebhookSecret == "" {
		log.Fatal("missing required env", "key", "TELEGRAM_WEBHOOK_SECRET",
			"why", "non-empty secret is the only auth on /webhook")
	}

	// Periodic metrics flush. Cancels with rootCtx and emits one final
	// flush on shutdown so the trailing window isn't lost.
	go metrics.Run(rootCtx)

	provider, closeProvider, err := buildProvider(rootCtx, cfg)
	if err != nil {
		log.Fatal("storage init failed", "err", err)
	}
	defer closeProvider()

	b, err := telegram.NewBot(cfg.TelegramBotToken)
	if err != nil {
		log.Fatal("telegram bot init failed", "err", err)
	}

	// Gemini is optional: twentyq checks for nil and refuses the command at
	// handler time. A blank GEMINI_API_KEY is therefore not fatal — the rest
	// of the bot still runs.
	aiClient, err := ai.NewClient(rootCtx, cfg.GeminiAPIKey)
	if err != nil && !errors.Is(err, ai.ErrNotConfigured) {
		log.Fatal("gemini init failed", "err", err)
	}
	if aiClient == nil {
		log.Warn("GEMINI_API_KEY unset; AI-backed modules will refuse commands")
	} else {
		log.Info("gemini client initialised")
	}

	reg, err := modules.Build(cfg.Modules, factories(), provider, modules.BuildOptions{
		Chatter: aiClient,
		Bot:     b,
	})
	if err != nil {
		log.Fatal("module registry build failed", "err", err)
	}
	auth := modules.Auth{BotOwnerID: cfg.BotOwnerID, AdminUserIDs: cfg.AdminUserIDs}
	modules.Install(b, reg, auth)
	log.Info("modules loaded",
		"modules", len(reg.Modules),
		"commands", len(reg.AllCommands),
		"crons", len(reg.Crons()))

	if cfg.BotOwnerID == 0 {
		log.Warn("BOT_OWNER_ID unset; all Private + Protected commands will be denied")
	}
	if cfg.CronSecret == "" {
		log.Warn("CRON_SHARED_SECRET unset; /cron/{name} disabled (404 to all)")
	}

	handler := server.New(server.Config{
		Bot:           b,
		Registry:      reg,
		WebhookSecret: cfg.WebhookSecret,
		CronSecret:    cfg.CronSecret,
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// 75s = cron handler cap (60s, internal/server/timeouts.go) plus a
		// 15s margin for response serialization. On Lambda the 30s function
		// timeout supersedes this; the tighter ceiling matters only for
		// local non-Lambda runs where a 6-minute slow-loris write was the
		// previous (over-generous) bound.
		WriteTimeout: 75 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Info("server listening", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("server crashed", "err", err)
		}
	}()

	<-rootCtx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "err", err)
	}
}

// buildProvider picks the storage backend. Selection order:
//  1. Explicit KV_PROVIDER env (memory|firestore|dynamodb) wins.
//  2. Auto-detect: AWS_LAMBDA_FUNCTION_NAME set → dynamodb; GOOGLE_CLOUD_PROJECT
//     or FIRESTORE_EMULATOR_HOST set → firestore; otherwise memory.
//
// Returned closer is always non-nil and safe to call exactly once.
func buildProvider(ctx context.Context, cfg config) (storage.KVProvider, func(), error) {
	backend := strings.ToLower(strings.TrimSpace(cfg.KVProvider))
	if backend == "" {
		switch {
		case os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != "":
			backend = "dynamodb"
		case cfg.FirestoreProject != "" || cfg.FirestoreEmulatorHost != "":
			backend = "firestore"
		default:
			backend = "memory"
		}
	}

	switch backend {
	case "memory":
		log.Warn("KV backend: in-memory (data lost on restart)")
		return storage.NewMemoryProvider(), func() {}, nil

	case "firestore":
		// Emulator ignores the project ID but the SDK still requires *some*
		// non-empty value; supply a placeholder so emulator-only local dev works.
		projectID := cfg.FirestoreProject
		if projectID == "" && cfg.FirestoreEmulatorHost != "" {
			projectID = "miti99bot-emulator"
		}
		initCtx, cancel := context.WithTimeout(ctx, firestoreInitTimeout)
		defer cancel()
		client, err := storage.NewFirestoreClient(initCtx, projectID)
		if err != nil {
			return nil, func() {}, err
		}
		closer := func() {
			if err := client.Close(); err != nil {
				log.Error("firestore close failed", "err", err)
			}
		}
		log.Info("storage backend",
			"backend", "firestore",
			"project", projectID,
			"emulator", cfg.FirestoreEmulatorHost)
		return storage.NewFirestoreProvider(client), closer, nil

	case "dynamodb":
		if cfg.DynamoDBTable == "" {
			return nil, func() {}, errors.New("KV_PROVIDER=dynamodb requires DYNAMODB_TABLE")
		}
		initCtx, cancel := context.WithTimeout(ctx, dynamodbInitTimeout)
		defer cancel()
		client, err := storage.NewDynamoDBClient(initCtx, storage.DynamoDBEndpointFromEnv())
		if err != nil {
			return nil, func() {}, err
		}
		log.Info("storage backend",
			"backend", "dynamodb",
			"table", cfg.DynamoDBTable,
			"endpoint_override", storage.DynamoDBEndpointFromEnv() != "")
		// DynamoDB SDK v2 client has no Close; HTTP client uses the default pool.
		return storage.NewDynamoDBProvider(client, cfg.DynamoDBTable), func() {}, nil

	default:
		return nil, func() {}, fmt.Errorf("unknown KV_PROVIDER %q (want memory|firestore|dynamodb)", backend)
	}
}

type config struct {
	Port                  string
	TelegramBotToken      string
	WebhookSecret         string
	CronSecret            string
	FirestoreProject      string
	FirestoreEmulatorHost string
	GeminiAPIKey          string
	Modules               []string
	BotOwnerID            int64
	AdminUserIDs          map[int64]bool
	KVProvider            string // empty = auto-detect; or "memory"|"firestore"|"dynamodb"
	DynamoDBTable         string // required when KVProvider=dynamodb
	TelegramBotTokenParam string
	WebhookSecretParam    string
	CronSecretParam       string
	GeminiAPIKeyParam     string
}

func loadConfig() config {
	envMap := make(map[string]string, len(os.Environ()))
	for _, kv := range os.Environ() {
		if eq := strings.IndexByte(kv, '='); eq >= 0 {
			envMap[kv[:eq]] = kv[eq+1:]
		}
	}
	port := envMap["PORT"]
	if port == "" {
		port = "8080"
	}
	// PORT must be numeric — http.Server constructs ":<port>" verbatim, so a
	// junk value would surface only at ListenAndServe time. Fail fast here
	// instead. Range check is delegated to http.Server (it handles 0/65535).
	if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
		log.Fatal("invalid PORT", "value", port)
	}
	return config{
		Port:                  port,
		TelegramBotToken:      envMap["TELEGRAM_BOT_TOKEN"],
		WebhookSecret:         envMap["TELEGRAM_WEBHOOK_SECRET"],
		CronSecret:            envMap["CRON_SHARED_SECRET"],
		FirestoreProject:      envMap["GOOGLE_CLOUD_PROJECT"],
		FirestoreEmulatorHost: envMap["FIRESTORE_EMULATOR_HOST"],
		GeminiAPIKey:          envMap["GEMINI_API_KEY"],
		Modules:               splitCSV(envMap["MODULES"]),
		BotOwnerID:            parseInt64(envMap["BOT_OWNER_ID"]),
		AdminUserIDs:          parseInt64Set(envMap["ADMIN_USER_IDS"]),
		KVProvider:            envMap["KV_PROVIDER"],
		DynamoDBTable:         envMap["DYNAMODB_TABLE"],
		TelegramBotTokenParam: strings.TrimSpace(envMap["TELEGRAM_BOT_TOKEN_PARAMETER_NAME"]),
		WebhookSecretParam:    strings.TrimSpace(envMap["TELEGRAM_WEBHOOK_SECRET_PARAMETER_NAME"]),
		CronSecretParam:       strings.TrimSpace(envMap["CRON_SHARED_SECRET_PARAMETER_NAME"]),
		GeminiAPIKeyParam:     strings.TrimSpace(envMap["GEMINI_API_KEY_PARAMETER_NAME"]),
	}
}

func resolveSSMSecrets(ctx context.Context, cfg *config) error {
	bindings := []struct {
		name   string
		target *string
	}{
		{name: cfg.TelegramBotTokenParam, target: &cfg.TelegramBotToken},
		{name: cfg.WebhookSecretParam, target: &cfg.WebhookSecret},
		{name: cfg.CronSecretParam, target: &cfg.CronSecret},
		{name: cfg.GeminiAPIKeyParam, target: &cfg.GeminiAPIKey},
	}

	targetsByName := map[string][]*string{}
	names := make([]string, 0, len(bindings))
	for _, b := range bindings {
		if b.name == "" || *b.target != "" {
			continue
		}
		if _, ok := targetsByName[b.name]; !ok {
			names = append(names, b.name)
		}
		targetsByName[b.name] = append(targetsByName[b.name], b.target)
	}
	if len(names) == 0 {
		return nil
	}

	initCtx, cancel := context.WithTimeout(ctx, ssmInitTimeout)
	defer cancel()

	awsCfg, err := awsconfig.LoadDefaultConfig(initCtx, awsconfig.WithHTTPClient(&http.Client{
		Timeout: ssmInitTimeout,
	}))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}
	client := ssm.NewFromConfig(awsCfg)
	out, err := client.GetParameters(initCtx, &ssm.GetParametersInput{
		Names:          names,
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("get parameters: %w", err)
	}
	if len(out.InvalidParameters) > 0 {
		return fmt.Errorf("missing SSM parameters: %s", strings.Join(out.InvalidParameters, ","))
	}
	for _, p := range out.Parameters {
		name := aws.ToString(p.Name)
		value := aws.ToString(p.Value)
		for _, target := range targetsByName[name] {
			*target = value
		}
	}
	log.Info("loaded secrets from ssm", "count", len(out.Parameters))
	return nil
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// parseInt64 returns 0 (the "unset" sentinel) when s is empty or invalid.
// Telegram user IDs are positive int64 so 0 is unambiguously "no value".
func parseInt64(s string) int64 {
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		log.Warn("invalid int64 in env", "value", s, "err", err)
		return 0
	}
	return n
}

// parseInt64Set parses a comma-separated list of int64 IDs into a set. Bad
// entries are logged and skipped — one malformed admin ID does not deny the
// rest.
func parseInt64Set(s string) map[int64]bool {
	if s == "" {
		return nil
	}
	out := map[int64]bool{}
	for _, p := range strings.Split(s, ",") {
		t := strings.TrimSpace(p)
		if t == "" {
			continue
		}
		n, err := strconv.ParseInt(t, 10, 64)
		if err != nil {
			log.Warn("invalid admin id", "value", t, "err", err)
			continue
		}
		out[n] = true
	}
	return out
}
