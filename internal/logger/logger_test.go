package logger

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"trace": slog.LevelDebug,
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"weird": slog.LevelInfo, // default
	}
	for in, want := range cases {
		if got := parseLevel(in); got != want {
			t.Errorf("parseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestWithFieldsRoundTrip(t *testing.T) {
	ctx := WithFields(context.Background(), Fields{RequestID: "r1", UserID: "u1", Role: "admin"})
	f, ok := fieldsFrom(ctx)
	if !ok || f.RequestID != "r1" || f.UserID != "u1" || f.Role != "admin" {
		t.Fatalf("fields round-trip failed: %+v ok=%v", f, ok)
	}
	// Empty context → no fields.
	if _, ok := fieldsFrom(context.Background()); ok {
		t.Fatal("expected no fields on bare context")
	}
	if _, ok := fieldsFrom(nil); ok { //nolint
		t.Fatal("nil context should yield no fields")
	}
}

// captureHandler lets us assert on emitted records by pointing the package
// logger at a buffer through a custom handler wrapper.
func TestLoggerEmitsContextFieldsAndRedacts(t *testing.T) {
	var buf bytes.Buffer
	// Recreate what Init does, but to a buffer we can read.
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug, ReplaceAttr: redact})
	base = slog.New(contextHandler{inner}).With(slog.String("service", "test"))

	ctx := WithFields(context.Background(), Fields{RequestID: "req-9", UserID: "u-9", Role: "admin"})
	Info(ctx, "hello", "password", "should-be-gone", "kept", "yes")

	out := buf.String()
	// Request-scoped fields folded in by contextHandler.Handle.
	if !strings.Contains(out, "req-9") {
		t.Fatalf("requestId missing: %s", out)
	}
	if !strings.Contains(out, "u-9") || !strings.Contains(out, "admin") {
		t.Fatalf("userId/role missing: %s", out)
	}
	// Sensitive key redacted; non-sensitive kept.
	if strings.Contains(out, "should-be-gone") {
		t.Fatalf("password not redacted: %s", out)
	}
	if !strings.Contains(out, "kept") {
		t.Fatalf("non-sensitive field dropped: %s", out)
	}
}

func TestInitProductionAndDev(t *testing.T) {
	// Both branches should set a usable base logger without panicking.
	Init("info", "production")
	Info(context.Background(), "prod line")
	Init("debug", "development")
	Debug(context.Background(), "dev line")
	Warn(context.Background(), "warn line")
	Error(context.Background(), "err line")
}

func TestContextHandlerWithAttrsAndGroup(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug, ReplaceAttr: redact})
	// .With re-wraps via WithAttrs; .WithGroup via WithGroup — both must keep
	// the contextHandler so request fields still inject.
	l := slog.New(contextHandler{inner}).With(slog.String("svc", "x")).WithGroup("g")
	ctx := WithFields(context.Background(), Fields{RequestID: "rid"})
	l.InfoContext(ctx, "msg", slog.String("k", "v"))
	if !strings.Contains(buf.String(), "rid") {
		t.Fatalf("request field lost after With/WithGroup: %s", buf.String())
	}
}

func TestLoggerFallbackBeforeInit(t *testing.T) {
	base = nil // simulate logging before Init
	// Should lazily build a fallback, not panic.
	Info(context.Background(), "before init")
	if base == nil {
		t.Fatal("logger() should have set a fallback")
	}
}
