package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tiennm99/miti99bot-go/internal/log"
	"github.com/tiennm99/miti99bot-go/internal/metrics"
	"github.com/tiennm99/miti99bot-go/internal/modules"
	"github.com/tiennm99/miti99bot-go/internal/modules/loldle"
	"github.com/tiennm99/miti99bot-go/internal/modules/loldleability"
	"github.com/tiennm99/miti99bot-go/internal/modules/loldleemoji"
	"github.com/tiennm99/miti99bot-go/internal/modules/loldlequote"
	"github.com/tiennm99/miti99bot-go/internal/modules/loldlesplash"
	"github.com/tiennm99/miti99bot-go/internal/modules/lolschedule"
	"github.com/tiennm99/miti99bot-go/internal/modules/misc"
	"github.com/tiennm99/miti99bot-go/internal/modules/util"
	"github.com/tiennm99/miti99bot-go/internal/modules/wordle"
	"github.com/tiennm99/miti99bot-go/internal/server"
	"github.com/tiennm99/miti99bot-go/internal/storage"
	"github.com/tiennm99/miti99bot-go/internal/telegram"
)

// factories is the static module catalog. Adding a new module is a one-line
// change here. Lives in main rather than the modules package to avoid an
// import cycle (modules → util → modules).
func factories() map[string]modules.Factory {
	return map[string]modules.Factory{
		"util":         util.New,
		"misc":         misc.New,
		"wordle":       wordle.New,
		"loldle":         loldle.New,
		"loldle-ability": loldleability.New,
		"loldle-emoji":   loldleemoji.New,
		"loldle-quote":   loldlequote.New,
		"loldle-splash":  loldlesplash.New,
		"lolschedule":    lolschedule.New,
	}
}

// firestoreInitTimeout caps client construction at startup. Cloud Run cold
// start budget is 500ms target; firestore.NewClient is normally fast but
// network blips can make it hang. Fail fast and let Cloud Run restart us.
const firestoreInitTimeout = 10 * time.Second

func main() {
	cfg := loadConfig()
	if cfg.TelegramBotToken == "" {
		log.Fatal("missing required env", "key", "TELEGRAM_BOT_TOKEN")
	}
	if cfg.WebhookSecret == "" {
		log.Fatal("missing required env", "key", "TELEGRAM_WEBHOOK_SECRET",
			"why", "non-empty secret is the only auth on /webhook")
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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

	reg, err := modules.Build(cfg.Modules, factories(), provider, cfg.ModuleEnv)
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
		// 6 min accommodates /cron/{name}; the webhook handler enforces a
		// tighter per-update ctx timeout internally.
		WriteTimeout: 6 * time.Minute,
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

// buildProvider picks the storage backend from env. Firestore is selected
// when GOOGLE_CLOUD_PROJECT or FIRESTORE_EMULATOR_HOST is set; otherwise we
// fall back to in-memory storage so a developer can run the bot without GCP.
//
// Returned closer is always non-nil and safe to call exactly once.
func buildProvider(ctx context.Context, cfg config) (storage.KVProvider, func(), error) {
	useFirestore := cfg.GCPProject != "" || cfg.FirestoreEmulatorHost != ""
	if !useFirestore {
		log.Warn("GOOGLE_CLOUD_PROJECT unset; using in-memory KV (data lost on restart)")
		return storage.NewMemoryProvider(), func() {}, nil
	}

	// Emulator ignores the project ID but the SDK still requires *some*
	// non-empty value; supply a placeholder so emulator-only local dev works.
	projectID := cfg.GCPProject
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
}

type config struct {
	Port                  string
	TelegramBotToken      string
	WebhookSecret         string
	CronSecret            string
	GCPProject            string
	FirestoreEmulatorHost string
	Modules               []string
	BotOwnerID            int64
	AdminUserIDs          map[int64]bool
	ModuleEnv             map[string]string // empty — modules opt in via per-module allowlist (Phase 07+)
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
		GCPProject:            envMap["GOOGLE_CLOUD_PROJECT"],
		FirestoreEmulatorHost: envMap["FIRESTORE_EMULATOR_HOST"],
		Modules:               splitCSV(envMap["MODULES"]),
		BotOwnerID:            parseInt64(envMap["BOT_OWNER_ID"]),
		AdminUserIDs:          parseInt64Set(envMap["ADMIN_USER_IDS"]),
		ModuleEnv:              map[string]string{}, // allowlist semantics — process env does not auto-flow
	}
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
