package server

import (
	"net/http"
	"runtime/debug"
	"time"

	"github.com/tiennm99/miti99bot/internal/log"
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
// CloudWatch Logs filters on `jsonPayload.msg=req AND jsonPayload.status>=500`
// for 5xx-rate alerting — keep the field names stable or the alarm goes dark.
//
// The req line is emitted from a deferred closure so a panic in a downstream
// handler still produces an observable log entry — without this, a cron
// panic would disappear silently (http.Server does its own recover but never
// runs middleware again on the way out).
func LogRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		defer func() {
			rec.status = recoverPanicStatus(recover(), rec.status)
			log.Info("req",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.effectiveStatus(),
				"ms", time.Since(start).Milliseconds(),
			)
		}()
		next.ServeHTTP(rec, r)
	})
}

// recoverPanicStatus folds a recovered panic into the status to log: returns
// 500 if a panic was recovered (and re-panics nothing — http.Server will
// terminate the connection cleanly while the deferred req log still runs),
// otherwise returns the original status untouched.
//
// Re-panicking would lose the deferred log line in some recover-order edge
// cases; absorbing the panic here matches the webhook handler's posture of
// "log the failure, keep the goroutine clean".
func recoverPanicStatus(rec any, currentStatus int) int {
	if rec == nil {
		return currentStatus
	}
	log.Error("middleware recovered panic",
		"panic", rec,
		"stack", string(debug.Stack()))
	return http.StatusInternalServerError
}
