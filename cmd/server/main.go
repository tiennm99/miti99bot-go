package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/tiennm99/miti99bot-go/internal/modules"
	"github.com/tiennm99/miti99bot-go/internal/modules/loldle"
	"github.com/tiennm99/miti99bot-go/internal/modules/loldleemoji"
	"github.com/tiennm99/miti99bot-go/internal/modules/misc"
	"github.com/tiennm99/miti99bot-go/internal/modules/util"
	"github.com/tiennm99/miti99bot-go/internal/modules/wordle"
	"github.com/tiennm99/miti99bot-go/internal/server"
	"github.com/tiennm99/miti99bot-go/internal/storage"
	"github.com/tiennm99/miti99bot-go/internal/telegram"
)

// secretEnvKeys are stripped from Deps.Env before any module sees it. Each
// new credential added to the environment must be appended here.
var secretEnvKeys = []string{
	"TELEGRAM_BOT_TOKEN",
	"TELEGRAM_WEBHOOK_SECRET",
	"CRON_SHARED_SECRET",
}

// factories is the static module catalog. Adding a new module is a one-line
// change here. Lives in main rather than the modules package to avoid an
// import cycle (modules → util → modules).
func factories() map[string]modules.Factory {
	return map[string]modules.Factory{
		"util":         util.New,
		"misc":         misc.New,
		"wordle":       wordle.New,
		"loldle":       loldle.New,
		"loldle-emoji": loldleemoji.New,
	}
}

// firestoreInitTimeout caps client construction at startup. Cloud Run cold
// start budget is 500ms target; firestore.NewClient is normally fast but
// network blips can make it hang. Fail fast and let Cloud Run restart us.
const firestoreInitTimeout = 10 * time.Second

func main() {
	cfg := loadConfig()
	if cfg.TelegramBotToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.WebhookSecret == "" {
		log.Fatal("TELEGRAM_WEBHOOK_SECRET is required (a non-empty secret is the only auth on /webhook)")
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	provider, closeProvider, err := buildProvider(rootCtx, cfg)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	defer closeProvider()

	b, err := telegram.NewBot(cfg.TelegramBotToken)
	if err != nil {
		log.Fatalf("telegram bot init: %v", err)
	}

	reg, err := modules.Build(cfg.Modules, factories(), provider, cfg.ModuleEnv)
	if err != nil {
		log.Fatalf("module registry: %v", err)
	}
	modules.Install(b, reg)
	log.Printf("loaded %d module(s), %d command(s), %d cron(s)",
		len(reg.Modules), len(reg.AllCommands), len(reg.Crons()))

	if cfg.CronSecret == "" {
		log.Println("WARN: CRON_SHARED_SECRET unset; /cron/{name} disabled (404 to all)")
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
		log.Printf("server listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-rootCtx.Done()
	log.Println("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown: %v", err)
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
		log.Println("WARN: GOOGLE_CLOUD_PROJECT unset; using in-memory KV (data lost on restart)")
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
			log.Printf("firestore close: %v", err)
		}
	}
	log.Printf("storage: Firestore project=%s emulator=%q", projectID, cfg.FirestoreEmulatorHost)
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
	ModuleEnv             map[string]string // sensitive keys stripped, safe to hand to modules
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
	return config{
		Port:                  port,
		TelegramBotToken:      envMap["TELEGRAM_BOT_TOKEN"],
		WebhookSecret:         envMap["TELEGRAM_WEBHOOK_SECRET"],
		CronSecret:            envMap["CRON_SHARED_SECRET"],
		GCPProject:            envMap["GOOGLE_CLOUD_PROJECT"],
		FirestoreEmulatorHost: envMap["FIRESTORE_EMULATOR_HOST"],
		Modules:               splitCSV(envMap["MODULES"]),
		ModuleEnv:              envForModules(envMap),
	}
}

func envForModules(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = v
	}
	for _, k := range secretEnvKeys {
		delete(out, k)
	}
	return out
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
