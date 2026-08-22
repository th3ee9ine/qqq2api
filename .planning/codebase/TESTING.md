# Testing Patterns

**Analysis Date:** 2026-08-23

Two independent test stacks: Go `testing` + testify for `backend/` (1176 test files), Vitest + Vue Test Utils for `frontend/` (239 spec files). Both are gated by `.github/workflows/backend-ci.yml`.

## Test Framework

**Backend runner:**
- Go standard `testing` package, Go 1.26.6 (pinned; `backend/go.mod`)
- Config: build tags, not a config file — see "Build Tags" below
- Orchestration: `backend/Makefile`

**Backend assertion library:**
- `github.com/stretchr/testify` v1.11.1 — imported by 990 of 1176 test files
- `testify/require` is the default (972 files). `testify/assert` is rare (53 files) — use `require` unless a test must keep going after a failed check.
- `testify/suite` for stateful DB tests (22 files)
- No mock framework: `testify/mock` and `gomock` are absent. Stubs are hand-written.

**Frontend runner:**
- Vitest 2.1.9, `environment: 'jsdom'`, `globals: true`
- Config: `frontend/vitest.config.ts`
- Setup file: `frontend/src/__tests__/setup.ts`
- Component harness: `@vue/test-utils` 2.4.6

**Run Commands:**
```bash
# From repo root (Makefile)
make test                     # backend + frontend
make test-backend             # go test ./... + golangci-lint run ./...
make test-frontend            # eslint + vue-tsc + critical vitest subset
make test-frontend-critical   # only the FRONTEND_CRITICAL_VITEST list

# Backend (cd backend)
make test-unit                # go test -tags=unit ./...
make test-integration         # go test -tags=integration ./...   (requires Docker)
make test-e2e-local           # go test -tags=e2e -v -timeout=300s ./internal/integration/...
golangci-lint run ./...

# Frontend (cd frontend)
pnpm test                     # vitest watch
pnpm test:run                 # vitest run, full suite
pnpm test:coverage            # vitest run --coverage
pnpm exec vitest run src/api/__tests__/client.spec.ts   # single file
```

`make test-e2e` in `backend/Makefile:26` invokes `./scripts/e2e-test.sh`, which is not present in `backend/scripts/` (only `resolve-version.sh` and a SQL file). Use `make test-e2e-local` instead.

No `-race` flag anywhere in the Makefiles or workflows. Add it manually when investigating a concurrency bug: `go test -race -tags=unit ./internal/service/`.

## Build Tags (backend — read this first)

Every backend test file carries a build tag on line 1, before the package clause. This is the single most important backend testing convention: an untagged file will not run under `make test-unit` or `make test-integration`, so it silently never executes in CI.

```go
//go:build unit

package service
```

| Tag | Files | Meaning |
|-----|-------|---------|
| `unit` | 417 | Pure in-process, no Docker, no network. Runs on every push. |
| `integration` | 76 | Needs Docker (Postgres + Redis testcontainers). Runs on every push. |
| `e2e` | 3 | Hits a running server, needs real API keys or `E2E_MOCK=true`. Not in CI. |

677 test files carry no tag at all. Some are intentional (`migrations/*_test.go` assert on embedded SQL text and run under the default `go test ./...` in `make test`), but many under `internal/service/` and `internal/handler/dto/` appear to be omissions — for example `internal/service/account_rpm_test.go`, `internal/service/account_openai_passthrough_test.go`, and `internal/handler/dto/credentials_redact_test.go`. **Add `//go:build unit` to any new pure test file** so CI picks it up.

Verify a new test actually runs:
```bash
cd backend && go test -tags=unit -run TestMyNewThing -v ./internal/service/
```

## Test File Organization

**Backend location:** co-located with the code under test, in the same package. `internal/service/account_rpm_test.go` tests `internal/service/account.go` and declares `package service`, giving direct access to unexported identifiers. No `_test` package suffix anywhere.

**Backend naming:** `<subject>_<aspect>_test.go`, one narrow behaviour per file. Common suffixes across the tree: `_integration_test.go` (78), `_service_test.go` (59), `_handler_test.go` (30), `_cache_test.go` (19), `_billing_test.go` (14), `_unit_test.go` (13), `_benchmark_test.go` (12). Prefer a new focused file over appending to a large one — `internal/service/` alone holds 642 test files.

**Test distribution:**
```
internal/service/            642
internal/repository/         177
internal/handler/             99
internal/handler/admin/       65
internal/pkg/apicompat/       26
internal/server/middleware/   23
migrations/                   10
```

**Frontend location:** `__tests__/` subdirectory beside the code under test.
```
src/api/__tests__/client.spec.ts              -> src/api/client.ts
src/stores/__tests__/auth.spec.ts             -> src/stores/auth.ts
src/components/user/profile/__tests__/ProfileInfoCard.spec.ts
src/views/admin/__tests__/SettingsView.spec.ts
src/__tests__/integration/navigation.spec.ts  -> cross-module integration
```

**Frontend naming:** `<Subject>.spec.ts` matching the subject's casing (`ProfileInfoCard.spec.ts`, `client.spec.ts`). A second dotted segment scopes a variant: `ChannelStatusView.mode.spec.ts`, `designSystem.structure.spec.ts`. `include` is `src/**/*.{test,spec}.{js,ts,jsx,tsx}` — both work, `.spec.ts` dominates.

`tsconfig.json` excludes `src/**/__tests__/**` and `*.spec.ts` from `vue-tsc`, so test files are not typechecked by `pnpm typecheck`. Type errors in specs surface only when Vitest runs them.

## Test Structure

**Backend — table-driven subtests are the default** (487 files use `t.Run`):

```go
func TestGetBaseRPM(t *testing.T) {
	tests := []struct {
		name     string
		extra    map[string]any
		expected int
	}{
		{"nil extra", nil, 0},
		{"int value", map[string]any{"base_rpm": 15}, 15},
		{"string value", map[string]any{"base_rpm": "15"}, 15},
		{"negative value", map[string]any{"base_rpm": -5}, 0},
		{"json.Number value", map[string]any{"base_rpm": json.Number("25")}, 25},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &Account{Extra: tt.extra}
			if got := a.GetBaseRPM(); got != tt.expected {
				t.Errorf("GetBaseRPM() = %d, want %d", got, tt.expected)
			}
		})
	}
}
```

Case names are short lowercase descriptions, often grouped by comment (`// floor 生效`, `// 手动 override`). Cover the degenerate inputs explicitly: nil, missing key, zero, negative, wrong type, extreme value. `internal/service/account_rpm_test.go` is the reference file.

**Backend — single-behaviour tests** name the scenario and expectation in the function name, and open with a comment stating the invariant:

```go
func TestUserAvailableChannel_Unauthenticated401(t *testing.T) {
	// 没有 AuthSubject 注入时，handler 应返回 401 且不触达 service 依赖。
	gin.SetMode(gin.TestMode)
	h := &AvailableChannelHandler{} // nil services — 401 路径不会调用它们
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channels/available", nil)

	h.List(c)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}
```

Naming is `Test<Subject>_<Scenario><Expectation>`: `TestFilterUserVisibleGroups_IntersectionOnly`, `TestToUserSupportedModels_NilAllowedPlatformsKeepsAll`. Passing `nil` dependencies is a deliberate assertion that the path under test never reaches them.

**Backend — suites for DB-backed tests** (`testify/suite`, 22 files):

```go
type GroupRepoSuite struct {
	suite.Suite
	ctx  context.Context
	tx   *dbent.Tx
	repo *groupRepository
}

func (s *GroupRepoSuite) SetupTest() {
	s.ctx = context.Background()
	tx := testEntTx(s.T())        // auto-rolled-back transaction
	s.tx = tx
	s.repo = newGroupRepositoryWithSQL(tx.Client(), tx)
}

func TestGroupRepoSuite(t *testing.T) {
	suite.Run(t, new(GroupRepoSuite))
}

func (s *GroupRepoSuite) TestCreate() {
	group := &service.Group{Name: "test-create", Platform: service.PlatformAnthropic, /* ... */}
	err := s.repo.Create(s.ctx, group)
	s.Require().NoError(err, "Create")
	s.Require().NotZero(group.ID, "expected ID to be set")
}
```

Inside a suite use `s.Require()`, not the package-level `require`. Full example: `internal/repository/group_repo_integration_test.go`.

**Backend cleanup:** `t.Cleanup(func() { ... })` over `defer` for resources that outlive the immediate scope. Mark helpers with `t.Helper()` so failures report the caller's line.

**Parallelism:** `t.Parallel()` in 105 files. Safe for pure computation; do not add it to tests sharing the integration Postgres/Redis singletons.

**Frontend — nested describe with Chinese or English case names:**

```ts
describe('useAuthStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    vi.useFakeTimers()
    vi.clearAllMocks()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  // --- login ---

  describe('login', () => {
    it('成功登录后设置 token 和 user', async () => {
      mockLogin.mockResolvedValue(fakeAuthResponse)
      // ...
    })
  })
})
```

Outer `describe` names the subject, inner `describe` names the method or concern, `it` states the expected behaviour. `// --- section ---` comments separate concern groups. Reference: `src/stores/__tests__/auth.spec.ts`, `src/api/__tests__/client.spec.ts`.

## Mocking

**Backend framework:** none. Hand-written stubs implementing the consumer-defined interfaces from `internal/service`.

**Pattern — struct of canned results plus call recorders:**

```go
// stubConcurrencyCacheForTest 用于并发服务单元测试的缓存桩
type stubConcurrencyCacheForTest struct {
	acquireResult  bool
	acquireErr     error
	concurrency    int
	concurrencyErr error

	// 记录调用
	releasedAccountIDs []int64
	releasedRequestIDs []string
	loadBatchCalls     atomic.Int64
}

// 编译期接口断言 — 接口新增方法时立刻编译失败
var _ ConcurrencyCache = (*stubConcurrencyCacheForTest)(nil)

func (c *stubConcurrencyCacheForTest) AcquireAccountSlot(_ context.Context, _ int64, _ int, _ string) (bool, error) {
	return c.acquireResult, c.acquireErr
}

func (c *stubConcurrencyCacheForTest) ReleaseAccountSlot(_ context.Context, accountID int64, requestID string) error {
	c.releasedAccountIDs = append(c.releasedAccountIDs, accountID)
	c.releasedRequestIDs = append(c.releasedRequestIDs, requestID)
	return c.releaseErr
}
```

Rules this encodes:
- Always add the compile-time assertion `var _ Iface = (*stubX)(nil)`. Without it, an interface gaining a method fails somewhere confusing instead of at the stub.
- Unused parameters are `_`.
- Add a `...Fn func(...)` field when a case needs per-call behaviour, and fall back to the canned value when nil — see `ingressLeaseCacheForTest` in `internal/service/concurrency_service_test.go:48-85`.
- Embed a base stub to extend it for an optional interface (`ingressLeaseCacheForTest` embeds `stubConcurrencyCacheForTest`).

Reference: `internal/service/concurrency_service_test.go`. Naming is `stub<Iface>ForTest` / `fake<Thing>` / `mock<Thing>` — all three prefixes are in use (43 `stub`, 49 `fake`, 34 `mock`); stubs are declared locally in the test file that needs them, near the top or just above their tests.

**Interface change checklist** (`DEV_GUIDE.md:158-172`): adding a method to a service interface breaks every stub. Find them with:
```bash
cd backend
grep -rn 'type stub.*struct\|type fake.*struct\|type mock.*struct' internal/
```

**`internal/testutil` — exists but unused.** It provides `StubConcurrencyCache`, `StubGatewayCache`, `StubSessionLimitCache` (`stubs.go`), fixture builders `NewTestUser`/`NewTestAccount`/`NewTestAPIKey`/`NewTestGroup` (`fixtures.go`), and `NewGinTestContext` (`httptest.go`), all under `//go:build unit`. Zero files import it — every test defines its own stubs instead. Either adopt it deliberately or keep matching the local-stub pattern; do not assume it is the house standard.

**Redis — `miniredis` for in-process fake** (15 files):
```go
redisServer := miniredis.RunT(t)   // auto-cleanup via t
client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
cache := NewConcurrencyCache(client, 15, 900)
```
`miniredis.RunT(t)` registers its own cleanup. Reference: `internal/repository/concurrency_cache_live_test.go`, `internal/repository/image_task_store_test.go`.

**SQL — `go-sqlmock` for asserting exact queries** (28 files):
```go
db, mock, err := sqlmock.New()
require.NoError(t, err)
t.Cleanup(func() { _ = db.Close() })

mock.ExpectQuery(`(?s)UPDATE accounts.*RETURNING id`).
	WithArgs(now).
	WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)).AddRow(int64(29)))
mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)")).
	WithArgs(service.SchedulerOutboxEventAccountBulkChanged, nil, nil, accountIDsPayloadMatcher{want: []int64{11, 29}}).
	WillReturnResult(sqlmock.NewResult(1, 1))

repo := newAccountRepositoryWithSQL(nil, db, nil)
updated, err := repo.AutoPauseExpiredAccounts(context.Background(), now)

require.NoError(t, err)
require.EqualValues(t, 2, updated)
require.NoError(t, mock.ExpectationsWereMet())   // always assert this
```
Use `regexp.QuoteMeta` for literal SQL and `(?s)...` regex for shape matching. Custom `sqlmock.Argument` matchers handle structured payloads (`accountIDsPayloadMatcher`, `internal/repository/account_repo_auto_pause_test.go:20-33`). Always close with `mock.ExpectationsWereMet()`. Reference: `internal/repository/account_repo_auto_pause_test.go`.

**HTTP upstreams:** `net/http/httptest` in 309 test files — spin up a real local server rather than stubbing an HTTP client.

**Gin handlers:**
```go
gin.SetMode(gin.TestMode)
w := httptest.NewRecorder()
c, _ := gin.CreateTestContext(w)
c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/channels/available", nil)
h.List(c)
require.Equal(t, http.StatusUnauthorized, w.Code)
```

**Frontend framework:** Vitest `vi.mock` / `vi.fn` (151 spec files use `vi.mock`).

**Pattern — hoisted module mock with outer spies** so assertions can reach the mock:
```ts
const mockLogin = vi.fn()
const mockLogout = vi.fn()

vi.mock('@/api', () => ({
  authAPI: {
    login: (...args: any[]) => mockLogin(...args),
    logout: (...args: any[]) => mockLogout(...args),
  },
  isTotp2FARequired: (response: any) => response?.requires_2fa === true,
}))
```
`vi.mock` is hoisted above imports, so the factory cannot close over a `const` declared later unless it is referenced lazily like this.

**Pattern — partial mock preserving the real module:**
```ts
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string>) => {
        if (key === 'profile.accountBalance') return 'Account Balance'
        return key   // fall through to the key itself
      }
    })
  }
})
```
Returning the key unchanged for unmapped lookups keeps assertions readable. Reference: `src/components/user/profile/__tests__/ProfileInfoCard.spec.ts:25-50`.

**Pattern — module state reset for singletons.** `src/api/client.ts` builds its axios instance at import time, so tests re-import per case:
```ts
beforeEach(async () => {
  localStorage.clear()
  window.history.replaceState({}, '', '/')
  vi.resetModules()
  const mod = await import('@/api/client')
  apiClient = mod.apiClient
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllEnvs()
})
```
Env stubbing uses `vi.stubEnv('VITE_API_BASE_URL', 'api/v1')` and must be paired with `vi.unstubAllEnvs()`.

**Pattern — axios adapter injection** instead of mocking axios itself:
```ts
const adapter = vi.fn().mockResolvedValue({
  status: 200, data: { code: 0, data: {} }, headers: {}, config: {}, statusText: 'OK',
})
apiClient.defaults.adapter = adapter

await apiClient.get('/test')

const config = adapter.mock.calls[0][0]
expect(config.headers.get('Authorization')).toBe('Bearer my-jwt-token')
```
This exercises the real interceptor chain. Reference: `src/api/__tests__/client.spec.ts`.

**Pinia:**
- Store tests: `setActivePinia(createPinia())` in `beforeEach` (`src/stores/__tests__/auth.spec.ts:56`).
- Component tests: pass a fresh pinia through mount options, `global: { plugins: [pinia] }` (88 spec files use a `global:` block).
- `@pinia/testing` / `createTestingPinia` is not installed — use real stores with mocked API modules.

**What to Mock:**
- Outbound API modules (`@/api`, `@/api/admin`) and the composables that wrap them
- `vue-router` (`useRoute`, `useRouter`) and `vue-i18n`'s `useI18n`
- Stores a component consumes but does not exercise (`@/stores/auth`, `@/stores/app`)
- Browser APIs jsdom lacks — already handled globally in `src/__tests__/setup.ts`: `localStorage`, `matchMedia`, `IntersectionObserver`, `ResizeObserver`, `requestIdleCallback`/`cancelIdleCallback`
- Backend: interfaces at the layer boundary — repositories and caches when testing services

**What NOT to Mock:**
- The subject under test, or pure helpers in `src/utils/`
- The axios interceptor chain — inject an adapter instead
- Redis: use `miniredis`. Postgres: use the testcontainers harness. Both are real enough to catch protocol and SQL errors a mock would hide.
- Backend: no mocking of concrete structs — depend on the interface

## Fixtures and Factories

**Backend — options-func builders** (`internal/testutil/fixtures.go`, currently unimported but the intended shape):
```go
func NewTestUser(opts ...func(*service.User)) *service.User {
	u := &service.User{
		ID: 1, Email: "test@example.com", Username: "testuser",
		Role: "user", Balance: 100.0, Concurrency: 5,
		Status: service.StatusActive,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	for _, opt := range opts {
		opt(u)
	}
	return u
}
```

In practice most backend tests construct structs inline with only the fields the assertion cares about — `&Account{Extra: tt.extra}`, `&service.Group{Name: "test-create", Platform: service.PlatformAnthropic, Status: service.StatusActive}`. Prefer that for narrow unit tests; use a builder when the same shape recurs across many cases in a file, declared locally as an unexported helper.

**Frontend — module-level `fake*` consts spread into variants:**
```ts
const fakeUser = { id: 1, username: 'testuser', email: 'test@example.com', role: 'user' as const, /* ... */ }
const fakeAdminUser = { ...fakeUser, id: 2, username: 'admin', role: 'admin' as const }
```

**Frontend — typed `create*` factory with overrides** for component props:
```ts
function createUser(overrides: Partial<User> = {}): User {
  return {
    id: 5, username: 'alice', email: 'alice@example.com', avatar_url: null,
    role: 'user', balance: 10, concurrency: 2, status: 'active',
    created_at: '2026-04-20T00:00:00Z', updated_at: '2026-04-20T00:00:00Z',
    ...overrides
  }
}
```
Return the real domain type so a schema change surfaces as a type error in the factory. Reference: `src/components/user/profile/__tests__/ProfileInfoCard.spec.ts:52-70`.

**Location:** fixtures live in the spec file that uses them. There is no shared frontend fixtures module.

## Integration Test Harness (backend)

`internal/repository/integration_harness_test.go` owns a package-level `TestMain` that boots real Postgres and Redis containers once for all `-tags=integration` tests in the package.

**What it does:**
- `timezone.Init("UTC")` before anything else
- Checks `docker info`. Docker missing + `CI` unset → `os.Exit(0)` (skip quietly). Docker missing + `CI` set → `os.Exit(1)` (fail loudly). A green local run therefore does not prove integration tests passed — confirm Docker is running.
- Starts `postgres:18.1-alpine3.23` (db `sub2api_test`, user/pass `postgres`) and `redis:8.4-alpine` via `testcontainers-go`
- Applies real migrations with `ApplyMigrations(ctx, integrationDB)` — the suite runs against the actual schema
- Exposes `integrationDB *sql.DB`, `integrationEntClient *dbent.Client`, `integrationRedis *redis.Client`

**Helpers:**

| Helper | Isolation | Use for |
|--------|-----------|---------|
| `testEntTx(t)` | ent tx, auto-rollback via `t.Cleanup` | Default. Tests needing isolation. |
| `testEntClient(t)` | none — writes commit | Code that manages its own transactions internally (`Create`/`Update`) |
| `testTx(t)` | `*sql.Tx`, auto-rollback | Raw SQL assertions |
| `testRedis(t)` | namespaced key prefix | Redis-backed repositories |
| `testEntSQLTx(t)` | — | **Deprecated, calls `t.Fatalf`.** Do not use. |

**Postgres version matrix:** override the image to check SQL that behaves differently across majors (e.g. jsonpath `.datetime()` only accepts the ISO-8601 `Z` designator from PG 17 on):
```bash
SUB2API_TEST_POSTGRES_IMAGE=postgres:15-alpine go test -tags integration ./internal/repository/
```

Other packages run their own containers: `internal/middleware/rate_limiter_integration_test.go`, `internal/server/routes/auth_rate_limit_integration_test.go`. `internal/securityaudit/prompt_repository_integration_test.go` reads a DSN from `PROMPT_AUDIT_TEST_POSTGRES_DSN` instead.

## Coverage

**Backend:** no threshold enforced, no coverage step in CI. Generate locally:
```bash
cd backend && go test -tags=unit -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

**Frontend:** thresholds are declared at 80% for statements, branches, functions, and lines (`frontend/vitest.config.ts:30-37`), provider `v8`, reporters `text`/`json`/`html`. These are **not enforced in CI** — the `frontend` job runs `make test-frontend`, which runs lint, typecheck, and the 13-file critical subset, never `pnpm test:coverage`. Coverage excludes `src/main.ts`, `*.d.ts`, and spec files.
```bash
cd frontend && pnpm test:coverage
```

## CI Gates

`.github/workflows/backend-ci.yml` runs four jobs on every push and PR:

| Job | Runner | Runs |
|-----|--------|------|
| `shell` | macos-15 | `bash -n deploy/apple-container.sh` plus `deploy/tests/*.sh` and `deploy/test-caddyfile-cache.sh` |
| `test` | ubuntu-latest | Go version assertion, then `make test-unit` and `make test-integration` |
| `frontend` | ubuntu-latest | `pnpm install --frozen-lockfile`, then `make test-frontend` |
| `golangci-lint` | ubuntu-latest | `golangci-lint` v2.9, `--timeout=30m`, working-directory `backend` |

Every Go job hard-asserts `go version | grep -q 'go1.26.6'`.

**The frontend gate runs only 13 of 239 spec files.** `FRONTEND_CRITICAL_VITEST` in `Makefile:3-16` lists them: `src/api/__tests__/{client,tokenRefresh,channelMonitorV2}.spec.ts`, `src/views/auth/__tests__/{LinuxDoCallbackView,WechatCallbackView}.spec.ts`, `src/views/user/__tests__/{PaymentView,PaymentResultView,ChannelStatusView.mode}.spec.ts`, `src/components/user/profile/__tests__/ProfileInfoCard.spec.ts`, `src/views/admin/__tests__/SettingsView.spec.ts`, and three under `src/features/channel-monitor-v2/__tests__/`. A regression in any other spec will not fail CI — run `pnpm test:run` locally before pushing frontend changes, and add genuinely load-bearing specs to that list.

`.github/workflows/security-scan.yml` adds `govulncheck ./...` on the backend and `pnpm audit --prod --audit-level=high` on the frontend, filtered through `tools/check_pnpm_audit_exceptions.py` against `.github/audit-exceptions.yml`. Runs on push, PR, and Mondays at 03:00 UTC.

## Test Types

**Unit (backend, `-tags=unit`):** in-process, hand-written stubs, no containers. Covers service logic, handler request/response shape, DTO mapping, pure helpers in `internal/pkg/**`.

**Unit (frontend):** stores, composables, utils, api modules, and single components mounted with mocked dependencies.

**Integration (backend, `-tags=integration`):** repository methods against real Postgres and Redis in testcontainers, exercising real migrations. Also rate limiting middleware and route-level auth throttling.

**Integration (frontend):** three specs in `src/__tests__/integration/` — `navigation.spec.ts`, `data-import.spec.ts`, `proxy-data-import.spec.ts` — crossing router, stores, and components.

**E2E (backend, `-tags=e2e`):** `internal/integration/e2e_{gateway,user_flow,helpers}_test.go`, driving a running server over HTTP.
- `BASE_URL` (default `http://localhost:8080`), `ENDPOINT_PREFIX`
- `CLAUDE_API_KEY` / `GEMINI_API_KEY`; without either, `skipIfNoRealAPI(t)` calls `t.Skip`
- `E2E_MOCK=true` substitutes local mock responses so the request/response flow can be verified without real keys
- Keys are truncated to 8 chars before logging via `safeLogKey` — follow that when adding logging to an E2E test

Browser E2E (Playwright, Cypress) is not used.

**Structure-contract tests (frontend):** a distinctive local pattern — specs that read source files off disk and assert on their text, guarding conventions a runtime test cannot reach:
```ts
const root = resolve(__dirname, '../../..')
function read(rel: string) { return readFileSync(resolve(root, rel), 'utf8') }

it('user ChannelStatus V2 shell uses page-header, card, btn, tabs utilities', () => {
  const src = read('views/user/ChannelStatusV2View.vue')
  expect(src).toContain('page-header')
  expect(src).toContain('btn btn-secondary')
  expect(src).not.toMatch(/min-w-\[980px\]/)
  expect(src.indexOf('summaryAria')).toBeLessThan(src.indexOf('MonitorTrendChart'))
})
```
Reference: `src/features/channel-monitor-v2/__tests__/designSystem.structure.spec.ts`. Two of these are in the CI critical list, so reskinning or reordering those components will fail CI until the spec is updated deliberately.

**i18n guard tests:** `src/i18n/__tests__/` holds eight specs asserting locale-tree health — `localesNoKeyCollision.spec.ts` (top-level key collisions across spread-merged modules), `localesMessageCompile.spec.ts` (every message compiles), plus per-domain key-presence checks (`opsLocaleKeys`, `riskControlLocales`, `ipGeoLocales`, `usageServiceTierLocales`, `openaiFastPolicyLocales`, `wsModeLocaleDesc`). Adding a locale module means adding it to the collision spec's import list.

**Migration text tests:** `backend/migrations/*_test.go` read migration SQL from the embedded `FS` and assert on normalized text:
```go
content, err := FS.ReadFile("174_add_usage_logs_api_key_latest_ip_index_notx.sql")
require.NoError(t, err)
sql := strings.Join(strings.Fields(string(content)), " ")
require.Contains(t, sql, "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_api_key_latest_ip")
```
These carry no build tag, so they run under plain `go test ./...` (i.e. `make test`) but not under `make test-unit`.

**Benchmarks:** 63 `Benchmark*` functions across 12 `*_benchmark_test.go` files, concentrated in `internal/service/` (gateway scheduling, JSON handling) and `internal/repository/` (concurrency cache, HTTP upstream). Not run in CI:
```bash
cd backend && go test -tags=unit -bench=. -benchmem -run=^$ ./internal/service/
```

No fuzz tests (`func Fuzz*`) and no `Example*` functions exist.

## Common Patterns

**Backend — async and timing:**
```go
// Redis TTL expiry with miniredis: advance the fake clock, do not sleep
redisServer := miniredis.RunT(t)
redisServer.FastForward(31 * time.Second)

// Concurrent call counting
loadBatchCalls atomic.Int64   // in the stub
require.EqualValues(t, 1, stub.loadBatchCalls.Load())
```
Poll with a deadline rather than a fixed `time.Sleep` when waiting on a goroutine.

**Backend — error assertions:**
```go
require.NoError(t, err)
require.ErrorIs(t, err, service.ErrAPIKeyNotFound)      // sentinel ApplicationError
require.ErrorContains(t, err, "invalid quota")           // message substring

// HTTP-level: assert the status the ApplicationError maps to
require.Equal(t, http.StatusUnauthorized, w.Code)
```
Prefer `require.ErrorIs` against the exported `Err*` sentinel over string matching — `ApplicationError.Is` compares code and reason, so it survives message changes.

**Backend — response shape assertions.** Serialize the DTO and assert on the JSON to pin down the field whitelist, so an accidentally exported admin field fails the test:
```go
raw, err := json.Marshal(row)
require.NoError(t, err)
// assert on decoded keys
```
Reference: `TestUserAvailableChannel_FieldWhitelist`, `internal/handler/available_channel_handler_test.go:66`.

**Frontend — async:**
```ts
// Let the microtask queue and Vue's render queue drain
import flushPromises from 'flush-promises'   // or: await Promise.resolve()
await flushPromises()          // 71 spec files
await wrapper.vm.$nextTick()

// Fake timers for debounce, polling, token-refresh windows (27 spec files)
beforeEach(() => { vi.useFakeTimers() })
afterEach(() => { vi.useRealTimers() })
await vi.advanceTimersByTimeAsync(1000)
```
Always restore real timers in `afterEach` — a leaked fake clock breaks unrelated specs in the same file.

**Frontend — error testing:**
```ts
await expect(store.login({ username: 'u', password: 'bad' })).rejects.toThrow()
await expect(fn()).rejects.toMatchObject({ status: 401, reason: 'UNAUTHORIZED' })
```
75 assertions across the suite use the `rejects` / `await expect(...)` form.

**Frontend — component selection.** Prefer `data-testid` over classes or DOM position:
```ts
expect(wrapper.get('[data-testid="profile-basics-panel"]').exists()).toBe(true)
expect(wrapper.text()).toContain('alice@example.com')
```
Stub leaf components you do not want to render: `global: { stubs: { Icon: true } }`.

**Frontend — global setup already in place** (`src/__tests__/setup.ts`, do not re-mock these per file): in-memory `localStorage`, `matchMedia` returning `matches: true` (tests render the desktop branch by default), `IntersectionObserver`, `ResizeObserver`, `requestIdleCallback`/`cancelIdleCallback`, and a 10s default `testTimeout`. Override `matchMedia` locally when asserting mobile layout.

## Verification Status

`make test-unit` and the frontend suite could not be executed in this environment: the local toolchain is Go 1.25.7 while `backend/go.mod` requires 1.26.6, and the automatic toolchain download fails with `GOSUMDB=off`. `frontend/node_modules` is not installed. All patterns above are read from source; none were confirmed by a run.

---

*Testing analysis: 2026-08-23*
