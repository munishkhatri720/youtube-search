package main

import (
	"log/slog"
	"net/http"
	"time"
)

func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		slog.Info(
			"Incoming request",
			"method",
			r.Method,
			"url",
			r.URL.String(),
			"remote_addr",
			r.RemoteAddr,
		)
		next.ServeHTTP(w, r)
		duration := time.Since(startedAt)
		slog.Info(
			"Completed request",
			"method",
			r.Method,
			"url",
			r.URL.String(),
			"remote_addr",
			r.RemoteAddr,
			"duration_ms",
			duration.Milliseconds(),
		)
	})
}

func PanicRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("Recovered from panic in HTTP handler", "error", r)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})

}
