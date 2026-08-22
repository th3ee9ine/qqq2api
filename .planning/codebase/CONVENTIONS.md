# Coding Conventions

**Analysis Date:** 2026-08-23

Monorepo with two independent convention sets: Go backend (`backend/`) and Vue 3 + TypeScript frontend (`frontend/`). Rules below are enforced by `backend/.golangci.yml`, `frontend/.eslintrc.cjs`, `frontend/tsconfig.json`, and CI (`.github/workflows/backend-ci.yml`).

## Naming Patterns

**Files — Go (`backend/`):**
- `snake_case.go`, one domain concept per file.
- Layer suffix identifies role: `_handler.go`, `_service.go`, `_repo.go`, `_cache.go`.
  - `internal/handler/api_key_handler.go`, `internal/service/account_service.go`, `internal/repository/account_repo.go`, `internal/repository/api_key_cache.go`
- Feature-scoped files drop the layer suffix and use a topic prefix instead: `internal/service/account_header_override.go`, `internal/service/account_credentials_redact.go`, `internal/handler/auth_linuxdo_oauth.go`.
- Split large concerns into many narrow files rather than one large file. `internal/service/` holds 600+ Go files under this scheme.

**Files — Frontend (`frontend/src/`):**
- Vue components: `PascalCase.vue` — `src/components/user/profile/ProfileInfoCard.vue`, `src/views/user/ChannelStatusV2View.vue`.
- Views carry a `View` suffix: `src/views/admin/SettingsView.vue`.
- Composables: `useCamelCase.ts` — `src/composables/useAccountOAuth.ts`, `src/composables/useClipboard.ts`.
- Stores, api modules, utils: `camelCase.ts` — `src/stores/adminSettings.ts`, `src/api/channelMonitorV2.ts`, `src/utils/apiError.ts`.
- Feature folders are `kebab-case`: `src/features/channel-monitor-v2/`, `src/features/prompt-audit/`.

**Functions — Go:**
- Exported `PascalCase`, unexported `camelCase`.
- Constructors are `New<Type>` returning a pointer: `NewAPIKeyHandler`, `NewAPIKeyService` (`internal/service/api_key_service.go:334`).
- DTO mappers read `<Type>FromService` with optional variant suffix: `APIKeyFromService`, `UserFromServiceShallow`, `UserFromServiceAdmin` (`internal/handler/dto/mappers.go`).
- Validators read `validate<Thing><Action>Request`: `validateAPIKeyCreateRequest` (`internal/handler/api_key_handler.go:69`).
- Initialisms stay uppercase (`API`, `ID`, `URL`, `IP`, `HTTP`). The full list is pinned in `backend/.golangci.yml:99` under `staticcheck.initialisms`. Note `ST1003` is disabled, so pre-existing `ApiKey`-style identifiers do not fail lint — write new code with correct initialisms anyway.

**Functions — Frontend:**
- `camelCase`. API modules export verb-named functions: `list`, `getById`, `create` (`src/api/keys.ts`).
- Stores export `use<Name>Store`: `useAuthStore`, `useAppStore` (`src/stores/auth.ts`, `src/stores/app.ts`).
- Event handlers in components are `handle<Event>`: `handleLogin`, `handleLogout`.

**Variables:**
- Go: `camelCase` locals; package-level unexported config constants `camelCase` (`defaultAuthLookupConcurrency`), exported constants `PascalCase` (`MaxAPIKeyCredentialBytes`).
- Frontend: `camelCase` locals; module-level constants `SCREAMING_SNAKE_CASE` — `AUTH_TOKEN_KEY`, `TOKEN_REFRESH_BUFFER` (`src/stores/auth.ts:17-23`).

**Types:**
- Go: `PascalCase` structs. Request/response payload structs live next to their handler and are named `<Action><Entity>Request` — `CreateAPIKeyRequest`, `UpdateAPIKeyRequest` (`internal/handler/api_key_handler.go:34,50`). Field-mask structs are `<Entity>UpdateFields` (`APIKeyUpdateFields`).
- TypeScript: `PascalCase` interfaces and types in `src/types/`. Local narrowing helpers use an `...Like` suffix: `ApiErrorLike` (`src/utils/apiError.ts:8`).
- JSON/API field names are `snake_case` on both sides of the wire, including in TypeScript payload types: `group_id`, `ip_whitelist`, `rate_limit_5h`.

## Code Style

**Formatting — Go:**
- `gofmt` via golangci-lint `formatters` (`backend/.golangci.yml:124`). Tabs, standard gofmt output.
- `simplify: false` — `gofmt -s` simplifications are NOT applied.
- Two rewrite rules are enforced: write `any` instead of `interface{}`, and `a[b:]` instead of `a[b:len(a)]`.

**Formatting — Frontend:**
- No Prettier, Biome, or `.editorconfig` in the repo. Style is carried by convention plus ESLint.
- 2-space indent, single quotes for imports and string literals (1168 single-quote imports vs 43 double-quote across `src/`), no trailing semicolons in most files (~12% of non-empty lines end in `;`).
- Match the file you are editing; there is no autoformatter to normalize a mismatch.

**Linting — Go (`backend/.golangci.yml`):**
- `golangci-lint` v2.9, `default: none` with an explicit allowlist: `depguard`, `errcheck`, `gosec`, `govet`, `ineffassign`, `staticcheck`, `unused`.
- **`depguard` enforces layering — the most important rule to know:**
  - `internal/service/**` must not import `internal/repository`, `gorm.io/gorm`, or `github.com/redis/go-redis/v9`. Services depend on interfaces they declare themselves; repositories implement them.
  - `internal/handler/**` must not import `internal/repository`, gorm, or redis.
  - Six `ops_*` service files plus `internal/service/wire.go` are grandfathered exceptions (`backend/.golangci.yml:22-27`). Do not add new ones.
- `errcheck` with `check-type-assertions: true` and `disable-default-exclusions: true`: every returned error must be handled, and `v := x.(T)` without the `ok` form is a finding. Only the `fmt.Print*`/`io.Copy` family listed at `backend/.golangci.yml:77-86` is exempt.
- `staticcheck` runs `all` checks minus comment-format checks `ST1000`, `ST1003`, `ST1020`, `ST1021`, `ST1022`.
- `gosec` at high severity/high confidence, with `G101`, `G104`, `G115`, `G304`, `G404` and others excluded (`backend/.golangci.yml:47-59`).
- Run before pushing: `cd backend && golangci-lint run ./...`

**Linting — Frontend (`frontend/.eslintrc.cjs`):**
- ESLint 8 flat-free config, `vue-eslint-parser` + `@typescript-eslint/parser`, extending `eslint:recommended`, `plugin:vue/vue3-essential`, `plugin:@typescript-eslint/recommended`.
- Deliberately relaxed: `@typescript-eslint/no-explicit-any` off, `ban-ts-comment` off, `vue/multi-word-component-names` off.
- Unused vars are a **warning**, and identifiers prefixed `_` are exempt (`argsIgnorePattern: "^_"`).
- Type safety comes from `tsconfig.json` instead: `strict: true`, `noUnusedLocals`, `noUnusedParameters`, `noFallthroughCasesInSwitch`. `vue-tsc --noEmit` is a CI gate.
- Commands: `pnpm lint:check` (verify), `pnpm lint` (autofix), `pnpm typecheck`.

**Package manager:** pnpm only, never npm (`DEV_GUIDE.md:13`). `pnpm-lock.yaml` must be committed; CI runs `pnpm install --frozen-lockfile`.

## Import Organization

**Go — three groups separated by blank lines:**
1. Standard library
2. Project packages (`github.com/Wei-Shaw/sub2api/...`)
3. Third-party

`internal/handler/api_key_handler.go:4-19` shows the canonical shape. `internal/service/api_key_service.go` merges groups 2 and 3 into one block; both forms exist, so follow the neighbouring file.

**Go aliasing conventions:**
- `infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"` — always aliased to avoid shadowing stdlib `errors`.
- `stderrors "errors"` when a file needs stdlib errors alongside the project package.
- `dbent "github.com/Wei-Shaw/sub2api/ent"` for the generated Ent client.
- `middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"` in handlers that also see `internal/middleware`.
- `dot-import-whitelist` permits only `fmt`; do not dot-import anything else.

**Frontend:**
1. Vue / third-party (`vue`, `pinia`, `axios`, `vitest`)
2. Project modules via the `@/` alias
3. Type-only imports last, using `import type { ... }`

`@/*` maps to `frontend/src/*`, declared in both `tsconfig.json:19` and `vitest.config.ts:9`. Always use `@/` for cross-directory imports; relative paths are used only within a directory or from a `__tests__` folder to its subject (`../locales/en/common`).

Barrel files re-export public surface: `src/api/index.ts`, `src/types/index.ts`, `src/stores/index.ts`, `src/components/common/index.ts`. Import from the barrel (`import { keysAPI } from '@/api'`) rather than deep paths where a barrel exists.

## Error Handling

**Go — typed application errors:**
`internal/pkg/errors` defines `ApplicationError` carrying `Status{Code, Reason, Message, Metadata}`. Construct via kind-named helpers, never raw `errors.New` for user-facing failures:

```go
var (
	ErrAPIKeyNotFound    = infraerrors.NotFound("API_KEY_NOT_FOUND", "api key not found")
	ErrGroupNotAllowed   = infraerrors.Forbidden("GROUP_NOT_ALLOWED", "user is not allowed to bind this group")
	ErrAPIKeyExists      = infraerrors.Conflict("API_KEY_EXISTS", "api key already exists")
	ErrAPIKeyRateLimited = infraerrors.TooManyRequests("API_KEY_RATE_LIMITED", "too many failed attempts, please try again later")
)
```

Declared as package-level `Err*` vars at the top of the service file (`internal/service/api_key_service.go:26-45`). Available constructors in `internal/pkg/errors/types.go`: `BadRequest`, `Unauthorized`, `Forbidden`, `NotFound`, `Conflict`, `TooManyRequests`, `InternalServer`, `ServiceUnavailable`, `GatewayTimeout`, each paired with an `Is<Kind>(err)` predicate.

The `reason` string is a stable, machine-readable contract: the frontend reads it off the response envelope and uses it as an i18n lookup key (`src/utils/apiError.ts:31`). Treat reason codes as API surface — do not rename them casually.

**Go — wrapping and inspection:**
- Wrap with context using `%w`: 1854 of 3172 `fmt.Errorf` calls in `internal/` wrap. Preserve the chain so `ApplicationError` survives — `FromError` unwraps to find it.
- Compare with `errors.Is` / `errors.As`, never string matching (606 uses in `internal/`).

**Go — handler error responses:**
Handlers never build error envelopes by hand. They delegate to `internal/pkg/response`:

```go
key, err := h.apiKeyService.GetByID(c.Request.Context(), keyID)
if err != nil {
	response.ErrorFrom(c, err)   // maps ApplicationError -> status + reason + metadata
	return
}
response.Success(c, dto.APIKeyFromService(key))
```

- `response.ErrorFrom(c, err)` is the default path; it maps via `infraerrors.ToHTTP` and logs 5xx with `logredact.RedactText` applied to the message (`internal/pkg/response/response.go:82-96`).
- Use the direct helpers only for failures that never originate as a typed error: `response.Unauthorized(c, "User not authenticated")` when no auth subject is in context, `response.BadRequest(c, "Invalid key ID")` for malformed path/query params.
- Success helpers: `response.Success`, `response.Created`, `response.Accepted`, `response.Paginated`.
- Every handler returns immediately after writing a response. Bare `return` after the helper, never fallthrough.

**Response envelope** (`internal/pkg/response/response.go:15`) — `{code, message, reason?, metadata?, data?}`, with `code: 0` on success and the HTTP status echoed into `code` on error. Paginated payloads nest `{items, total, page, page_size, pages}`.

**Frontend:**
- The axios interceptor in `src/api/client.ts` rejects with a plain `{status, code, message, error, reason, metadata}` object, not an `AxiosError`.
- Extract user-facing text through `src/utils/apiError.ts` (`extractApiErrorCode`, `extractApiErrorMetadata`) rather than reading `err.response.data` inline. Backend `metadata` supplies i18n interpolation params.
- Surface failures via the app store toasts: `appStore.showError(...)`, `showSuccess`, `showWarning`, `showInfo`. Wrap async work in `appStore.withLoading(fn)` / `withLoadingAndError(fn, msg)` to couple the global spinner to the request.
- Guard `JSON.parse` of persisted state with `try`/`catch` and clear the bad key on failure (`src/stores/auth.ts:47-66`).

## Logging

**Framework — Go:** zap, wrapped by `internal/pkg/logger`. Access the global via `logger.L()` (153 call sites); do not construct loggers ad hoc. `logger.FromContext(ctx)` retrieves a request-scoped logger, and `logger.IntoContext` installs one.

**Patterns:**
```go
logger.L().Warn("gateway.web_search.account_wait_counter_increment_failed",
	zap.Int64("account_id", account.ID),
	zap.Error(waitErr),
)

logger.L().With(
	zap.String("component", "handler.openai_gateway.images"),
	zap.Int64("user_id", subject.UserID),
	zap.Int64("api_key_id", apiKey.ID),
).Info("...")
```

- Messages are **dotted lowercase event names**, not prose: `gateway.web_search.search_price_per_1k_explicit_free`. This keeps them groupable in the log sink.
- Always structured fields (`zap.String`, `zap.Int64`, `zap.Error`) — 433 `zap.String` and 200 `zap.Error` uses in `internal/`. Never `fmt.Sprintf` into the message.
- Attach a `component` field naming the layer and feature (`handler.gateway.web_search`) when using `.With(...)`.
- Set `logger.OpsSystemLogSkipField` on an event to keep it out of the DB-backed Ops system-log sink.
- 141 residual `log.Printf` calls exist in `internal/` (plus deliberate ones inside `response.ErrorFrom`). Prefer `logger.L()` in new code; `logger.LegacyPrintf(component, format, args...)` bridges old call sites.
- Redact before logging anything user-supplied or credential-adjacent: `internal/util/logredact.RedactText`.

**Frontend:** no logging framework. `console.*` only, sparingly. User-visible failures go to toasts, not the console.

## Comments

**When to Comment:**
- Explain *why*, not *what*. The codebase's comments are dense on rationale for non-obvious invariants and thin elsewhere.
- Document the consequence of getting it wrong. `APIKeyUpdateFields` in `internal/service/api_key_service.go` carries a multi-line comment explaining that unconditional full-row writes would clobber concurrently-incremented quota counters — that is the house style for a subtle constraint.
- Mark deprecated helpers explicitly and make misuse fail loudly. `testEntSQLTx` in `internal/repository/integration_harness_test.go:234` has a `Deprecated:` note and calls `t.Fatalf` in its body.
- Annotate exceptions to enforced rules where they exist, e.g. `// nolint:mnd` at the top of `internal/pkg/errors/types.go`.

**Language:** Chinese and English are both normal. 550 of 935 non-test Go files under `internal/` contain CJK text; comments explaining domain rules and pitfalls are usually Chinese, while exported-symbol doc comments are usually English. Mirror the file you are editing. Log messages, error reason codes, and identifiers stay ASCII.

**Go doc comments:**
- Package comment on the primary file of a package: `// Package handler provides HTTP request handlers for the application.` (`internal/handler/api_key_handler.go:1`), `// Package response provides standardized HTTP response helpers.`
- Exported types and constructors get a one-line doc comment starting with the identifier.
- Handlers document their route on the line after the description:
  ```go
  // List handles listing user's API keys with pagination
  // GET /api/v1/api-keys
  func (h *APIKeyHandler) List(c *gin.Context) {
  ```
- Struct fields document units and nil-vs-zero semantics inline: `Quota *float64 \`json:"quota"\` // 配额限制 (USD), 0=无限制`, `IPWhitelist *[]string // nil 不修改，空数组清空`. Pointer fields in update payloads almost always mean "absent = no change" — say so.
- `ST1000`/`ST1020`/`ST1021`/`ST1022` are disabled, so comment formatting is not lint-enforced. Follow the convention anyway.

**TSDoc (frontend):** JSDoc block at the top of every api module, store, and non-trivial util describing purpose:
```ts
/**
 * API Keys management endpoints
 * Handles CRUD operations for user API keys
 */
```
Exported functions in `src/api/**` document each `@param` and `@returns` (`src/api/keys.ts:9-16`). Non-obvious utils explain the reasoning, e.g. why `reason` is preferred over numeric `code` (`src/utils/apiError.ts:24-30`).

## Function Design

**Size:** Small and single-purpose. Extract predicates into named helpers rather than inlining conditions — `validAPIKeyLimit(v float64) bool` is a one-line function used by two validators (`internal/handler/api_key_handler.go:67`).

**Parameters:**
- Go: `ctx context.Context` first on anything doing I/O; pass `c.Request.Context()` from Gin handlers, never the `*gin.Context` itself, into services.
- Go: bundle 3+ related inputs into a named request struct (`service.CreateAPIKeyRequest`, `service.APIKeyListFilters`) instead of a long positional list.
- Go constructors take dependencies as interface-typed parameters, one per line when there are several (`NewAPIKeyService`, `internal/service/api_key_service.go:334`).
- Frontend: positional params with defaults for simple readers (`list(page = 1, pageSize = 10, filters?, options?)`), and an options object for cross-cutting concerns like `{ signal }` for `AbortSignal`.

**Return Values:**
- Go: `(value, error)` or `(value, meta, error)` — `List` returns `(keys, result, err)`.
- Go: two-result lookups return `(T, bool)` — `middleware2.GetAuthSubjectFromContext(c)`.
- Frontend: `async` functions return the unwrapped payload, not the axios response — destructure `const { data } = await apiClient.get<T>(...)` and return `data`.
- Frontend: return `undefined`/`null` for "not present" in extractors and narrow with early `if (!err || typeof err !== 'object') return undefined`.

**Slice/map preallocation (Go):** allocate with known capacity and append — `out := make([]dto.APIKey, 0, len(keys))`. When ranging to take addresses, index rather than copy: `for i := range keys { ... &keys[i] }` (`internal/handler/api_key_handler.go:143-146`).

## Module Design

**Exports — Go:**
- Everything lives under `backend/internal/`, so packages are private to the module by construction. Export only what crosses a package boundary.
- Layer boundaries are interface-based and **consumer-defined**: `internal/service` declares the interfaces it needs (`APIKeyRepository`, `APIKeyCache`, `ConcurrencyCache`) and `internal/repository` implements them. This is what makes the `depguard` ban on `service -> repository` workable. Adding a method to one of these interfaces means updating every test stub that implements it (`DEV_GUIDE.md:158`).
- Repository constructors return the interface or an unexported struct pointer (`newGroupRepositoryWithSQL`), keeping implementations unexported.
- Wiring is Google Wire: `internal/service/wire.go`, `cmd/server/wire_gen.go`. Regenerate with `cd backend && go generate ./cmd/server`.
- Ent is generated code — edit `ent/schema/*.go`, then `go generate ./ent`, and commit the generated output (`DEV_GUIDE.md:199-208`).

**Exports — Frontend:**
- Named exports throughout; default exports only for Vue SFCs and a few api modules (`announcements`, `client`).
- `<script setup lang="ts">` in 299 of 302 `.vue` files. No Options API anywhere — do not introduce `export default { ... }` components.
- Props via `defineProps` (212 files), often with `withDefaults` (65 files); events via `defineEmits` (143 files).
- Styling is Tailwind utility classes in the template. Only 59 of 302 components have a `<style>` block, and those use `<style scoped>`. Reach for the project's design-system utility classes (`card`, `card-header`, `card-body`, `btn btn-secondary`, `page-header`, `tab`/`tab-active`, `badge badge-warning`) before writing custom CSS — `src/features/channel-monitor-v2/__tests__/designSystem.structure.spec.ts` asserts their presence in specific shells and will fail if a component is reskinned with ad-hoc styles.
- Add `data-testid` attributes to elements that tests need to select (40 components do this) rather than depending on class names or DOM position.

**Barrel Files:** used at each public boundary — `src/api/index.ts`, `src/api/admin/index.ts`, `src/types/index.ts`, `src/stores/index.ts`, `src/components/{common,layout,icons,account}/index.ts`, `src/i18n/locales/{en,zh}/index.ts`. The locale barrels aggregate domain modules by object spread, which silently drops duplicate top-level keys; `src/i18n/__tests__/localesNoKeyCollision.spec.ts` guards this, so adding a locale module means checking for top-level key overlap.

**i18n:** every user-facing string goes through `vue-i18n`. Locale trees live in `src/i18n/locales/en/**` and `src/i18n/locales/zh/**` and must stay in structural parity — the specs in `src/i18n/__tests__/` assert key parity, message compilation, and absence of collisions. Never hardcode display text in a component.

## Pre-Commit Checklist

From `DEV_GUIDE.md:235-244`, verify locally before opening a PR:

```bash
cd backend && go test -tags=unit ./...          # unit tests
cd backend && go test -tags=integration ./...   # integration tests (needs Docker)
cd backend && golangci-lint run ./...           # lint, v2.9 to match CI
cd frontend && pnpm lint:check && pnpm typecheck
```

- `pnpm-lock.yaml` regenerated and committed if `package.json` changed.
- All test stubs updated if a service interface gained a method.
- `ent/` regenerated and committed if a schema changed.
- Go version stays at 1.26.6 across `backend/go.mod`, three workflows, and three Dockerfiles (`DEV_GUIDE.md:56`) — the Dockerfile copies fail silently in CI and only break at image build time.

---

*Convention analysis: 2026-08-23*
