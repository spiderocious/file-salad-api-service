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

## Layout

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

## Getting started

```bash
cp .env.example .env        # then fill JWT_* secrets (min 32 chars each)
go run ./cmd/server
```

The server loads `.env` automatically on start, so there's nothing to source.
Generate secrets with `openssl rand -hex 32`.

MongoDB is optional in development: if it's unreachable the server logs a warning
and boots without it. In production a failed connection is fatal.

## Commands

```bash
go run ./cmd/server                 # run
go build -o bin/server ./cmd/server # build a binary
go vet ./...                        # static checks
go test ./...                       # tests
gofmt -w .                          # format
```

## Deploy

It's a single static binary — build it, set the env vars, run it:

```bash
go build -o server ./cmd/server
./server            # reads config from real environment variables
```

There's no `.env` in production; set the variables below through your platform.

## Environment

| Var | Required | Default | Notes |
|---|---|---|---|
| `NODE_ENV` | no | `development` | `development` \| `test` \| `production` |
| `PORT` | no | `8080` | HTTP listen port |
| `LOG_LEVEL` | no | `info` | `trace`\|`debug`\|`info`\|`warn`\|`error` |
| `JWT_ACCESS_SECRET` | **yes** | — | min 32 chars |
| `JWT_REFRESH_SECRET` | **yes** | — | min 32 chars |
| `JWT_ACCESS_EXPIRES_IN` | no | `15m` | |
| `JWT_REFRESH_EXPIRES_IN` | no | `30d` | |
| `WEB_BASE_URL` | no | `*` | CORS allow-origin (`*` = any) |
| `MONGODB_URI` | **yes** | — | e.g. `mongodb://localhost:27017` |
| `MONGODB_DB` | no | `file_salad` | database name |

## Endpoints (current scaffold)

| Method | Path | Notes |
|---|---|---|
| GET | `/api/v1/health` | Liveness — service, env, time. |
| POST | `/api/v1/auth/register` | Stub — validates body, returns stub tokens. |
| POST | `/api/v1/auth/login` | Stub — validates body, returns stub tokens. |
| POST | `/api/v1/auth/refresh` | Stub. |
| POST | `/api/v1/auth/logout` | Stub — 204. |
