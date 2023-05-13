package logging

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

type ctxKey string

const requestIDKey ctxKey = "request_id"

// New creates a new zerolog.Logger writing JSON to the given writer.
func New(w io.Writer) zerolog.Logger {
	if w == nil {
		w = os.Stdout
	}
	return zerolog.New(w).With().Timestamp().Logger()
}

// WithRequestID adds a request ID to the context.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID extracts the request ID from context.
func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

// LogRequest logs an HTTP request with standard fields.
func LogRequest(logger zerolog.Logger, ctx context.Context, method, path string, status int, size int64, latency time.Duration) {
	logger.Info().
		Str("request_id", RequestID(ctx)).
		Str("method", method).
		Str("path", path).
		Int("status", status).
		Int64("size", size).
		Dur("latency", latency).
		Msg("request")
}
