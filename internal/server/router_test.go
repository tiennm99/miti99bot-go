package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tiennm99/miti99bot-go/internal/modules"
	"github.com/tiennm99/miti99bot-go/internal/storage"
)

const testCronSecret = "shared-cron-secret"

func buildRegistry(t *testing.T, factories map[string]modules.Factory, names ...string) *modules.Registry {
	t.Helper()
	reg, err := modules.Build(names, factories, modules.Deps{KV: storage.NewMemoryKVStore()})
	if err != nil {
		t.Fatalf("modules.Build: %v", err)
	}
	return reg
}

func TestHealthHandler_OK(t *testing.T) {
	rec := httptest.NewRecorder()
	HealthHandler()(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ok") {
		t.Errorf("body = %q, want contains 'ok'", rec.Body.String())
	}
}

func TestCronHandler_DisabledWhenSecretEmpty(t *testing.T) {
	reg := buildRegistry(t, nil)
	h := cronHandler(reg, "")

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/cron/anything", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (disabled)", rec.Code)
	}
}

func TestCronHandler_RejectsNonPost(t *testing.T) {
	reg := buildRegistry(t, nil)
	h := cronHandler(reg, testCronSecret)

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/cron/anything", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestCronHandler_RejectsMissingAuth(t *testing.T) {
	reg := buildRegistry(t, nil)
	h := cronHandler(reg, testCronSecret)

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/cron/anything", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestCronHandler_RejectsInvalidName(t *testing.T) {
	reg := buildRegistry(t, nil)
	h := cronHandler(reg, testCronSecret)

	// Use bare names that fail the regex; net/http already %-decodes the
	// path, so a smuggled \n on the wire would arrive here as a literal byte.
	cases := map[string]string{
		"uppercase":   "/cron/BadName",
		"hyphen":      "/cron/with-dash",
		"newline":     "/cron/with\nnewline",
		"empty":       "/cron/",
		"too long":    "/cron/abcdefghijklmnopqrstuvwxyzabcdefg", // 33 chars
		"path nested": "/cron/foo/bar",
	}
	for label, path := range cases {
		t.Run(label, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/cron/x", nil)
			req.URL.Path = path // bypass NewRequest's URL parser
			req.Header.Set(cronAuthHeader, testCronSecret)
			rec := httptest.NewRecorder()
			h(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Errorf("path %q: status = %d, want 404", path, rec.Code)
			}
		})
	}
}

func TestCronHandler_UnknownNameReturns404(t *testing.T) {
	reg := buildRegistry(t, nil)
	h := cronHandler(reg, testCronSecret)

	req := httptest.NewRequest(http.MethodPost, "/cron/missing", nil)
	req.Header.Set(cronAuthHeader, testCronSecret)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestCronHandler_RunsRegisteredCron(t *testing.T) {
	called := false
	factories := map[string]modules.Factory{
		"alpha": func(_ modules.Deps) modules.Module {
			return modules.Module{Crons: []modules.Cron{{
				Name: "tick",
				Handler: func(_ context.Context, _ modules.Deps) error {
					called = true
					return nil
				},
			}}}
		},
	}
	reg := buildRegistry(t, factories, "alpha")
	h := cronHandler(reg, testCronSecret)

	req := httptest.NewRequest(http.MethodPost, "/cron/tick", nil)
	req.Header.Set(cronAuthHeader, testCronSecret)
	rec := httptest.NewRecorder()
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !called {
		t.Error("cron handler not invoked")
	}
}
