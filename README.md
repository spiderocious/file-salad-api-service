# backend

The FileSalad API in **Go + Gin + MongoDB**.

## Stack

- **Go 1.25** (the toolchain auto-installs via `go.mod` — Gin v1.12 requires it).
- **Gin** for routing/middleware.
- **MongoDB** via the official `mongo-driver`.
- **slog** for structured logging. Request-scoped fields (`requestId`, `userId`,
  `role`) ride on `context.Context`, and a custom slog handler folds them into
  every line and redacts sensitive keys.
- **Errors are returned, not thrown.** Handlers attach an `*apperror.Error` with
  `c.Error(err)`; a central middleware translates it to the response envelope.
- The `auth` endpoints are **stubs** (validate body → return stub tokens) — the
  feature shape is in place, ready to grow into the real implementation.

## Structure

```
cmd/server/main.go        entry: load env, connect Mongo, serve, graceful shutdown
internal/
  app/                    Gin engine composition (middleware + feature wiring)
  env/                    env loading + validation, fail-fast at boot
  logger/                 slog setup + request-scoped context fields + redaction
  httpx/                  error codes + response envelope types (shared vocab)
  apperror/               domain error type (code, status, field errors)
  response/               envelope writers
  ids/                    opaque random id generation
  db/                     MongoDB connect / ping / disconnect
  middleware/             requestID, requestLog, errorHandler
  features/
    health/               GET /api/v1/health
    auth/                 stub register / login / refresh / logout
```

Each feature exposes a `Register(rg *gin.RouterGroup, ...)` function; `app` calls
each in order. A new feature is a new package plus one line in `app`.

## Commands

```bash
go run ./cmd/server                 # run
