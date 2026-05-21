// Package logger is the structured logger, built on slog. It mirrors the Node
// backend's pino setup: a base logger with service/env fields, PII-style
// redaction of sensitive keys, and request-scoped fields (requestId, userId,
// role) folded in automatically.
//
// Where the Node side uses AsyncLocalStorage to carry per-request context, Go's
// idiom is the explicit context.Context — so the request id and friends live on
// the context and a custom slog handler reads them on every log call. Use the
// package-level helpers (Info/Warn/Error...) and pass the request ctx.
package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// ctxKey is the private context key under which request-scoped log fields live.
type ctxKey struct{}

// Fields carried per-request and merged into every log line.
type Fields struct {
	RequestID string
	UserID    string
	Role      string
}

// WithFields returns a child context carrying the per-request log fields.
func WithFields(ctx context.Context, f Fields) context.Context {
	return context.WithValue(ctx, ctxKey{}, f)
}

func fieldsFrom(ctx context.Context) (Fields, bool) {
	if ctx == nil {
		return Fields{}, false
	}
	f, ok := ctx.Value(ctxKey{}).(Fields)
	return f, ok
}

// redactKeys are dropped from log output entirely (matches pino's redact list).
var redactKeys = map[string]struct{}{
	"authorization": {}, "cookie": {}, "password": {}, "token": {},
	"refresh_token": {}, "secret": {},
}

// contextHandler wraps a slog.Handler to inject request-scoped fields and redact
// sensitive attribute keys.
type contextHandler struct{ slog.Handler }

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if f, ok := fieldsFrom(ctx); ok {
		if f.RequestID != "" {
			r.AddAttrs(slog.String("requestId", f.RequestID))
		}
		if f.UserID != "" {
			r.AddAttrs(slog.String("userId", f.UserID))
		}
		if f.Role != "" {
			r.AddAttrs(slog.String("role", f.Role))
		}
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs and WithGroup must re-wrap so that .With(...) on the logger keeps
// the context-field injection. Without these, the embedded handler's methods
// return a bare handler and request-scoped fields are silently dropped.
func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}

func redact(_ []string, a slog.Attr) slog.Attr {
	if _, hit := redactKeys[strings.ToLower(a.Key)]; hit {
		return slog.Attr{}
	}
	return a
}

var base *slog.Logger

// Init configures the package logger. Call once at boot. In non-production it
// uses a readable text handler; in production, JSON.
func Init(level, nodeEnv string) {
	opts := &slog.HandlerOptions{Level: parseLevel(level), ReplaceAttr: redact}

	var inner slog.Handler
	if nodeEnv == "production" {
		inner = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		inner = slog.NewTextHandler(os.Stdout, opts)
	}

	base = slog.New(contextHandler{inner}).With(
		slog.String("service", "file-salad-backend"),
		slog.String("env", nodeEnv),
	)
}

func logger() *slog.Logger {
	if base == nil {
		// Safe fallback before Init (e.g. very early boot errors).
		base = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
	return base
}

func Debug(ctx context.Context, msg string, args ...any) { logger().DebugContext(ctx, msg, args...) }
func Info(ctx context.Context, msg string, args ...any)  { logger().InfoContext(ctx, msg, args...) }
func Warn(ctx context.Context, msg string, args ...any)  { logger().WarnContext(ctx, msg, args...) }
func Error(ctx context.Context, msg string, args ...any) { logger().ErrorContext(ctx, msg, args...) }

// parseLevel maps the env's level names onto slog levels. "trace" has no slog
// equivalent, so it maps to debug.
func parseLevel(level string) slog.Level {
	switch level {
	case "trace", "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
