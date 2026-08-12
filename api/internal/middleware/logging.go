// Package middleware holds the Huma middleware the API runs on every operation.
package middleware

import (
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
)

// Logging logs one line per request and attaches a logger carrying the request id to the
// context, which handlers read with ctx.Value("logger").(*slog.Logger).
func Logging(base *slog.Logger) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		start := time.Now()

		logger := base.With("request_id", uuid.Must(uuid.NewV7()).String())
		ctx = huma.WithValue(ctx, "logger", logger)

		next(ctx)

		// nil, not "", so a request without a query string logs null rather than an empty field.
		var query any
		if raw := ctx.URL().RawQuery; raw != "" {
			query = raw
		}

		attrs := []slog.Attr{
			slog.String("method", ctx.Method()),
			slog.String("path", ctx.URL().Path),
			slog.Any("query", query),
			slog.Int("status", ctx.Status()),
			slog.Float64("duration_s", time.Since(start).Seconds()),
			slog.String("remote_addr", ctx.RemoteAddr()),
		}

		// One event name: the message is a grouping key, so the outcome belongs in level
		// and status. Only 5xx raises the level — every 4xx here is the API working
		// correctly, and feed-refresh churn would make Warn unreadable.
		level := slog.LevelInfo
		if ctx.Status() >= 500 {
			level = slog.LevelError
		}
		logger.LogAttrs(ctx.Context(), level, "http_request", attrs...)
	}
}
