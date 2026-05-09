package server

import (
	"net/http"
	"time"

	"github.com/tiennm99/miti99bot-go/internal/log"
)

// statusRecorder wraps http.ResponseWriter to capture the final status
// code. http.ResponseWriter doesn't expose what was written; the middleware
// needs the status to log a per-request `req` line.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// status returns the recorded status code, defaulting to 200 when no
// explicit WriteHeader was called (Go's net/http implicitly writes 200 on
// the first body write).
func (r *statusRecorder) effectiveStatus() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

// LogRequests wraps an http.Handler with a request log line:
//
//	{"msg":"req","method":"POST","path":"/webhook","status":200,"ms":12}
//
// Cloud Logging filters on `jsonPayload.msg=req AND jsonPayload.status>=500`
// for 5xx-rate alerting. Mirrors the JS source's index.js shape so existing
// dashboards keep working post-cutover.
func LogRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		log.Info("req",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.effectiveStatus(),
			"ms", time.Since(start).Milliseconds(),
		)
	})
}
