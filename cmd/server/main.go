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

	b, err := telegram.NewBot(cfg.TelegramBotToken)
	if err != nil {
		log.Fatalf("telegram bot init: %v", err)
	}

	kv := storage.NewMemoryKVStore()
	deps := modules.Deps{KV: kv, Env: cfg.ModuleEnv}

	reg, err := modules.Build(cfg.Modules, modules.Factories, deps)
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

type config struct {
	Port             string
	TelegramBotToken string
	WebhookSecret    string
	CronSecret       string
	Modules          []string
	ModuleEnv        map[string]string // sensitive keys stripped, safe to hand to modules
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
		Port:             port,
		TelegramBotToken: envMap["TELEGRAM_BOT_TOKEN"],
		WebhookSecret:    envMap["TELEGRAM_WEBHOOK_SECRET"],
		CronSecret:       envMap["CRON_SHARED_SECRET"],
		Modules:          splitCSV(envMap["MODULES"]),
		ModuleEnv:        envForModules(envMap),
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
