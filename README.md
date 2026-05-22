# backend

The FileSalad API in **Go + Gin + MongoDB + Redis**, with presigned uploads to
S3-compatible object storage (Tigris by default). Turns a file into a public
link: the backend checks quota and signs a URL; the client uploads bytes
**directly to storage** — the backend never touches the bytes.

## Stack

- **Go 1.25** (the toolchain auto-installs via `go.mod` — Gin v1.12 requires it).
- **Gin** for routing/middleware.
- **MongoDB** (`mongo-driver`) — users, uploads, and the monthly quota counter.
- **Redis** (`go-redis`) — refresh-token sessions (with rotation) and the
  presigned download-URL cache.
- **S3-compatible storage** (`aws-sdk-go-v2`) — provider-neutral; Tigris, R2,
  MinIO, or S3 by env. Presigning is local (no network call).
- **slog** for structured logging. Request-scoped fields (`requestId`, `userId`,
  `role`) ride on `context.Context`; a custom handler folds them into every line
  and redacts sensitive keys.
- **Errors are returned, not thrown.** Handlers attach an `*apperror.Error` with
  `c.Error(err)`; a central middleware translates it to the response envelope.

## Features

- **Auth (desktop):** email+password register/login, argon2id hashing, JWT
  access tokens, Redis-backed refresh sessions with rotation + reuse rejection,
  `GET /me`.
- **Hosted uploads (desktop, authenticated):** presign → client PUT → complete,
  cursor-paginated list, presigned download. Monthly cap + per-file size limit
  enforced server-side.
- **Web uploads (anonymous one-pager):** same presign path, no account, capped
  per browser by IP + `X-Fingerprint`.
- **Share codes:** mint a short (7-char), human-shareable code for an uploaded
  file that anyone can redeem for 24h; redeem is rate-limited per IP.

See [../docs/api-docs.md](../docs/api-docs.md) for the full API reference and
[../docs/qa-handoff.md](../docs/qa-handoff.md) for the QA scenario matrix.

## Structure

```
cmd/server/main.go        entry: load env, connect Mongo + Redis, wire, serve, graceful shutdown
internal/
  app/                    Gin engine composition (middleware + feature wiring)
  env/                    env loading + validation, fail-fast at boot
  logger/                 slog + request-scoped context fields + redaction
  httpx/                  error codes + response envelope types (shared vocab)
  apperror/               domain error type (code, status, field errors)
  response/               envelope writers
  ids/                    prefixed-ULID + opaque id generation
  db/                     MongoDB connect / ping / disconnect
  redis/                  Redis connect / ping / close
  security/               argon2 password, JWT, refresh-token hashing
  session/                Redis refresh-token sessions (rotation, revoke-all)
  storage/                S3-compatible presign (PUT/GET) + Level-1 download cache
  quota/                  atomic monthly upload counter (server-side source of truth)
  sharecode/              short shareable codes in Redis (SETNX + TTL)
  stats/                  background "total uploads" counter
  middleware/             requestID, requestLog, errorHandler, requireAuth, rateLimit
  features/
    health/               GET /api/v1/health
    auth/                 register / login / refresh / logout / me
    uploads/              hosted presign / complete / list / download
    webuploads/           anonymous presign / download / usage / stats
    share/                mint share code / redeem code (rate-limited)
```

Each feature exposes a `Register(rg *gin.RouterGroup, ...)` function; `app` wires
them in. A new feature is a new package plus one line in `app`.

> **Feature mounting:** auth needs Mongo + Redis; uploads need those + storage
> configured. Missing deps in dev simply skip that feature (its routes 404), so
> the server still boots.

## Getting started

```bash
cp .env.example .env        # fill JWT_* (min 32 chars); STORAGE_* for uploads
go run ./cmd/server
```

The server loads `.env` automatically. Generate secrets with `openssl rand -hex 32`.
Needs MongoDB + Redis (e.g. `brew services start mongodb-community redis`).

## Commands

```bash
go run ./cmd/server                 # run
go build -o bin/server ./cmd/server # build a static binary
go test ./...                       # unit tests
go test -tags=integration ./...     # + integration tests (need MongoDB)
./cover.sh                          # coverage summary (≥90%)
./cover.sh html                     # + write coverage.html
go vet ./... && gofmt -l .          # static checks
```

## Testing

- **Unit tests** are colocated (`*_test.go`); pure logic, no infra. Redis is
  faked in-process with `miniredis`.
- **Integration tests** (`//go:build integration`) drive the full Gin engine via
  `httptest` against a real **MongoDB** (the quota counter's atomic reserve and
  unique indexes need real Mongo). They **skip** cleanly if Mongo is unreachable.
- `./cover.sh` uses `-coverpkg` so the HTTP-level integration tests count toward
  the feature packages they exercise. Current total: **~90%**.

## Deploy

A single static binary — build it, set env vars, run it:

```bash
go build -o server ./cmd/server
./server            # reads config from real environment variables
```

There's no `.env` in production; set the variables through your platform.

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
| `REDIS_URL` | **yes** | — | e.g. `redis://localhost:6379` |
| `STORAGE_ENDPOINT` | for uploads | — | S3 API base, e.g. `https://t3.storage.dev` |
| `STORAGE_REGION` | no | `auto` | |
| `STORAGE_ACCESS_KEY_ID` | for uploads | — | |
| `STORAGE_SECRET_ACCESS_KEY` | for uploads | — | |
| `STORAGE_BUCKET` | for uploads | — | |
| `STORAGE_PUBLIC_BASE_URL` | for uploads | — | public read base, e.g. `https://<bucket>.t3.storage.dev` |
| `STORAGE_USE_PATH_STYLE` | no | `false` | force path-style addressing |
| `UPLOAD_URL_TTL` | no | `15m` | presigned PUT validity |
| `DOWNLOAD_URL_TTL` | no | `1h` | presigned GET validity |
| `MONTHLY_UPLOAD_CAP` | no | `50` | hosted + web per-month cap |
| `MAX_FILE_SIZE_BYTES` | no | `52428800` | 50 MB per-file limit |
| `HOSTED_LINK_EXPIRY_DAYS` | no | `90` | public link lifetime |
| `SHARE_CODE_TTL` | no | `24h` | how long a shareable code stays valid |

## Endpoints

| Method | Path | Auth | Notes |
|---|---|---|---|
| GET | `/api/v1/health` | — | Liveness |
| POST | `/api/v1/auth/register` | — | Create account → tokens |
| POST | `/api/v1/auth/login` | — | → tokens |
| POST | `/api/v1/auth/refresh` | — | Rotate refresh token |
| POST | `/api/v1/auth/logout` | — | Revoke session (204) |
| GET | `/api/v1/me` | Bearer | Current user |
| POST | `/api/v1/uploads/presign` | Bearer | Presigned PUT + quota |
| POST | `/api/v1/uploads/:id/complete` | Bearer | Mark ready |
| GET | `/api/v1/uploads` | Bearer | Cursor-paginated list |
| GET | `/api/v1/uploads/:id/download` | Bearer | Presigned GET |
| POST | `/api/v1/web/uploads/presign` | — (`X-Fingerprint`) | Anonymous presign + cap |
| GET | `/api/v1/web/uploads/:id/download` | — | Presigned GET |
| GET | `/api/v1/web/usage` | — (`X-Fingerprint`) | Cap usage |
| POST | `/api/v1/share` | — | Mint a 24h share code from an `upload_id` |
| GET | `/api/v1/share/:code` | — | Redeem a code → presigned GET (rate-limited) |
