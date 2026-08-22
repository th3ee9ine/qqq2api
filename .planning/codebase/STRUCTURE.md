# Codebase Structure

**Analysis Date:** 2026-08-23

## Directory Layout

```
qqq2api/
├── backend/                        # Go 1.26 API server + LLM gateway
│   ├── cmd/                        # Executable entry points
│   │   ├── server/                 # Main binary (main.go, wire.go, wire_gen.go, VERSION)
│   │   ├── jwtgen/                 # JWT generation helper
│   │   ├── profit-preview/         # Group profit-control preview tool
│   │   └── cleanup-ingress-reject-logs/  # One-off log cleanup job
│   ├── ent/                        # Ent ORM: schema/ is source, rest is GENERATED
│   │   ├── schema/                 # 40+ entity declarations (edit here)
│   │   ├── migrate/, hook/, runtime/, predicate/, intercept/, enttest/
│   │   └── <entity>/               # Generated per-entity predicate packages
│   ├── internal/
│   │   ├── config/                 # Viper config structs, load, validation
│   │   ├── domain/                 # Dependency-free constants and enums
│   │   ├── model/                  # Small standalone value types
│   │   ├── handler/                # HTTP handlers (~300 files)
│   │   │   ├── admin/              # Admin-only handlers (~127 files)
│   │   │   ├── dto/                # Wire types + service→DTO mappers
│   │   │   └── quotaview/          # Quota presentation helpers
│   │   ├── service/               # Business logic + port interfaces (~490 files)
│   │   │   ├── openai_ws_v2/       # OpenAI realtime WS passthrough relay
│   │   │   ├── prompts/            # Embedded prompt text assets
│   │   │   └── testdata/           # Service-layer test fixtures
│   │   ├── repository/            # Ent/SQL/Redis adapters (flat, ~200 files)
│   │   ├── server/
│   │   │   ├── http.go, router.go  # http.Server + global middleware wiring
│   │   │   ├── routes/             # Per-domain route registration
│   │   │   └── middleware/         # Auth, audit, CORS, CSP, rate limit
│   │   ├── securityaudit/         # Prompt audit / guard pipeline
│   │   ├── payment/               # Payment providers, fees, load balancing
│   │   │   └── provider/           # Per-provider implementations
│   │   ├── setup/                 # First-run wizard (HTTP + CLI)
│   │   ├── web/                   # Embedded SPA serving (embed build tag)
│   │   ├── platform/              # OS-specific code (liveattestation)
│   │   ├── pkg/                   # 28 reusable infra subpackages
│   │   ├── util/                  # logredact, urlvalidator, httputil, responseheaders
│   │   ├── integration/           # Build-tagged e2e tests
│   │   └── testutil/              # Shared fixtures, stubs, httptest helpers
│   ├── migrations/                # 276 embedded idempotent .sql files + migrations.go
│   ├── resources/model-pricing/   # Bundled pricing data
│   ├── scripts/                   # resolve-version.sh, maintenance SQL
│   ├── go.mod, go.sum, Makefile, .golangci.yml, Dockerfile
├── frontend/                       # Vue 3 + TypeScript SPA (pnpm)
│   ├── src/
│   │   ├── main.ts                 # Bootstrap (theme → pinia → i18n → router)
│   │   ├── App.vue
│   │   ├── router/                 # index.ts (1006 lines), guards, title, setupRedirect
│   │   ├── stores/                 # Pinia stores
│   │   ├── api/                    # Axios modules (client.ts + per-domain)
│   │   │   └── admin/              # Admin API modules
│   │   ├── views/                  # Route-level pages (admin/, user/, auth/, setup/, public/)
│   │   ├── components/             # Reusable components by domain
│   │   ├── features/               # Self-contained feature slices
│   │   ├── composables/            # useXxx() composition functions
│   │   ├── constants/, types/, utils/, i18n/, styles/, assets/
│   │   └── __tests__/              # Cross-cutting specs
│   ├── package.json, pnpm-lock.yaml, vite.config.ts, vitest.config.ts
│   ├── tsconfig.json, tailwind.config.js, .eslintrc.cjs
├── deploy/                         # Docker Compose, Caddyfile, systemd, install scripts
│   └── tests/                      # Deployment smoke tests + fixtures
├── docs/                           # Feature and integration docs
├── openspec/                       # Spec-driven change proposals
│   └── changes/<change-name>/      # proposal, design, tasks, verification
├── skills/sub2api-admin/           # Admin CLI skill (SKILL.md, scripts/, references/)
├── assets/                         # Marketing/partner logos
├── tools/                          # check_pnpm_audit_exceptions.py
├── .github/workflows/              # backend-ci, security-scan, release, cla
├── Makefile                        # Top-level build/test entry
├── Dockerfile, Dockerfile.goreleaser, .goreleaser*.yaml
└── DEV_GUIDE.md, README*.md, CLA.md, LICENSE
```

## Directory Purposes

**`backend/cmd/`:**
- Purpose: Executable `main` packages. One directory per binary.
- Contains: `main.go` per command; `server/` additionally holds the Wire definitions.
- Key files: `backend/cmd/server/main.go`, `backend/cmd/server/wire.go`, `backend/cmd/server/wire_gen.go`, `backend/cmd/server/VERSION`.

**`backend/ent/`:**
- Purpose: Ent ORM layer. Only `schema/` is hand-written; everything else is generated by `go generate ./ent` and committed.
- Contains: Entity schemas, generated builders, per-entity predicate packages, mixins.
- Key files: `backend/ent/schema/account.go`, `backend/ent/schema/user.go`, `backend/ent/schema/api_key.go`, `backend/ent/schema/usage_log.go`, `backend/ent/schema/mixins/`.

**`backend/internal/handler/`:**
- Purpose: HTTP request handling — parse, validate, orchestrate services, map DTOs, shape responses and SSE streams.
- Contains: Flat `*_handler.go` files plus topic files for the gateway; `admin/` for admin endpoints; `dto/` for wire types.
- Key files: `backend/internal/handler/handler.go` (the `Handlers` / `AdminHandlers` aggregates), `backend/internal/handler/gateway_handler.go`, `backend/internal/handler/openai_gateway_handler.go`, `backend/internal/handler/failover_loop.go`, `backend/internal/handler/dto/mappers.go`.

**`backend/internal/service/`:**
- Purpose: All business logic, and the home of every repository/port interface.
- Contains: ~490 non-test Go files, 188 interface declarations, background workers, the account scheduler, billing, per-provider gateway logic.
- Key files: `backend/internal/service/wire.go` (`ProviderSet` + worker startup), `backend/internal/service/gateway_service.go`, `backend/internal/service/gateway_scheduling.go`, `backend/internal/service/account.go`, `backend/internal/service/account_service.go` (port definitions), `backend/internal/service/http_upstream_port.go`, `backend/internal/service/billing_service.go`.

**`backend/internal/repository/`:**
- Purpose: Adapters implementing the service interfaces — Postgres via Ent and raw SQL, Redis caches, S3, and outbound third-party clients.
- Contains: Flat package, ~200 non-test files. No subdirectories.
- Key files: `backend/internal/repository/ent.go` (client init + migration run), `backend/internal/repository/redis.go`, `backend/internal/repository/wire.go` (`ProviderSet`), `backend/internal/repository/account_repo.go` (3822 lines), `backend/internal/repository/db_pool.go`.

**`backend/internal/server/`:**
- Purpose: HTTP transport wiring only. No business logic.
- Contains: `http.go` (server config, h2c, body cap, trusted proxies), `router.go` (global middleware + module registration), `routes/`, `middleware/`.
- Key files: `backend/internal/server/router.go`, `backend/internal/server/http.go`, `backend/internal/server/routes/gateway.go`, `backend/internal/server/routes/admin.go`, `backend/internal/server/api_contract_test.go` (108 KB route contract snapshot).

**`backend/internal/pkg/`:**
- Purpose: Business-agnostic infrastructure kit shared by every layer. Must not import `internal/service`.
- Contains: 28 subpackages — `errors`, `response`, `logger`, `httpclient`, `httputil`, `tlsfingerprint`, `apicompat`, `openai`, `openai_compat`, `claude`, `gemini`, `geminicli`, `googleapi`, `antigravity`, `anthropicfp`, `xai`, `oauth`, `pagination`, `ctxkey`, `ip`, `proxyurl`, `proxyutil`, `redissession`, `servertiming`, `sysutil`, `timezone`, `usagestats`, `websearch`.
- Key files: `backend/internal/pkg/errors/types.go`, `backend/internal/pkg/response/response.go`, `backend/internal/pkg/logger/logger.go`, `backend/internal/pkg/apicompat/chatcompletions_responses_bridge.go`.

**`backend/migrations/`:**
- Purpose: Hand-written idempotent SQL migrations, embedded into the binary.
- Contains: 276 `NNN_description.sql` files, `migrations.go` (the `embed.FS`), `README.md`, and migration verification tests.
- Key files: `backend/migrations/migrations.go`, `backend/migrations/001_init.sql`.

**`frontend/src/views/`:**
- Purpose: Route-level pages, one per route, lazy-loaded from `router/index.ts`.
- Contains: `admin/` (dashboard, accounts, groups, settings, ops, orders, affiliates), `user/` (dashboard, keys, usage, payment, subscriptions), `auth/` (login, register, OAuth callbacks), `setup/`, `public/`, plus top-level `HomeView.vue`, `ModelPlazaView.vue`, `KeyUsageView.vue`, `NotFoundView.vue`.
- Key files: `frontend/src/views/admin/DashboardView.vue`, `frontend/src/views/admin/SettingsView.vue`, `frontend/src/views/user/PaymentView.vue`.

**`frontend/src/features/`:**
- Purpose: Self-contained feature slices that own their components, API calls, types, and view model together.
- Contains: `channel-monitor-v2/`, `prompt-audit/`.
- Key files: `frontend/src/features/prompt-audit/PromptAuditView.vue`, `frontend/src/features/prompt-audit/api.ts`, `frontend/src/features/prompt-audit/viewModel.ts`, `frontend/src/features/channel-monitor-v2/monitorFormat.ts`.

**`frontend/src/api/`:**
- Purpose: Typed axios wrappers, one module per backend domain.
- Contains: `client.ts` (shared instance + interceptors), `tokenRefresh.ts`, `url.ts`, `adminUIRequest.ts`, per-domain modules, `admin/` subdirectory.
- Key files: `frontend/src/api/client.ts`, `frontend/src/api/tokenRefresh.ts`, `frontend/src/api/url.ts`.

**`openspec/changes/`:**
- Purpose: Spec-driven change proposals with design, tasks, verification, and frozen source snapshots.
- Contains: One directory per change, e.g. `add-openai-compatible-prompt-audit/`.
- Key files: `openspec/config.yaml`, `openspec/changes/<change>/proposal.md`, `design.md`, `tasks.md`, `verification.md`.

## Key File Locations

**Entry Points:**
- `backend/cmd/server/main.go`: Process entry — flags, setup-vs-server dispatch, graceful shutdown.
- `backend/cmd/server/wire.go`: Wire provider-set declaration and the `provideCleanup` shutdown closure (build tag `wireinject`).
- `backend/cmd/server/wire_gen.go`: Generated constructor for the whole graph. Do not hand-edit.
- `backend/internal/setup/handler.go` / `backend/internal/setup/cli.go`: First-run wizard.
- `frontend/src/main.ts`: SPA bootstrap.
- `frontend/index.html`: Vite HTML entry; also the injection target for runtime settings and the CSP nonce.

**Configuration:**
- `backend/internal/config/config.go`: 3816 lines — the full `Config` tree (server, log, cors, security, billing, database, redis, jwt, gateway, pricing, oauth providers, batch_image, image_storage, …).
- `backend/internal/config/wire.go`: Config `ProviderSet`.
- `deploy/config.example.yaml`: Template for `backend/config.yaml` (the real file is gitignored).
- `frontend/vite.config.ts`: Aliases (`@` → `src`), dev proxy for `/api` and `/v1`, `outDir: ../backend/internal/web/dist`, manual chunking.
- `frontend/tsconfig.json`, `frontend/tailwind.config.js`, `frontend/.eslintrc.cjs`, `backend/.golangci.yml`.
- `.github/workflows/backend-ci.yml`, `security-scan.yml`, `release.yml`.

**Core Logic:**
- `backend/internal/server/router.go`: Global middleware order and route-module registration.
- `backend/internal/server/routes/gateway.go`: All LLM gateway routes (`/v1`, `/v1beta`, `/responses`, `/antigravity/*`, images, videos, voice, realtime).
- `backend/internal/server/routes/admin.go`: 38 KB of admin route groups.
- `backend/internal/handler/gateway_handler.go`: Anthropic-shaped request path.
- `backend/internal/handler/openai_gateway_handler.go`: OpenAI/Responses/Codex path.
- `backend/internal/handler/failover_loop.go`: Retry/switch decision machine.
- `backend/internal/service/gateway_scheduling.go`: Account candidate filtering and selection.
- `backend/internal/service/billing_service.go`, `gateway_usage_billing.go`: Cost computation and usage recording.
- `backend/internal/securityaudit/coordinator.go`: Prompt audit mode dispatch.
- `backend/internal/repository/ent.go`: DB connection, timezone init, migration execution.

**Testing:**
- `backend/internal/server/api_contract_test.go`: Route/contract snapshot — update when routes change.
- `backend/internal/integration/`: `e2e_gateway_test.go`, `e2e_user_flow_test.go` (build tag `e2e`).
- `backend/internal/testutil/`: `fixtures.go`, `stubs.go`, `httptest.go`.
- `backend/ent/enttest/`: In-memory Ent client for repository tests.
- `frontend/src/**/__tests__/*.spec.ts`: Vitest specs co-located per directory.
- `Makefile` `FRONTEND_CRITICAL_VITEST`: The 13 specs that gate the frontend build.

**Documentation:**
- `DEV_GUIDE.md`: Local environment, CI requirements, and 11 documented pitfalls. Read before changing Go version, Ent schemas, interfaces, or `pnpm-lock.yaml`.
- `docs/`: `PAYMENT.md`, `COMPOSITE_GROUPS.md`, `ASYNC_IMAGE_TASKS.md`, `BATCH_IMAGE_MVP.md`, `ADMIN_PAYMENT_INTEGRATION_API.md`, `channel-monitor-v2-safe-defaults.md`.
- `deploy/DOCKER.md`, `deploy/EDGE_SECURITY.md`, `deploy/README.md`.
- `skills/sub2api-admin/SKILL.md` + `references/admin-cli.md`: Admin API CLI usage.

## Naming Conventions

**Go files:**
- `snake_case.go`, named after the topic: `gateway_scheduling.go`, `token_refresh_service.go`.
- Handlers: `<domain>_handler.go` (`api_key_handler.go`, `admin/account_handler.go`).
- Gateway handler split by concern with a shared prefix: `gateway_handler_chat_completions.go`, `gateway_handler_responses.go`.
- Repositories: `<entity>_repo.go`; large ones split by concern with the same prefix (`usage_log_repo_insert.go`, `usage_log_repo_query.go`, `usage_log_repo_stats.go`).
- Redis caches: `<domain>_cache.go` (`billing_cache.go`, `api_key_cache.go`).
- Port interfaces intended as ports: `*_port.go` (`http_upstream_port.go`, `user_platform_quota_port.go`).
- DI: `wire.go` per package holding `ProviderSet` and `ProvideXxx` functions.
- Tests: `<file>_test.go` alongside the source. Build tags `unit`, `integration`, `e2e` at the top where applicable.

**Go identifiers:**
- Exported `PascalCase`, unexported `camelCase`, acronyms stay uppercase (`APIKeyService`, `HTTPUpstream`, `TLSFingerprintProfile`).
- Constructors: `NewXxx`. Wire wrappers that need extra setup: `ProvideXxx`.
- Sentinel errors: `ErrXxx` package-level vars built with `pkg/errors` helpers.
- DTO mappers: `XxxFromService`, `XxxFromServiceAdmin`, `XxxFromServiceShallow`.
- Middleware types: `XxxMiddleware` aliasing `gin.HandlerFunc`.

**Vue / TypeScript files:**
- Components and views: `PascalCase.vue`. Views end in `View` (`DashboardView.vue`, `PaymentResultView.vue`).
- Composables: `useXxx.ts` (`useTableLoader.ts`, `useStepUp.ts`).
- Stores, api modules, utils, constants: `camelCase.ts` (`adminSettings.ts`, `channelMonitorV2.ts`, `maskApiKey.ts`).
- Non-component logic beside a view: `camelCase.ts` in the same directory (`views/admin/groupsProfitControl.ts`, `views/user/paymentUx.ts`).
- Tests: `<subject>.spec.ts` inside a sibling `__tests__/` directory.
- Barrel files: `index.ts` (used in `api/`, `stores/`, `types/`, `views/auth/`, `components/layout/`).

**Directories:**
- Go: lowercase, no separators (`securityaudit`, `openai_ws_v2` is the one underscore exception).
- Frontend: `camelCase` for shared groupings (`modelPlaza`), `kebab-case` for feature slices (`channel-monitor-v2`, `prompt-audit`), `__tests__` for specs.

**Migrations:**
- `NNN_description.sql` with a zero-padded numeric prefix and snake_case description. Must be idempotent (`IF NOT EXISTS` / `IF EXISTS`) and never edited once released.

## Where to Add New Code

**New gateway feature (new upstream endpoint or provider behavior):**
- Route: `backend/internal/server/routes/gateway.go`
- Handler: new topic file in `backend/internal/handler/`, e.g. `gateway_handler_<topic>.go`. Do not extend `gateway_handler.go` (2467 lines).
- Business logic: new file in `backend/internal/service/`, e.g. `gateway_<topic>.go` or `<provider>_gateway_<topic>.go`.
- Protocol translation helpers: `backend/internal/pkg/apicompat/` or a provider package under `backend/internal/pkg/`.
- Contract test: update `backend/internal/server/api_contract_test.go`.

**New panel API endpoint:**
- Route: `backend/internal/server/routes/user.go` or `auth.go`; admin endpoints in `backend/internal/server/routes/admin.go`.
- Handler: `backend/internal/handler/<domain>_handler.go`, or `backend/internal/handler/admin/<domain>_handler.go`. Register the field in `handler.Handlers` / `handler.AdminHandlers` (`backend/internal/handler/handler.go`) and add the constructor to `backend/internal/handler/wire.go`.
- DTO: add the type to `backend/internal/handler/dto/types.go` and the mapper to `backend/internal/handler/dto/mappers.go`.

**New business capability:**
- Service: `backend/internal/service/<domain>_service.go`, with any new port interface declared in that same file.
- Register: add `NewXxxService` (or a `ProvideXxxService` wrapper) to `ProviderSet` in `backend/internal/service/wire.go`.
- Background worker: start it in the `ProvideXxx` function, add a `Stop()` method, and add a `cleanupStep` to `provideCleanup` in `backend/cmd/server/wire.go`.

**New persistence:**
- Entity: `backend/ent/schema/<entity>.go`, then `go generate ./ent` and commit the generated files.
- Migration: `backend/migrations/NNN_<description>.sql`, idempotent.
- Interface: declare `<Entity>Repository` in the consuming `backend/internal/service/` file.
- Implementation: `backend/internal/repository/<entity>_repo.go`, registered in `backend/internal/repository/wire.go`.

**New middleware:**
- Implementation: `backend/internal/server/middleware/<name>.go`.
- If Wire must inject it, add a `type XxxMiddleware gin.HandlerFunc` alias plus the constructor in `backend/internal/server/middleware/wire.go`, and thread the parameter through `ProvideRouter` → `SetupRouter` → `registerRoutes` in `backend/internal/server/http.go` and `router.go`.

**New frontend page:**
- View: `frontend/src/views/<area>/<Name>View.vue`.
- Route: add a lazy-loaded record to `frontend/src/router/index.ts` with `meta` (`requiresAuth`, `title`, `titleKey`).
- API: `frontend/src/api/<domain>.ts` (admin-only in `frontend/src/api/admin/`), using the shared `apiClient` from `frontend/src/api/client.ts`.
- State: `frontend/src/stores/<domain>.ts` if it must be shared across views.
- Strings: `frontend/src/i18n/locales/en/` and `frontend/src/i18n/locales/zh/`.
- Test: `frontend/src/views/<area>/__tests__/<Name>View.spec.ts`.

**New frontend feature slice (multi-component, self-contained):**
- Directory: `frontend/src/features/<kebab-case-name>/` with `api.ts`, `types.ts`, `viewModel.ts`, `components/`, `__tests__/`, and the entry `<Name>View.vue`. Follow `frontend/src/features/prompt-audit/`.

**Utilities:**
- Go, business-agnostic: new subpackage under `backend/internal/pkg/`.
- Go, redaction/URL/header helpers: `backend/internal/util/`.
- Go constants shared across layers: `backend/internal/domain/constants.go`.
- Frontend: `frontend/src/utils/<name>.ts`; reusable reactive logic goes to `frontend/src/composables/useXxx.ts`; enums to `frontend/src/constants/`.

**Tests:**
- Go: `<file>_test.go` beside the source. Integration/e2e in `backend/internal/integration/` behind build tags. Reuse `backend/internal/testutil/` and `backend/ent/enttest/`.
- Frontend: `__tests__/<subject>.spec.ts` beside the source. If the spec guards a critical path, add it to `FRONTEND_CRITICAL_VITEST` in the root `Makefile`.

## Special Directories

**`backend/ent/` (excluding `schema/`):**
- Purpose: Generated Ent CRUD, query builders, and predicate packages.
- Generated: Yes — `go generate ./ent`.
- Committed: Yes.

**`backend/cmd/server/wire_gen.go`:**
- Purpose: Generated dependency-injection constructor (43 KB).
- Generated: Yes — `go generate ./cmd/server`.
- Committed: Yes.

**`backend/internal/web/dist/`:**
- Purpose: Frontend build output, embedded via `//go:embed all:dist` under the `embed` build tag.
- Generated: Yes — `pnpm --dir frontend run build` (`vite.config.ts` `outDir`).
- Committed: No. Only `.keep` is tracked so the embed directive always matches (`.gitignore:98-103`).

**`backend/migrations/`:**
- Purpose: Embedded SQL migrations.
- Generated: No, hand-written.
- Committed: Yes. Never modify a released file.

**`frontend/node_modules/`, `backend/bin/`:**
- Purpose: Dependencies and build artifacts.
- Generated: Yes.
- Committed: No (`.gitignore:13,34-36`).

**`backend/config.yaml`, `deploy/config.yaml`:**
- Purpose: Runtime configuration containing credentials.
- Generated: By the setup wizard or copied from `deploy/config.example.yaml`.
- Committed: No (`.gitignore:111-112`). Reference values by config key or env var name only.

**`openspec/changes/`:**
- Purpose: Change proposals with frozen source snapshots (`source-freeze/` holds patches and tarballs).
- Generated: Partly — snapshots are produced during the change workflow.
- Committed: Yes.

**`skills/sub2api-admin/`:**
- Purpose: Packaged admin CLI skill for operating a running deployment.
- Generated: No.
- Committed: Yes.
- Note: Reads `SUB2API_BASE_URL` plus `SUB2API_ADMIN_API_KEY` or `SUB2API_JWT` from the environment; `accounts export` emits credentials, so use `--file`.

**`assets/`, `docs/screenshots/`:**
- Purpose: README and marketing images.
- Generated: No.
- Committed: Yes.

---

*Structure analysis: 2026-08-23*
