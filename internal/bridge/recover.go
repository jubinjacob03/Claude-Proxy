package bridge

import (
	"net/http"
	"runtime/debug"

	"claude-proxy/internal/logx"
)

func logPanic(name string, rec any) {
	logx.Error("[panic] %s: %v\n%s", name, rec, debug.Stack())
}

// recoverGoroutine contains a panic in a background goroutine so one bad stream
// can't take the whole process down. Defer it at the top of every goroutine.
func recoverGoroutine(name string) {
	if rec := recover(); rec != nil {
		logPanic(name, rec)
	}
}

// withRecover converts a panic in a handler into a 500 in the caller's dialect.
// If the response already started (a stream mid-flight) the status is left alone
// and the connection simply ends, which clients treat as a truncated stream.
func withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			logPanic(r.Method+" "+r.URL.Path, rec)
			if sw, ok := w.(*statusWriter); ok && sw.wrote {
				return
			}
			writeErrorForPath(w, r.URL.Path, http.StatusInternalServerError, "api_error", "internal error")
		}()
		next.ServeHTTP(w, r)
	})
}
