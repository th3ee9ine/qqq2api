<!-- refreshed: 2026-08-23 -->
# Architecture

**Analysis Date:** 2026-08-23

## System Overview

```text
┌─────────────────────────────────────────────────────────────┐
│              Clients (Claude Code / Codex / SDKs / Browser)   │
├──────────────────┬──────────────────┬───────────────────────┤
│  LLM Gateway API │  Panel REST API  │   Embedded Vue SPA    │
│  `/v1`, `/v1beta`│  `/api/v1/...`   │  `internal/web/dist`  │
└────────┬─────────┴────────┬─────────┴──────────┬────────────┘
         │                  │                     │
         ▼                  ▼                     ▼
┌─────────────────────────────────────────────────────────────┐
│                Gin Engine + Middleware Chain                 │
│  `backend/internal/server/router.go`                         │
│  `backend/internal/server/middleware/`                       │
│  (Recovery, RequestLogger, SessionBinding, CORS, CSP,        │
│   APIKeyAuth / JWTAuth / AdminAuth / StepUp, AuditLog,       │
│   IngressReject, PanelRateLimit, BodyLimit)                  │
└────────┬────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│                        Handler layer                         │
│  `backend/internal/handler/`  (+ `handler/admin/`)           │
│  Request parse → validate → orchestrate → DTO → response     │
│  Gateway failover loop: `handler/failover_loop.go`           │
└────────┬────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│                    Service layer (core)                      │
│  `backend/internal/service/` (~490 non-test files)           │
│  Business rules, account scheduling, billing, quota,          │
│  upstream forwarding, background workers.                     │
│  Also OWNS all repository/port interfaces (ports).            │
└────────┬───────────────────────────────┬────────────────────┘
         │                               │
         ▼                               ▼
┌────────────────────────────┐  ┌──────────────────────────────┐
│    Repository layer         │  │      Upstream providers      │
│ `backend/internal/repository`│  │ Anthropic / OpenAI / Gemini  │
│ Ent ORM + raw SQL + Redis   │  │ Antigravity / Grok / Bedrock  │
│ caches; implements service  │  │ via `service.HTTPUpstream`    │
│ interfaces                  │  │ `pkg/httpclient`,             │
└────────┬───────────────────┘  │ `pkg/tlsfingerprint`          │
         │                       └──────────────────────────────┘
         ▼
┌─────────────────────────────────────────────────────────────┐
│  PostgreSQL 16 (Ent schema `backend/ent/schema/`,             │
│  SQL migrations `backend/migrations/*.sql`)                   │
│  Redis (cache, sessions, rate limits, queues, pub/sub)        │
└─────────────────────────────────────────────────────────────┘
```

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| Process entry | Flag parsing, setup-wizard vs. server mode, graceful shutdown | `backend/cmd/server/main.go` |
| DI graph | Wire-generated construction of the whole object graph | `backend/cmd/server/wire.go`, `backend/cmd/server/wire_gen.go` |
| HTTP server | `http.Server` config, h2c, body-size cap, trusted proxies | `backend/internal/server/http.go` |
| Router | Global middleware, embedded-SPA mount, route module registration | `backend/internal/server/router.go` |
| Route modules | Per-domain route trees (auth, user, admin, gateway, payment) | `backend/internal/server/routes/` |
| Middleware | Auth, audit, rate limit, CSP, ingress reject, session binding | `backend/internal/server/middleware/` |
| Handler aggregate | Struct holding every handler, injected into the router | `backend/internal/handler/handler.go` |
| LLM gateway handler | Anthropic-shaped request path, failover, streaming, usage | `backend/internal/handler/gateway_handler.go` |
| OpenAI gateway handler | OpenAI/Responses/Codex-shaped request path | `backend/internal/handler/openai_gateway_handler.go` |
| Failover loop | Account-switch / same-account-retry decision machine | `backend/internal/handler/failover_loop.go` |
| DTO layer | Service structs → JSON wire types, credential redaction | `backend/internal/handler/dto/` |
| Gateway service | Account selection, upstream forwarding, billing hooks | `backend/internal/service/gateway_service.go` |
| Scheduling | Candidate filtering, sticky sessions, load awareness, RPM/quota gates | `backend/internal/service/gateway_scheduling.go` |
| Repositories | Ent/SQL/Redis implementations of service-owned interfaces | `backend/internal/repository/` |
| Security audit | Prompt audit/guard pipeline (off / async / blocking) | `backend/internal/securityaudit/coordinator.go` |
| Payment | Provider registry, load balancing, fee/amount math | `backend/internal/payment/` |
| Setup wizard | First-run config generation (HTTP + CLI) | `backend/internal/setup/` |
| Embedded frontend | Serves `dist` with runtime settings + CSP nonce injection | `backend/internal/web/embed_on.go` |
| Frontend SPA | Vue 3 admin/user panel | `frontend/src/main.ts`, `frontend/src/router/index.ts` |

## Pattern Overview

**Overall:** Layered monolith with ports-and-adapters (hexagonal) inversion between service and repository, wired by Google Wire at compile time.

**Key Characteristics:**
- Single Go binary serves three surfaces: the LLM gateway (`/v1`, `/v1beta`), the panel REST API (`/api/v1`), and the embedded Vue SPA.
- **The `service` package owns the interfaces**; `repository` imports `service` and implements them (103 repository files import `service`). `service` never imports `repository`.
- Compile-time DI: `backend/cmd/server/wire.go` declares provider sets; `wire_gen.go` (43 KB, generated) is the actual constructor. Regenerate with `go generate ./cmd/server`.
- Background workers are started inside Wire providers (`svc.Start()` in `backend/internal/service/wire.go`) and stopped by the single `provideCleanup` closure in `backend/cmd/server/wire.go`.
- Frontend build output lands in `backend/internal/web/dist` and is compiled into the binary under the `embed` build tag.
- Multi-provider gateway: one request shape can be normalized and forwarded to a different upstream protocol (`pkg/apicompat`, `service/gateway_forward_as_chat_completions.go`, `service/gateway_forward_as_responses.go`).

## Layers

**Entry / DI (`backend/cmd/`):**
- Purpose: Process bootstrap, version/flags, Wire graph, shutdown.
- Location: `backend/cmd/server/`
- Contains: `main.go`, `wire.go` (wireinject tag), `wire_gen.go` (generated), `VERSION`.
- Depends on: config, handler, server, setup, service, securityaudit, web.
- Used by: nothing (top of graph).

**Server (`backend/internal/server/`):**
- Purpose: HTTP transport concerns and route wiring.
- Location: `backend/internal/server/`
- Contains: `http.go`, `router.go`, `routes/*.go`, `middleware/*.go`.
- Depends on: config, handler, service, web, pkg.
- Used by: `cmd/server`.

**Handler (`backend/internal/handler/`):**
- Purpose: HTTP-facing orchestration. Parse and validate input, call services, map to DTOs, shape errors and SSE streams.
- Location: `backend/internal/handler/`, `backend/internal/handler/admin/`, `backend/internal/handler/dto/`, `backend/internal/handler/quotaview/`
- Contains: 300+ Go files; `Handlers` and `AdminHandlers` aggregate structs in `handler.go`.
- Depends on: service, domain, dto, middleware, pkg. **Never imports `repository`.**
- Used by: `server/routes`.

**Service (`backend/internal/service/`):**
- Purpose: All business logic, plus the port interfaces the repository layer implements.
- Location: `backend/internal/service/` (+ `service/openai_ws_v2/`, `service/prompts/`)
- Contains: ~490 non-test files, 188 interface declarations, background workers, scheduler, billing.
- Depends on: config, domain, model, pkg, and `ent` only where transactional access is needed (29 files, e.g. `service/auth_service.go`, `service/payment_order.go`).
- Used by: handler, middleware, repository, securityaudit, payment.

**Repository (`backend/internal/repository/`):**
- Purpose: Adapters for Postgres (Ent + raw SQL), Redis caches, S3, and outbound third-party clients (captcha, GitHub releases, pricing feeds).
- Location: `backend/internal/repository/` (flat, ~200 non-test files)
- Contains: `*_repo.go` (persistence), `*_cache.go` (Redis), `ent.go` (client init + migrations), `redis.go`, `wire.go` (`ProviderSet`).
- Depends on: `ent`, `migrations`, config, `service` (for the interfaces it satisfies), pkg.
- Used by: Wire only — never referenced from handler.

**Domain / Model (`backend/internal/domain/`, `backend/internal/model/`):**
- Purpose: Shared constants and small value types with no dependencies.
- Location: `backend/internal/domain/constants.go`, `backend/internal/model/error_passthrough_rule.go`
- Contains: platform identifiers, announcement/quota constants, reasoning-effort enums.
- Depends on: stdlib only.
- Used by: every other layer.

**Infrastructure kit (`backend/internal/pkg/`):**
- Purpose: Reusable, business-agnostic helpers.
- Location: 28 subpackages, e.g. `pkg/errors`, `pkg/response`, `pkg/logger`, `pkg/httpclient`, `pkg/tlsfingerprint`, `pkg/apicompat`, `pkg/pagination`, `pkg/ctxkey`, `pkg/websearch`.
- Depends on: stdlib + third-party only (no `internal/service`).
- Used by: all layers.

**Persistence schema (`backend/ent/`, `backend/migrations/`):**
- Purpose: Ent-generated CRUD/query builders plus hand-written idempotent SQL migrations.
- Location: `backend/ent/schema/*.go` (declarations), `backend/ent/**` (generated), `backend/migrations/*.sql` (276 files).
- Contains: 40+ entities (account, user, api_key, group, usage_log, payment_order, channel_monitor, …).
- Note: `backend/ent` outside `schema/` is generated. Change `ent/schema/*.go`, then run `go generate ./ent` and commit the output.

**Frontend (`frontend/src/`):**
- Purpose: Vue 3 SPA for admin and user panels.
- Location: `frontend/src/`
- Contains: `main.ts` bootstrap, `router/index.ts` (1006 lines, lazy-loaded routes + guards), Pinia stores, `api/` axios modules, views, components, composables, i18n.
- Depends on: backend REST API via `frontend/src/api/client.ts`.
- Used by: browser; served embedded by `backend/internal/web/embed_on.go` in release builds.

## Data Flow

### Primary Request Path — LLM gateway (Anthropic-shaped)

1. `POST /v1/messages` hits the Gin engine; global middleware runs (`backend/internal/server/router.go:59-71`).
2. Route-level middleware chain applies: body limit, client request ID, ops error logger, endpoint normalization, `APIKeyAuth`, composite target resolution, group gate (`backend/internal/server/routes/gateway.go:185`).
3. `GatewayHandler.Messages` reads the body, parses it into `service.ParsedRequest`, and resolves channel model mapping (`backend/internal/handler/gateway_handler.go:122`).
4. Security audit coordinator evaluates the prompt (off / async enqueue / blocking) (`backend/internal/securityaudit/coordinator.go:29`).
5. User concurrency slot is acquired with wait, then billing eligibility is re-checked post-wait (`backend/internal/handler/gateway_handler.go:~228-250`).
6. Sticky session hash is computed and any bound account ID is prefetched into the request context (`backend/internal/handler/gateway_handler.go:~256-290`).
7. `GatewayService.SelectAccountWithLoadAwareness` filters candidates by platform, model whitelist, schedulability, window cost, RPM, quota, and profit control, then acquires an account concurrency slot (`backend/internal/service/gateway_scheduling.go:100`).
8. The request is forwarded upstream through `service.HTTPUpstream` (optionally with a TLS fingerprint profile) (`backend/internal/service/http_upstream_port.go:11`, `backend/internal/service/gateway_forward.go`).
9. On a retryable upstream error, `HandleFailoverError` returns `FailoverContinue` / `FailoverExhausted` / `FailoverCanceled` and the handler loops to the next account (`backend/internal/handler/failover_loop.go`).
10. Response (streamed or buffered) is relayed to the client; a usage-record task is submitted to the worker pool for billing and usage logging (`backend/internal/handler/gateway_handler.go:2392`).

### Panel API Path

1. `GET /api/v1/...` → global middleware → `panelRateLimiter` (Redis-backed, per user ID or per client IP) (`backend/internal/server/router.go:124`).
2. `JWTAuth` / `AdminAuth` / `StepUpAuth` resolves the auth subject; `AuditLog` records admin mutations.
3. Handler in `backend/internal/handler/` or `backend/internal/handler/admin/` calls services.
4. Service reads/writes through a repository interface; repository uses Ent, raw SQL, or a Redis cache.
5. Handler maps the service struct to a DTO (`dto.AccountFromService`, `dto.UserFromService`) and replies via `pkg/response`.

### Startup Path

1. `logger.InitBootstrap()`, flag parsing (`--setup`, `--version`).
2. `setup.NeedsSetup()` → either the setup wizard server (`runSetupServer`) or auto-setup from env, or straight to `runMainServer` (`backend/cmd/server/main.go:78-94`).
3. `config.LoadForBootstrap()` then `logger.Init` from config.
4. `initializeApplication(buildInfo)` (Wire) builds config → Ent client + migrations → Redis → repositories → services (starting background workers) → handlers → middleware → router → `http.Server`.
5. `app.PromptAudit.Start(ctx)` — a failure here degrades Prompt Audit but does not abort startup (`backend/cmd/server/main.go:156-164`).
6. Server goroutine listens; SIGINT/SIGTERM triggers a 5 s `Shutdown` plus the 10 s `provideCleanup` closure.

**State Management:**
- Postgres is the source of truth; Redis holds sticky sessions, concurrency slots, scheduler snapshots, rate-limit and RPM counters, billing and API-key caches, and pub/sub cache invalidation.
- Per-request state travels in `context.Context` under `pkg/ctxkey` keys plus `gin.Context` keys defined in `backend/internal/server/middleware/middleware.go:16-32`.
- Frontend state is in Pinia stores (`frontend/src/stores/`); auth token in `localStorage` under `auth_token`.

## Key Abstractions

**Repository ports (service-owned interfaces):**
- Purpose: Invert the dependency so business logic does not know about Ent or Redis.
- Examples: `service.AccountRepository` (`backend/internal/service/account_service.go:50`), `service.HTTPUpstream` (`backend/internal/service/http_upstream_port.go:11`), `service.UserPlatformQuotaRepository` (`backend/internal/service/user_platform_quota_port.go`).
- Pattern: Declare the interface next to its consumer in `service`; implement it in `repository`; register the constructor in `backend/internal/repository/wire.go`.

**Wire provider sets:**
- Purpose: Compose the object graph without a runtime container.
- Examples: `config.ProviderSet`, `repository.ProviderSet` (`backend/internal/repository/wire.go:67`), `service.ProviderSet` (`backend/internal/service/wire.go:796`), `middleware.ProviderSet`, `handler.ProviderSet`, `server.ProviderSet` (`backend/internal/server/http.go:24`).
- Pattern: A `ProvideXxx` function performs any post-construction setter injection and `Start()` call, then returns the value.

**Handler aggregates:**
- Purpose: Pass one struct to the router instead of ~60 constructor arguments.
- Examples: `handler.Handlers`, `handler.AdminHandlers` (`backend/internal/handler/handler.go:9-70`).
- Pattern: Wire fills the struct; `routes.RegisterXxxRoutes` reads fields off it.

**Middleware type aliases:**
- Purpose: Give Wire distinct types for interchangeable `gin.HandlerFunc` values.
- Examples: `middleware.JWTAuthMiddleware`, `AdminAuthMiddleware`, `APIKeyAuthMiddleware` (`backend/internal/server/middleware/wire.go:8-19`).
- Pattern: `type XxxMiddleware gin.HandlerFunc`; convert with `gin.HandlerFunc(x)` at the registration site.

**ApplicationError:**
- Purpose: Carry HTTP status, machine reason code, and message through the layers.
- Examples: `service.ErrAccountNotFound = infraerrors.NotFound("ACCOUNT_NOT_FOUND", ...)` (`backend/internal/service/account_service.go:13`).
- Pattern: Construct with `pkg/errors` helpers; render with `response.ErrorFrom`.

**Failover decision machine:**
- Purpose: Bound retry and account-switch behavior per request.
- Examples: `FailoverAction` (`FailoverContinue` / `FailoverExhausted` / `FailoverCanceled`), `service.UpstreamFailoverError` (`backend/internal/handler/failover_loop.go:22-31`).
- Pattern: The handler owns the loop; the service returns a classified error; the loop decides retry vs. switch vs. abort.

**DTO mappers:**
- Purpose: Keep wire format decoupled from service structs and redact credentials.
- Examples: `dto.AccountFromService`, `dto.AccountFromServiceShallow`, `dto.redactAccountManagedExtra` (`backend/internal/handler/dto/mappers.go:228,405,423`).
- Pattern: `XxxFromService` for the user view, `XxxFromServiceAdmin` for the admin view, `XxxFromServiceShallow` when relations are not loaded.

## Entry Points

**Main server:**
- Location: `backend/cmd/server/main.go`
- Triggers: `go run ./cmd/server`, `bin/server`, container `CMD`.
- Responsibilities: Bootstrap logging, dispatch setup vs. normal mode, build the app via Wire, serve, shut down gracefully.

**Setup wizard (HTTP):**
- Location: `backend/internal/setup/handler.go`, routes via `setup.RegisterRoutes` (called from `backend/cmd/server/main.go:104`)
- Triggers: First run when `setup.NeedsSetup()` is true and auto-setup is off.
- Responsibilities: Collect DB/Redis/admin details, write config, then require a restart.

**Setup wizard (CLI):**
- Location: `backend/internal/setup/cli.go` via `setup.RunCLI()`
- Triggers: `server --setup`.
- Responsibilities: Same as the HTTP wizard, non-interactive-friendly.

**Auxiliary commands:**
- `backend/cmd/jwtgen/main.go` — generate a JWT for testing.
- `backend/cmd/profit-preview/main.go` — preview group profit-control outcomes.
- `backend/cmd/cleanup-ingress-reject-logs/main.go` — one-off ingress-reject log cleanup.

**Frontend:**
- Location: `frontend/src/main.ts`
- Triggers: Browser load of `index.html` (dev via `vite`, prod via the embedded SPA middleware).
- Responsibilities: Apply theme before mount, hydrate `useAppStore` from server-injected config, install Pinia/router/i18n, await `router.isReady()`, mount.

## Architectural Constraints

- **Threading:** Go goroutine-per-request under Gin. Background workers each own a ticker goroutine started in `backend/internal/service/wire.go` and stopped through `provideCleanup`. Streaming responses deliberately have no `WriteTimeout` and no `ReadTimeout` on `http.Server` (`backend/internal/server/http.go:119-124`); only `ReadHeaderTimeout` and `IdleTimeout` are set.
- **Dependency direction:** `cmd → server → handler → service → (interfaces) ← repository → ent`. `handler` must not import `repository` or use Ent for panel CRUD; `service` must not import `repository`. Verified: zero `internal/repository` imports in `handler/` non-test files, zero `internal/repository` imports in `service/` non-test files.
- **Global state:** Several process-level singletons exist and are set at startup or on settings save — `service.SetWebSearchManager` (atomic pointer, `backend/internal/service/gateway_websearch_emulation.go:39`), `middleware.SetIngressRejectRecorder` (`backend/internal/server/middleware/ingress_reject.go:75`), `service.SetDefaultIdempotencyCoordinator`, `service.SetCodexIdentityEnforcementEnabled`, `logger.SetSink` / `logger.SetLevel`, `timezone.Init`. Treat these as write-once-at-boot (or settings-save) and never per-request.
- **Circular imports:** None between layers, because interfaces live in `service`. `securityaudit` imports both `service` and `repository`, so it sits above the repository layer and must not be imported from `service`.
- **Code generation:** `backend/ent/**` (except `schema/`) and `backend/cmd/server/wire_gen.go` are generated and committed. Editing them by hand is wrong; regenerate with `go generate ./ent` and `go generate ./cmd/server`.
- **Build tags:** `embed` toggles the bundled SPA (`backend/internal/web/embed_on.go` vs. `embed_off.go`). Tests use `unit`, `integration`, and `e2e` tags (`backend/Makefile`).
- **Migrations:** SQL files in `backend/migrations/` are embedded (`migrations/migrations.go`), must be idempotent (`IF NOT EXISTS`), are checksum-verified, and must never be edited after release. Ent auto-migration plus these SQL files both run inside `repository.InitEnt`.
- **Frontend build coupling:** `vite.config.ts` writes to `../backend/internal/web/dist` with `emptyOutDir: true`. The backend `embed` build fails without that directory.
- **Go toolchain pinning:** CI hard-asserts `go1.26.6` (`.github/workflows/backend-ci.yml`, `release.yml`, `security-scan.yml`) and three Dockerfiles pin the Go image. Version bumps must touch all of them (see `DEV_GUIDE.md` section 3).

## Anti-Patterns

### Handler reaching into Ent or repository directly

**What happens:** A few auth/payment handlers hold an `*ent.Client` — `handler/auth_oidc_oauth.go`, `handler/auth_dingtalk_oauth.go`, `handler/auth_linuxdo_oauth.go`, `handler/auth_wechat_oauth.go`, `handler/auth_email_oauth.go`, `handler/auth_oauth_pending_flow.go`, `handler/payment_handler.go`, `handler/admin/payment_handler.go`.
**Why it's wrong:** It bypasses the service port boundary, makes those handlers untestable without a database, and spreads transaction management into the transport layer.
**Do this instead:** Add a method to the relevant service (e.g. `service.AuthService`, which already exposes transactional helpers around `s.entClient` in `backend/internal/service/auth_service.go:773`) and call that from the handler.

### Adding a repository interface in the repository package

**What happens:** Declaring `type FooRepository interface` inside `backend/internal/repository/`.
**Why it's wrong:** It reverses the dependency: `repository` already imports `service`, so a consumer-side interface there creates a cycle and defeats the port pattern.
**Do this instead:** Declare the interface in the consuming service file (see `service.AccountRepository` in `backend/internal/service/account_service.go:50`), implement it in `repository`, and register the constructor in `backend/internal/repository/wire.go`.

### Starting a goroutine without a matching stop path

**What happens:** A new background service spins up a ticker in its constructor but is not added to `provideCleanup`.
**Why it's wrong:** Shutdown leaks the goroutine, in-flight work is lost, and tests that build the graph never terminate cleanly.
**Do this instead:** Add a `Stop()` method, start the worker from a `ProvideXxx` function in `backend/internal/service/wire.go`, and add a `cleanupStep` entry in `provideCleanup` (`backend/cmd/server/wire.go:75-360`).

### Editing generated code

**What happens:** Hand-editing `backend/ent/*.go` or `backend/cmd/server/wire_gen.go`.
**Why it's wrong:** The next `go generate` silently discards the change, and CI diffs become unreviewable.
**Do this instead:** Edit `backend/ent/schema/*.go` or `backend/cmd/server/wire.go`, regenerate, and commit the generated output.

### Growing the god-files

**What happens:** New gateway behavior is appended to `backend/internal/handler/gateway_handler.go` (2467 lines), `backend/internal/handler/openai_gateway_handler.go` (3710 lines), or `backend/internal/service/account.go` (3186 lines).
**Why it's wrong:** These files already exceed a reviewable size and concentrate unrelated concerns, which is how the model-mapping regressions in `DEV_GUIDE.md` (坑 10) happened.
**Do this instead:** Follow the split already present in the package: `gateway_handler_chat_completions.go`, `gateway_handler_responses.go`, `gateway_scheduling.go`, `gateway_usage_billing.go`. Add a new topic-named file in the same package.

### Bypassing the DTO layer

**What happens:** Returning a `service.Account` or `ent.Account` straight from a handler with `c.JSON`.
**Why it's wrong:** Service structs carry `Credentials` and `Extra` maps; serializing them leaks upstream tokens. `dto.redactAccountManagedExtra` exists precisely for this (`backend/internal/handler/dto/mappers.go:405`).
**Do this instead:** Map through `dto.XxxFromService` / `XxxFromServiceAdmin` and reply with `pkg/response` helpers.

## Error Handling

**Strategy:** Typed `ApplicationError` values (status + reason code + message) created in the service layer, translated to a stable envelope at the handler boundary. The LLM gateway paths use provider-shaped error bodies instead of the panel envelope.

**Patterns:**
- Declare sentinel errors as package-level vars with `pkg/errors` constructors: `infraerrors.NotFound("ACCOUNT_NOT_FOUND", "account not found")` (`backend/internal/service/account_service.go:13-16`).
- Panel responses use the fixed envelope `{code, message, reason, metadata, data}` via `response.Success` / `response.Error` / `response.ErrorFrom` (`backend/internal/pkg/response/response.go`).
- Gateway responses use provider-native error shapes through `GatewayHandler.errorResponse` and `handleStreamingAwareError`, which suppress a body once bytes have already been streamed (`backend/internal/handler/gateway_handler.go:1837-1913`).
- Upstream failures are classified into `service.UpstreamFailoverError` with flags such as `RetryableOnSameAccount` and `RequestScopedTransient`, driving exponential backoff capped at 8 s (`backend/internal/handler/failover_loop.go:38-79`).
- Configurable per-tenant error passthrough rules let raw upstream errors reach the client: `service.ErrorPassthroughService`, bound onto the request via `service.BindErrorPassthroughService`.
- `middleware.Recovery()` is the outermost middleware and converts panics into 500s (`backend/internal/server/router.go:51`).
- Startup failures are fatal (`log.Fatalf`) except Prompt Audit, which degrades to `ModeOff` so the gateway stays usable (`backend/cmd/server/main.go:156-164`).

## Cross-Cutting Concerns

**Logging:** Zap through `backend/internal/pkg/logger`, initialized from config with a `slog` bridge and a pluggable `Sink` (used by `service.OpsSystemLogSink` to persist system logs). Per-request loggers are derived with `requestLogger(c, "handler.gateway.messages", zap.Int64(...))`; sensitive fields go through `internal/util/logredact`. Access logs come from `middleware.RequestLogger()` and `middleware.Logger()`.

**Validation:** Gin binding plus explicit guards in handlers (empty body, missing `model`, composite-target resolution). Body size is capped globally by `http.MaxBytesHandler` (`backend/internal/server/http.go:127-134`) and per-route by `middleware.RequestBodyLimit`. Config is validated at load (`backend/internal/config/config.go`, `validate_dingtalk.go`).

**Authentication:** Three independent schemes — JWT for the panel (`middleware/jwt_auth.go`, optional variant for public pages), `AdminAuth` for admin routes with `StepUpAuth` for sensitive mutations (`middleware/step_up.go`), and API-key auth for the gateway (`middleware/api_key_auth.go`, with Google/image-task variants). TOTP and Passkey/WebAuthn are supported second factors. Client IP and UA are pinned into the request context by `middleware.SessionBindingContext` for token issuance and audit.

**Authorization:** Group/plan gating on gateway routes (`requireGroupAnthropic`, composite target resolution in `backend/internal/server/routes/gateway.go`), role checks in `middleware/admin_auth.go` and `admin_only.go`, and per-user allowed-group records (`ent/schema/user_allowed_group.go`).

**Rate limiting & quota:** `middleware.NewPanelRateLimiter` (Redis, per user ID or client IP, thresholds from system settings) for the panel; `service.RateLimitService`, RPM caches, per-account concurrency slots, window-cost gates, and `user_platform_quota` for the gateway.

**Auditing:** `middleware.AuditLogMiddleware` plus `service.AuditLogService` write admin actions to `audit_log`; `middleware.SetAuditAction` / `SetAuditActor` / `SetAuditExtra` enrich entries from handlers. `middleware.IngressReject` records pre-auth rejections for ops dashboards.

**Security headers:** `middleware.SecurityHeaders` emits CSP with a per-request nonce; `frame-src` origins are refreshed from settings into an `atomic.Pointer` cache and re-read on every settings save (`backend/internal/server/router.go:42-90`). The embedded SPA injects the nonce and public settings into `index.html` (`backend/internal/web/embed_on.go`).

**Observability:** `middleware.ServerTiming` plus `pkg/servertiming` add Server-Timing headers; `service.OpsMetricsCollector`, `OpsAggregationService`, `OpsAlertEvaluatorService`, and `OpsScheduledReportService` feed the admin ops dashboards. `/health` is unauthenticated (`backend/internal/server/routes/common.go:11`).

---

*Architecture analysis: 2026-08-23*
