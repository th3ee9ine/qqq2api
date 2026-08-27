# Technology Stack

**Analysis Date:** 2026-08-23

Sub2API (module `github.com/th3ee9ine/qqq2api`) is an AI API gateway platform. Two-part monorepo: a Go backend (`backend/`) and a Vue 3 SPA (`frontend/`) that is compiled into the Go binary via `go:embed`.

## Languages

**Primary:**
- Go 1.26.6 - Entire backend: HTTP gateway, scheduler, billing, payments, admin API. Declared in `backend/go.mod:3`, pinned in CI at `.github/workflows/backend-ci.yml` ("Verify Go version" step greps `go1.26.6`).
- TypeScript ~5.6 - Frontend application code under `frontend/src/`. Config: `frontend/tsconfig.json`, `frontend/tsconfig.node.json`.

**Secondary:**
- Vue SFC (`.vue`) - Components and views under `frontend/src/components/`, `frontend/src/views/`, `frontend/src/features/`.
- SQL - 280 hand-written migration files in `backend/migrations/` (`001_init.sql` onward).
- Shell - Deployment and build scripts: `deploy/install.sh`, `deploy/docker-deploy.sh`, `deploy/docker-entrypoint.sh`, `backend/scripts/resolve-version.sh`, `backend/scripts/e2e-test.sh`.
- JavaScript (Node) - Admin CLI helper `skills/sub2api-admin/scripts/sub2api-admin.js`.

## Runtime

**Environment:**
- Backend: single static Go binary, `CGO_ENABLED=0`, built to `backend/bin/server` (see `backend/Makefile`). Runtime image is `alpine:3.21` (`Dockerfile`).
- Frontend: Node 24 Alpine at build time (`NODE_IMAGE=node:24-alpine` in `Dockerfile`); CI uses Node 20 (`.github/workflows/backend-ci.yml`). No Node at runtime — the built SPA is embedded in the Go binary.

**Package Manager:**
- Go modules. Lockfile: `backend/go.sum` (present).
- pnpm v9 (pinned via `corepack prepare pnpm@9` in `Dockerfile`, `pnpm/action-setup@v6` with `version: 9` in CI). Lockfile: `frontend/pnpm-lock.yaml` (present). Registry/hoisting settings in `frontend/.npmrc`.
- pnpm `overrides` pin security-sensitive transitive deps in `frontend/package.json:62`: `js-cookie@3.0.7`, `form-data>=4.0.6`, `postcss>=8.5.18`.

## Frameworks

**Core (backend):**
- `github.com/gin-gonic/gin` v1.9.1 - HTTP router and middleware. Engine wired in `backend/internal/server/router.go`; server bootstrap in `backend/internal/server/http.go`.
- `entgo.io/ent` v0.14.5 - Type-safe ORM. Generated client in `backend/ent/` (~250 files); schema definitions in `backend/ent/schema/` (39 entities including `account.go`, `api_key.go`, `usage_log.go`, `payment_order.go`, `subscription_plan.go`).
- `github.com/google/wire` v0.7.0 - Compile-time DI. Providers declared in `backend/internal/*/wire.go`; generated graph in `backend/cmd/server/wire_gen.go`.
- `github.com/spf13/viper` v1.18.2 - Config loading (YAML + env). See `backend/internal/config/config.go:1765`.
- `github.com/zeromicro/go-zero` v1.9.4 - Used for selected utilities alongside Gin (not as the primary framework).

**Core (frontend):**
- Vue 3 ^3.4 with `vue-router` ^4.2.5 and `pinia` ^2.1.7 (state).
- `vue-i18n` ^9.14.5 - Aliased to the runtime-only build with `__INTLIFY_JIT_COMPILATION__: true` in `frontend/vite.config.ts` to avoid CSP `unsafe-eval`.
- Tailwind CSS ^3.4 - `frontend/tailwind.config.js`, `frontend/postcss.config.js`.

**Testing:**
- Go stdlib `testing` + `github.com/stretchr/testify` v1.11.1. Build tags separate tiers: `unit`, `integration`, `e2e` (see `backend/Makefile`).
- `github.com/testcontainers/testcontainers-go` v0.40.0 with `modules/postgres` and `modules/redis` - real Postgres/Redis for integration tests.
- `github.com/alicebob/miniredis/v2` v2.38.0 - in-process Redis fake.
- `github.com/DATA-DOG/go-sqlmock` v1.5.2 - SQL-level mocking.
- Vitest ^2.1.9 with `jsdom` ^24.1.3 and `@vue/test-utils` ^2.4.6. Config: `frontend/vitest.config.ts`. Coverage via `@vitest/coverage-v8` with 80% global thresholds.

**Build/Dev:**
- Vite ^5.0.10 - `frontend/vite.config.ts`. Builds to `../backend/internal/web/dist` so the Go `embed` build tag can pick it up.
- `vue-tsc` ^2.2 / `vite-plugin-checker` ^0.9.1 - type checking during dev and build.
- ESLint ^8.57 with `@typescript-eslint` ^7.18 and `eslint-plugin-vue` ^9.25 - `frontend/.eslintrc.cjs`, `frontend/.eslintignore`.
- `golangci-lint` v2.9 - `backend/.golangci.yml`; run in CI via `golangci/golangci-lint-action@v9` with `--timeout=30m`.
- GoReleaser - `.goreleaser.yaml` and `.goreleaser.simple.yaml`; release image built from `Dockerfile.goreleaser`.
- Make - root `Makefile` (orchestrates both halves), `backend/Makefile`, `deploy/Makefile`.

## Key Dependencies

**Critical (gateway data path):**
- `github.com/imroc/req/v3` v3.59.0 - HTTP client for upstream AI providers; supports HTTP/2 and custom TLS.
- `github.com/refraction-networking/utls` v1.8.2 - TLS fingerprint impersonation. Wrapper in `backend/internal/pkg/tlsfingerprint/`; enabled by `gateway.tls_fingerprint` (default profile `claude_cli_v2`, mimics Node.js 20.x).
- `github.com/coder/websocket` v1.8.14 and `github.com/gorilla/websocket` v1.5.3 - WebSocket transports. The OpenAI Responses WS path lives in `backend/internal/service/openai_ws_v2/`.
- `github.com/tidwall/gjson` v1.18.0 / `github.com/tidwall/sjson` v1.2.5 - Surgical JSON read/rewrite on request and response bodies without full unmarshalling.
- `github.com/tiktoken-go/tokenizer` v0.8.0 - Token counting for billing and `count_tokens`.
- `github.com/shopspring/decimal` v1.4.0 - Exact decimal arithmetic for balances and billing.
- `github.com/andybalholm/brotli` v1.2.0 / `github.com/klauspost/compress` v1.18.2 - Response decompression on the proxy path.
- `github.com/quic-go/quic-go` v0.60.0 (indirect, via req) - HTTP/3 capability.

**Infrastructure:**
- `github.com/lib/pq` v1.10.9 - PostgreSQL driver; also used directly for `pq.Array` and error-code translation (`backend/internal/repository/error_translate.go`).
- `modernc.org/sqlite` v1.44.3 - Pure-Go SQLite (no CGO); used for local/test paths.
- `github.com/redis/go-redis/v9` v9.17.2 - Redis client (cache, distributed locks, session stickiness, concurrency slots).
- `github.com/dgraph-io/ristretto` v0.2.0 and `github.com/patrickmn/go-cache` v2.1.0 - In-process L1 caches.
- `github.com/alitto/pond/v2` v2.6.2 - Worker pools.
- `github.com/robfig/cron/v3` v3.0.1 - Scheduled jobs (token refresh, aggregation, cleanup).
- `go.uber.org/zap` v1.24.0 + `gopkg.in/natefinch/lumberjack.v2` v2.2.1 - Structured logging with rotation. Wrapper: `backend/internal/pkg/logger/`.
- `github.com/shirou/gopsutil/v4` v4.25.6 - Host metrics for the ops dashboard.
- `github.com/aws/aws-sdk-go-v2` v1.41.5 + `service/s3` v1.97.3 - S3-compatible object storage for async image results.

**Auth and crypto:**
- `github.com/golang-jwt/jwt/v5` v5.3.1 - Access tokens.
- `github.com/go-webauthn/webauthn` v0.17.4 - Passkey/WebAuthn.
- `github.com/pquerna/otp` v1.5.0 - TOTP 2FA.
- `golang.org/x/crypto` v0.53.0.

## Configuration

**Environment:**
- Primary source is a YAML file. Viper searches for `config.yaml`; the path can be overridden by `CONFIG_FILE`. Loader: `backend/internal/config/config.go:1765`. Reference template with inline docs: `deploy/config.example.yaml` (~1200 lines).
- `viper.AutomaticEnv()` with `SetEnvKeyReplacer(".", "_")` means every config key has an env override: `gateway.max_body_size` → `GATEWAY_MAX_BODY_SIZE`. Note the documented caveat at `backend/internal/config/config.go:2539` — `AutomaticEnv` can only override keys already registered via `SetDefault`, the config file, or explicit `BindEnv`.
- Explicit `BindEnv` bindings: `SERVER_TRUSTED_PROXIES`, `SECURITY_FORWARDED_CLIENT_IP_HEADERS` (`config.go:2576`), `ENABLE_SERVER_TIMING` (`config.go:1776`).
- Config struct root: `type Config struct` in `backend/internal/config/config.go`, with 39 sections (`server`, `gateway`, `database`, `redis`, `jwt`, `billing`, `pricing`, `image_storage`, and the OAuth/SSO providers).
- Ignored by git: `.env*`, `backend/config.yaml`, `deploy/config.yaml` (`.gitignore:51`, `:111`). No `.env` file is present in the repo.
- A large share of runtime configuration is **not** in the config file — it lives in the `settings` DB table and is edited from the admin UI. Keys are declared as `SettingKey*` constants across `backend/internal/service/setting_*.go`. This covers SMTP, captcha providers, GitHub/Google OAuth, feature flags, and moderation.

**Runtime env vars (read directly, outside viper):**
- `CONFIG_FILE`, `DATA_DIR`, `TZ`, `RUN_MODE`, `AUTO_SETUP`, `SKIP_SETUP`
- `GEMINI_CLI_OAUTH_CLIENT_SECRET`, `UPDATE_GITHUB_TOKEN`
- `WECHAT_OAUTH_MP_APP_ID` / `_SECRET`, `WECHAT_OAUTH_OPEN_APP_ID` / `_SECRET`, `WECHAT_OAUTH_FRONTEND_REDIRECT_URL`
- `YESCAPTCHA_API_KEY`, `YESCAPTCHA_CLIENT_KEY`
- Debug/test only: `SUB2API_DEBUG_CLAUDE_MIMIC`, `SUB2API_DEBUG_MODEL_ROUTING`, `CHANNEL_MONITOR_V2_DISABLE_AGGREGATOR`, `E2E_MOCK`, `TEST_REDIS_URL`, `SUB2API_TEST_POSTGRES_IMAGE`, `TLSFINGERPRINT_CAPTURE_URL`, `TLSFINGERPRINT_NETWORK_TESTS`

**Frontend build-time env:**
- `VITE_DEV_PROXY_TARGET` (default `http://localhost:8080`), `VITE_DEV_PORT` (default `3000`). Read in `frontend/vite.config.ts`.
- Public runtime settings are injected server-side as `window.__APP_CONFIG__`; `frontend/vite.config.ts` replicates this in dev by fetching `/api/v1/settings/public`.

**Build:**
- `Dockerfile` - 4-stage build (frontend → backend cross-compile → pg-client → alpine runtime). Cross-compiles with `--platform=$BUILDPLATFORM` to avoid QEMU; build args `VERSION`, `COMMIT`, `DATE`, `GOPROXY`, `GOSUMDB`, `NPM_CONFIG_REGISTRY`.
- Version resolution precedence: `-ldflags -X main.Version` > exact git tag (`backend/scripts/resolve-version.sh`) > embedded `backend/cmd/server/VERSION` (`backend/cmd/server/main.go:33`).
- `backend/internal/web/dist` is the embed target; the `embed` build tag switches between the real frontend server and a stub.

## Platform Requirements

**Development:**
- Go 1.26.6, Node 20+ with pnpm 9, PostgreSQL, Redis. Docker required for `make test-integration` (testcontainers) — the code reads `DOCKER_HOST` / `XDG_RUNTIME_DIR`.
- Build commands: `make build` (both halves), `make build-backend`, `make build-frontend`.
- Test commands: `make test`, `make test-backend`, `make test-frontend`. Frontend CI runs only a pinned critical subset (`FRONTEND_CRITICAL_VITEST` in the root `Makefile`), not the whole suite.
- Regenerate code after schema/DI changes: `make -C backend generate` (runs `go generate ./ent` and `go generate ./cmd/server`).
- Contributor guide: `DEV_GUIDE.md`.

**Production:**
- Docker Compose is the primary target: `deploy/docker-compose.yml` (app `ghcr.io/th3ee9ine/sub2api:latest`, `postgres:18-alpine`, `redis:8-alpine`). Variants: `.dev.yml`, `.local.yml`, `.standalone.yml`.
- Also supported: systemd (`deploy/sub2api.service`, `deploy/sub2api-datamanagementd.service`), Apple Container (`deploy/apple-container.sh`, `deploy/APPLE_CONTAINER.md`), Caddy reverse proxy (`deploy/Caddyfile`).
- Runs as non-root uid/gid 1000; data volume at `/app/data`; listens on 8080 (`SERVER_PORT`).
- Health check: `GET /health` (`backend/internal/server/routes/common.go`).
- The runtime image bundles `pg_dump` and `psql` copied from `postgres:18-alpine` so backup tooling matches the DB server version.
- Postgres is the supported production database. It is expected to tolerate high connection counts — `database.max_open_conns` defaults to 256 and `redis.pool_size` to 1024.

---

*Stack analysis: 2026-08-23*
