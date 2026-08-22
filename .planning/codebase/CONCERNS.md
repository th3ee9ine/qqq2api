# Codebase Concerns

**Analysis Date:** 2026-08-23

Scope: full repo. Backend is Go (~372K LOC non-test in `backend/internal`, plus ~267K LOC generated in `backend/ent`); frontend is Vue 3 + TypeScript (~232K LOC in `frontend/src`).

Overall the codebase is unusually disciplined for its size: 13 `TODO`/`FIXME` markers across 372K LOC of hand-written Go, 36 `nolint` directives (each with a justifying comment), 4 `panic()` calls in non-test code, parameterized SQL throughout, and an architecture boundary enforced by `depguard` in `backend/.golangci.yml`. The concerns below are the genuine exceptions, ordered roughly by consequence.

## Tech Debt

**Live (realtime) sessions bypass billing entirely:**
- Issue: `finalizeLiveCall` writes a usage log with `TotalCost`/`ActualCost` permanently zero and never routes through `recordUsageCore`/`applyUsageBilling`. The in-code comment states the consequence directly: under balance mode a user with near-zero balance can repeatedly open sessions up to `liveMaxSessionDuration`.
- Files: `backend/internal/service/openai_live.go:827` (the `TODO(billing)` block)
- Impact: unbilled compute on the Live path. The zero-value behaviour is *locked in by a test* (`TestFinalizeLiveCallIsIdempotentAndWritesZeroUsage`), so it is currently a deliberate, tested state rather than an accident — but the comment records that the product decision was never made.
- Fix approach: decide whether Live is intentionally free. If billed, hook the duration-based cost into the existing billing pipeline at this call site and update the locking test. If intentionally free, delete the TODO comment (the comment itself says this is the resolution). Do not change this without an explicit product decision.

**Legacy AES ciphertext fallback in payment provider configs:**
- Issue: provider configs are now stored as plaintext JSON, but both readers retain an AES-256-GCM decryption fallback for pre-migration records, calling a deprecated `Decrypt` behind `//nolint:staticcheck`. Unreadable values are silently coerced to empty so the service stays up.
- Files: `backend/internal/payment/load_balancer.go:323` (`decryptConfig`), `backend/internal/service/payment_config_providers.go:498` (`decryptConfig`)
- Impact: duplicated shim in two places; a legacy record with a missing/invalid `TOTP_ENCRYPTION_KEY` degrades to an empty config, which surfaces as a misconfigured payment provider rather than a hard error. Both sites log `payment provider config unreadable, treating as empty for re-entry`.
- Fix approach: as both TODOs state — remove the fallback branch, the `encryptionKey` field, and the `Decrypt` import once deployments have re-saved configs through the UI. Coordinate both files in one change; they are intentionally parallel implementations.

**Stub endpoints returning hardcoded zeros on live routes:**
- Issue: three implementations return fixed placeholder values while being wired into the router / service interface.
  - `GET /api/v1/admin/groups/:id/stats` returns `total_api_keys: 0, active_api_keys: 0, total_requests: 0, total_cost: 0.0` with a literal `// Return mock data for now` comment.
  - `RedeemService.GetStats` returns an all-zero map.
  - `RefreshAccountCredentials` fetches the account and returns it unchanged without refreshing anything.
- Files: `backend/internal/handler/admin/group_handler.go:734`, `backend/internal/service/redeem_service.go:642`, `backend/internal/service/admin_account.go:1228`
- Impact: The group stats route is reachable and typed in the frontend client at `frontend/src/api/admin/groups.ts:259`, so any caller receives plausible-looking zeros rather than an error — silently wrong data, which is worse than a 501. `RefreshAccountCredentials` is declared in the interface (`backend/internal/service/admin_service.go:93`) but has no route wired, so it is currently unreachable dead code.
- Fix approach: implement against the existing dashboard aggregation services (`GetUsageSummary` in the same handler already does real work via `dashboardService.GetGroupUsageSummary` and is a good model), or return an explicit unimplemented error so callers fail loudly. Delete `RefreshAccountCredentials` if the capability is not planned.

**Very large single files:**
- Issue: several files far exceed comfortable review/merge size.
  - `frontend/src/views/admin/SettingsView.vue` — 12,999 lines
  - `frontend/src/views/admin/GroupsView.vue` — 6,843 lines
  - `frontend/src/components/account/CreateAccountModal.vue` — 6,830 lines
  - `backend/internal/repository/account_repo.go` — 3,822 lines
  - `backend/internal/config/config.go` — 3,816 lines (67 `*Config` structs, 493 `viper.SetDefault` calls)
  - `backend/internal/service/gemini_messages_compat_service.go` — 3,717 lines
  - `backend/internal/handler/openai_gateway_handler.go` — 3,710 lines
- Impact: high merge-conflict probability on shared files, slow editor/typecheck feedback, and difficulty reasoning about a change's blast radius. `SettingsView.vue` in particular is a single-file admin surface for the entire 493-default config space.
- Fix approach: split by feature section rather than mechanically by line count. `SettingsView.vue` should decompose into per-section child components — note that `frontend/src/views/admin/__tests__/SettingsView.spec.ts` is in the CI critical-test list, so it provides a safety net for that refactor.

## Known Bugs

No confirmed bugs found. Grep for `FIXME`, `HACK`, and `XXX:` returns zero hits across both backend and frontend; the 13 backend markers are all `TODO`. Behaviours that could be read as bugs are catalogued above and below as deliberate, commented tradeoffs.

## Security Considerations

**WebSocket endpoints do not verify `Origin`:**
- Risk: all three `coderws.Accept` call sites omit `OriginPatterns`, and `backend/internal/handler/openai_live.go:231` explicitly sets `InsecureSkipVerify: true`. In `coder/websocket` this option disables the **Origin check (CSRF protection)**, not TLS verification. `grep -rn "OriginPatterns"` returns no hits anywhere in the repo. This means a page on any origin can open a WebSocket to these endpoints from a victim's browser.
- Files: `backend/internal/handler/openai_live.go:230` (explicit `InsecureSkipVerify: true`), `backend/internal/handler/openai_gateway_handler.go:1818`, `backend/internal/handler/grok_audio.go:126`
- Current mitigation: substantially reduces but does not eliminate the risk. All three paths authenticate via API key from the request context (`GetAPIKeyFromContext`) rather than cookies, so a cross-origin page cannot ride an ambient session — it must already possess a valid API key. `openai_live.go` additionally gates on `liveEnabledForAPIKey` and verifies call ownership via `GetLiveCallForIdentity`, returning `ErrLiveIdentityMismatch` on a mismatch. `openai_gateway_handler.go` enforces a per-API-key ingress connection cap.
- Recommendations: set explicit `OriginPatterns` from the existing `cors.allowed_origins` config so the WebSocket policy matches the HTTP CORS policy, and remove `InsecureSkipVerify: true` from `openai_live.go:231`. Low effort, and it closes the gap for any future cookie-authenticated WebSocket route.

**Frontend `v-html` with an accepted XSS tradeoff:**
- Risk: 13 `v-html` bindings exist in `frontend/src`. Most are safe — SVG icons pass through `sanitizeSvg` (DOMPurify with the SVG profile, `frontend/src/utils/sanitize.ts`), markdown/legal content is sanitized, and `frontend/src/views/KeyUsageView.vue:260` renders hardcoded icon constants. The exception is `homeContent`, rendered raw with the comment `SECURITY: homeContent is admin-only setting, XSS risk is acceptable`.
- Files: `frontend/src/views/HomeView.vue:12` (raw), `frontend/src/utils/sanitize.ts` (the sanitizer), `frontend/src/components/layout/AppSidebar.vue:97,122,142` (sanitized)
- Current mitigation: the value comes from an admin-only setting, and the risk is explicitly documented at the call site rather than overlooked.
- Recommendations: the tradeoff is defensible (an admin who can set site HTML can generally already do worse), but it makes admin-settings write access equivalent to stored XSS against every visitor of the public home page. Worth ensuring that setting is behind the strictest admin permission tier and covered by the audit log.

**gosec rules excluded project-wide:**
- Risk: `backend/.golangci.yml:46-61` excludes 11 gosec rules, including `G104` (unhandled errors), `G304` (file path from variable / path traversal), `G404` (weak random), `G115` (integer overflow on conversion), and `G201`/`G202` (SQL string formatting/concatenation).
- Files: `backend/.golangci.yml`
- Current mitigation: mostly compensated elsewhere. `errcheck` runs with `check-type-assertions: true` and `disable-default-exclusions: true`, which is stricter than G104. Manual review of the SQL layer found queries correctly parameterized — `backend/internal/repository/batch_image_repo.go:80-120` builds dynamic `WHERE` clauses using numbered placeholders (`$1`, `$2`, …) with values in an `args` slice, and `backend/internal/repository/ops_repo_dashboard.go:400-442` uses `fmt.Sprintf` only to generate placeholder *indices*, never to interpolate values. `math/rand` appears in ~20 non-test files but the two audited uses are non-security (monitor challenge generation, annotated `//nolint:gosec // 仅用于生成测试问题，无安全影响`).
- Recommendations: the `G201`/`G202` exclusions are the ones worth revisiting, since they remove the automated backstop for the one class of bug the SQL layer is currently avoiding by convention alone. Re-enabling them (with targeted `//nolint` where the placeholder-index pattern trips them) would keep future dynamic-query code honest.

**Auto-generated secrets on startup:**
- Risk: when `totp.encryption_key` is unset, the server generates a random AES-256 key at boot and logs a warning. Similarly `jwt.secret` may be temporarily empty during startup before database initialization completes.
- Files: `backend/internal/config/config.go:1906-1917` (TOTP key generation), `backend/internal/config/config.go:1919-1931` (JWT startup window), `backend/internal/config/config.go:1628-1634` (`TotpConfig`)
- Current mitigation: well-guarded. The generated key sets `EncryptionKeyConfigured = false`, and per the struct comment, TOTP can only be *enabled* in the admin UI when the key was manually configured — so a random key cannot silently encrypt data that is later unrecoverable. Weak JWT secrets trigger `isWeakJWTSecret` warnings. `deploy/docker-compose.yml` requires `POSTGRES_PASSWORD` via `${POSTGRES_PASSWORD:?...}` (hard failure, no default) and documents the need for fixed `JWT_SECRET` and TOTP keys in comments at lines 92-107.
- Recommendations: no change needed for TOTP. The residual risk is operational: a multi-instance deployment with `JWT_SECRET` unset gets per-instance random secrets, invalidating sessions on any instance switch. That is documented in the compose file but not enforced; consider failing startup when a multi-instance mode is configured without a fixed `JWT_SECRET`.

**Security defaults that can be disabled:**
- Risk: `security.url_allowlist.enabled=false` disables allowlist/SSRF checks (falling back to minimal format validation), and `security.response_headers.enabled=false` disables configurable header filtering.
- Files: `backend/internal/config/config.go:1933-1938`, `backend/internal/util/urlvalidator/validator.go`, `backend/internal/service/channel_monitor_ssrf.go`
- Current mitigation: both log explicit `slog.Warn` messages naming exactly what was disabled. Dedicated SSRF machinery exists across `channel_monitor_ssrf.go`, `cn_provider_probe_url.go`, and `urlvalidator/validator.go`. TLS verification cannot be disabled at all: `backend/internal/pkg/httpclient/pool.go:126` hard-fails with `insecure_skip_verify is not allowed; install a trusted certificate instead`, and the config field at `config.go:832` is annotated as disabled.
- Recommendations: this is good design — the escape hatches are loud and TLS has no escape hatch. Worth verifying the warnings are surfaced in the admin UI health view, not only in logs.

## Performance Bottlenecks

**Channel Monitor V2 backfill load on the primary database (mitigated, documented):**
- Problem: the original first-enable bootstrap forced 5-second ticks and ~24h `RecomputeRange` chunks walking back 30 days, described in the design doc as "hammering the primary DB". A related error-dedup query matched `request_id` against `candidate_ids` with no `created_at` bound, scanning the full `ops_error_logs` history.
- Files: `docs/channel-monitor-v2-safe-defaults.md` (the analysis and decisions), `backend/internal/service/channel_monitor_runner.go`, `backend/internal/service/channel_monitor_checker.go`, `backend/migrations/195_channel_monitor_mode.sql`
- Cause: aggressive bootstrap defaults combined with an unbounded history scan.
- Improvement path: already designed and marked "Approved for implementation" — hard gates (tick = `refresh_interval` of 60/300s, no 5s override; at most one historical chunk per tick; single leader lock; ~55s transaction budget) plus adaptive soft gates (1h initial chunk, 15m floor, depth-based ceilings, ×1.5 growth on success, halving with exponential backoff on failure), and a `created_at` bound on the dedup query. Verify these landed before treating this as resolved; the doc is dated 2026-08-08 and describes a branch (`fix/channel-monitor-v2-ops-ui-blockers`), not necessarily merged state.

**Config surface parsed at startup:**
- Problem: `backend/internal/config/config.go` defines 67 config structs and 493 defaults in one 3,816-line file.
- Files: `backend/internal/config/config.go`
- Cause: accumulated feature configuration in a single flat module.
- Improvement path: startup cost is one-time and unlikely to matter at runtime; this is primarily a maintainability concern (see Tech Debt). Splitting per-domain config into separate files under `backend/internal/config/` would reduce conflict surface without changing behaviour. Note `config_test.go` is 2,591 lines and provides good coverage for such a split.

## Fragile Areas

**Goroutines without panic recovery:**
- Files: 110 `go func` sites in non-test `backend/internal` code against only 32 `recover()` calls. Confirmed unrecovered spawners include `backend/internal/service/payment_fulfillment.go:394` (notification email dispatch), `backend/internal/service/subscription_service.go`, `backend/internal/service/openai_gateway_usage.go`, `backend/internal/service/batch_image_cleanup.go`, `backend/internal/service/gateway_upstream_response.go`, and ~20 more.
- Why fragile: `backend/internal/server/middleware/recovery.go` uses `gin.CustomRecoveryWithWriter`, which only protects the request-handling goroutine. A panic inside a spawned `go func` crashes the entire process — for a gateway serving all tenants, one nil-pointer in a background email send takes down the whole server. No shared `SafeGo`/`safego` helper exists (grepped `backend/internal/pkg` and `backend/internal/util`; no hits).
- Safe modification: when adding a `go func`, add `defer func() { if r := recover(); r != nil { ... } }()` with structured logging. Better: introduce one `safego.Go(ctx, name, fn)` helper in `backend/internal/pkg` and migrate call sites incrementally, highest-traffic paths first.
- Test coverage: gaps here are hard to test directly; a lint rule or a review checklist item is more effective than tests.

**Detached background contexts:**
- Files: 323 `context.Background()` occurrences in non-test `backend/internal/service`.
- Why fragile: work started with `context.Background()` is not cancelled when the originating request or the server shuts down, risking work that outlives its shutdown window. Some uses are correct and deliberate — `backend/internal/service/openai_live.go` uses it intentionally because finalization is the session's only chance to persist (documented, referencing issue #3656), and `backend/internal/service/payment_fulfillment.go:395` correctly derives a `WithTimeout` child.
- Safe modification: follow the `payment_fulfillment.go` pattern — always wrap in `context.WithTimeout` rather than passing bare `Background()`. For shutdown-sensitive work, derive from a long-lived application context that the server cancels on shutdown.
- Test coverage: `backend/internal/service/openai_live.go`'s finalization path has an explicit idempotency test, which is the right model for the rest.

**Multi-instance in-memory state:**
- Files: `backend/internal/handler/auth_dingtalk_client.go:31` (`TODO(multi-instance): Redis 集中缓存 appToken` — DingTalk `appToken` cached in a struct field behind a mutex), plus package-level `sync.Map` caches in `backend/internal/service/grok_free_quota_gate.go:127-131`, `backend/internal/service/openai_agent_identity.go:32`, `backend/internal/service/grok_observed_models.go:29`, and `backend/internal/service/custom_channel_time_pricing.go:12`.
- Why fragile: each instance maintains independent state. For the DingTalk token this means N instances each fetching and refreshing their own token (rate-limit pressure upstream); for the quota gates it means per-instance views of quota that may disagree.
- Safe modification: the DingTalk TODO names the fix — move to Redis, for which infrastructure already exists (`backend/internal/pkg/redissession`, `backend/internal/repository/totp_cache.go` shows the Redis pipeline pattern). Before adding any new package-level cache, decide explicitly whether per-instance divergence is acceptable.
- Test coverage: the codebase does test multi-instance concerns elsewhere (the openspec change includes "多 Worker/多实例测试" and fencing-token tests), so the pattern to follow exists.

**Concurrency primitives that are correctly guarded (verified, not a concern):**
- `backend/internal/handler/admin/ops_ws_handler.go:67` — `wsConnCountByIP` map is guarded by `wsConnCountByIPMu`, connection counts use `atomic.Int32`, and the idle-stop timer is guarded by `qpsWSIdleStopMu` with a re-check of `wsConnCount.Load() == 0` at fire time. Noted here only to record that it was audited.
- The 10 `//nolint:gochecknoglobals` directives in `channel_monitor_checker.go` and `channel_monitor_validate.go` are read-only static lookup tables, each annotated as such.

## Scaling Limits

**WebSocket connection caps:**
- Current capacity: `defaultMaxWSConns = 100` and `defaultMaxWSConnsPerIP = 20` for the ops WebSocket; the OpenAI gateway enforces a separate `max_ingress_connections_per_api_key` limit, rejecting with HTTP 429 and `Retry-After: 5`.
- Limit: `backend/internal/handler/admin/ops_ws_handler.go:59-60`, enforcement at `backend/internal/handler/openai_gateway_handler.go:1805-1815`.
- Scaling path: caps are per-instance in-memory, so total capacity scales with instance count but no single instance can be given a global view. Distributed limiting would need Redis-backed counters (see Multi-instance in-memory state above).

**Single-leader background work:**
- Current capacity: channel monitor backfill uses a single leader lock with a ~55s transaction budget and at most one historical chunk per tick.
- Limit: `docs/channel-monitor-v2-safe-defaults.md`; runner at `backend/internal/service/channel_monitor_runner.go`.
- Scaling path: backfill throughput is bounded by one leader regardless of instance count — by design, to protect the primary database. If backfill latency becomes the constraint, shard by channel rather than lifting the per-tick chunk gate.

## Dependencies at Risk

**`xlsx` (SheetJS) 0.18.5 — two high-severity CVEs with no npm fix:**
- Risk: CVE-2023-30533 (prototype pollution, CVSS 7.8) and CVE-2024-22363 (ReDoS, CVSS 7.5). Both advisories state `"patched_versions": "<0.0.0"` — i.e. **no fixed version is obtainable from npm**; the GitHub repo and npm package are unmaintained, and fixes exist only via `https://cdn.sheetjs.com/`.
- Files: `frontend/package.json:36`, `frontend/audit.json` (the recorded audit), `.github/audit-exceptions.yml:3-16`, consumer at `frontend/src/views/admin/UsageView.vue:575`
- Impact: **both CVEs are unreachable in this codebase.** Verified by grep: the only production use is `XLSX.write` for admin export (`UsageView.vue:619`); there are no `XLSX.read`/`readFile` calls anywhere in `frontend/src`. Both advisories are triggered only when *reading* crafted files — the prototype-pollution overview states plainly that "workflows that do not read arbitrary files (for example, exporting data to spreadsheet files) are unaffected." The import is also dynamic (`await import('xlsx')`), so the library is not in the main bundle.
- Migration plan: the exceptions expire **2026-10-06** (about six weeks out). Since no npm fix will ever appear, expiry cannot be resolved by upgrading. Either migrate to a maintained writer (`exceljs`, or `write-excel-file`) — a contained change given the single call site — or pin the CDN 0.20.2+ build, or extend the exception with the reachability argument recorded above.

**Expired audit exceptions:**
- Risk: three entries in `.github/audit-exceptions.yml` have `expires_on` dates already in the past relative to 2026-08-23: `lodash` and `lodash-es` (GHSA-r5fr-rjxr-66jc, expired 2026-07-02) and `axios` (GHSA-3p68-rc4w-qgx5, **critical**, expired 2026-07-10).
- Files: `.github/audit-exceptions.yml:17-37`, enforcement in `tools/check_pnpm_audit_exceptions.py:216-235`, workflow `.github/workflows/security-scan.yml`
- Impact: currently harmless, and CI is not silently broken. Reading `tools/check_pnpm_audit_exceptions.py:206-218`, the expiry check only fires for advisories still present in the audit output — so a stale exception for a resolved advisory is inert. The current `frontend/audit.json` reports only the two `xlsx` advisories, and `axios` is resolved to 1.18.1 in `pnpm-lock.yaml`. No `from 'lodash'` imports exist in `frontend/src` (it survives only as a transitive dependency, 14 lockfile references).
- Migration plan: prune the three stale entries so the file reflects reality. Leaving expired exceptions in place is a slow-acting trap: if `axios` or `lodash` later reappears in an audit at high severity, the exception is already expired and CI fails with a confusing message rather than a fresh decision.

**Go toolchain version mismatch blocks local builds:**
- Risk: `backend/go.mod` declares `go 1.26.6`, the locally installed toolchain is `go1.25.7`, and `GOSUMDB=off` is set in the environment. `go build ./...` therefore fails with `download go1.26.6: verifying module: checksum database disabled by GOSUMDB=off` — Go cannot auto-download the required toolchain because it cannot verify it.
- Files: `backend/go.mod:3`, `.github/workflows/backend-ci.yml:29` (CI uses `go-version-file: backend/go.mod`, so CI is unaffected)
- Impact: **backend build and tests could not be verified in this environment.** This is an environment/toolchain issue, not a code defect — CI pins the version correctly from `go.mod` and includes an explicit "Verify Go version" step. But it blocks local development for anyone whose toolchain lags.
- Migration plan: install Go 1.26.6 directly, or set `GOTOOLCHAIN=local` after upgrading, or re-enable `GOSUMDB` (`GOFLAGS`/`GONOSUMDB` inspection shows `GOPROXY` already routes through `goproxy.cn`, suggesting a region-specific setup). Worth documenting the required toolchain and the `GOSUMDB` interaction in `DEV_GUIDE.md`, which currently records environment pitfalls but not this one.

**Dependency volume:**
- Risk: 54 direct + 146 indirect Go modules; 639 total npm dependencies.
- Files: `backend/go.mod`, `frontend/pnpm-lock.yaml`
- Impact: broad supply-chain surface, though the stack itself is mainstream and well-chosen (`gin-gonic/gin` 1.9.1, `entgo.io/ent` 0.14.5, `redis/go-redis/v9` 9.17.2, `google/wire` 0.7.0, `coder/websocket` 1.8.14, `refraction-networking/utls` 1.8.2, `go.uber.org/zap`, `spf13/viper`).
- Migration plan: no action needed. `govulncheck` runs in `.github/workflows/security-scan.yml` for Go, and `pnpm audit --prod --audit-level=high` plus the exception gate covers npm. Note `gin-gonic/gin` 1.9.1 is somewhat behind current releases; worth a routine bump review.

## Missing Critical Features

**Group and redeem-code statistics:**
- Problem: `GET /admin/groups/:id/stats` and the redeem-code stats path return hardcoded zeros (see Tech Debt).
- Blocks: any admin view depending on per-group key counts, request volume, or cost attribution, and any redeem-code inventory reporting. Adjacent real implementations exist (`GetUsageSummary` / `dashboardService.GetGroupUsageSummary`), so the data is available — only the wiring is missing.

**Credential test paths for several platforms:**
- Problem: `backend/internal/service/account_service.go:506-512` carries three parallel TODOs for testing Anthropic, OpenAI, and Gemini API credentials; `backend/internal/service/proxy_service.go:185` has an unimplemented proxy connectivity test (`TODO: 实现代理连接测试逻辑`).
- Blocks: admins cannot validate credentials or proxy reachability from the UI for those paths, pushing discovery of bad configuration to first real traffic. Note a substantial `account_test_service.go` (3,154 lines) exists, so some platforms *are* covered — these are gaps in an otherwise-built feature, not an absent one.

**Batch image generation parameter passthrough:**
- Problem: `backend/internal/service/batch_image_provider_gemini.go:302` defers `response_mime_type`/`aspect_ratio`/`image_size` until upstream support lands.
- Blocks: callers cannot control output format or dimensions on the Gemini batch-image path. Externally gated, so not actionable now.

**Content moderation enforcement granularity:**
- Problem: `backend/internal/service/content_moderation.go:1932` notes it should disable *the triggering API key* rather than taking the broader action it currently takes, pending API key mutation being available at that call site.
- Blocks: moderation response is coarser than intended — likely affecting more of a user's access than the specific offending key. Worth confirming the current blast radius before changing.

## Test Coverage Gaps

Overall coverage is strong and should not be understated: ~365K lines of test code against ~372K non-test lines in `backend/internal` (≈1:1), 1,163 Go test files, and 239 frontend `.spec.ts` files. `backend/internal/server/api_contract_test.go` alone is 2,942 lines of contract tests. Naming is granular and behaviour-oriented (e.g. 15 separate `ratelimit_service_*_test.go` files covering 401/403/HTML/window-limit/threshold cases). The gaps below are specific, not systemic.

**Untagged Go tests may not run in CI:**
- What's not tested: `.github/workflows/backend-ci.yml` runs `make test-unit` (`go test -tags=unit`) and `make test-integration` (`go test -tags=integration`). Of 1,163 test files, 416 declare `//go:build unit` and 76 declare `//go:build integration` — leaving ~665 files with no build tag. Under `-tags=unit`, untagged files *are* still compiled and run (a build tag restricts inclusion; its absence does not exclude), so these most likely do execute. But the split means the tag is not a reliable signal of what CI covers.
- Files: `.github/workflows/backend-ci.yml:36-41`, `backend/Makefile` (`test-unit`, `test-integration` targets)
- Risk: low-to-moderate, and worth confirming empirically rather than assuming. If any of the 665 carry a different constraint (`//go:build !unit` appears once), those are genuinely skipped. The `backend/Makefile` `test` target runs plain `go test ./...` with no tags, so a local `make test` and CI's `make test-unit` cover different sets — meaning a test can pass locally and never run in CI, or vice versa.
- Priority: Medium. Verify with `go test -tags=unit ./... -list '.*' | wc -l` against `go test ./... -list '.*' | wc -l`, then either tag consistently or drop the tag scheme for unit tests and reserve tags for integration/e2e only.

**Frontend CI runs a curated 13-file subset:**
- What's not tested: `make test-frontend` runs `lint:check`, `typecheck`, then `vitest run` against only the 13 files in `FRONTEND_CRITICAL_VITEST` — out of 239 `.spec.ts` files. The remaining ~226 run only if someone invokes `pnpm test:run` locally.
- Files: `Makefile:3-16` (the `FRONTEND_CRITICAL_VITEST` list), `.github/workflows/backend-ci.yml:60-61`
- Risk: regressions in non-critical frontend paths reach main unnoticed. The chosen 13 are sensible (API client, token refresh, auth callbacks, payment views, settings), and full `typecheck` does run across everything — which catches type-level breakage if not behavioural. But ~95% of the behavioural test suite is not gated.
- Priority: Medium. If the subset exists for CI runtime reasons, consider running the full suite nightly or on a `main`-merge trigger, keeping the fast subset for PRs. If it exists because some tests are flaky, those should be quarantined explicitly rather than implicitly excluded.

**Large service files without a same-named test file:**
- What's not tested: several large files have no `<name>_test.go` sibling — `backend/internal/service/account.go` (3,186 lines), `backend/internal/service/gateway_scheduling.go` (2,583), `backend/internal/service/ratelimit_service.go` (2,595), `backend/internal/handler/admin/account_handler.go` (3,078), `backend/internal/handler/gateway_handler.go` (2,467), `backend/internal/handler/admin/setting_handler_update.go` (2,466).
- Files: as listed above.
- Risk: **lower than it appears, and partially a false signal.** This project deliberately names tests by behaviour rather than by file — `ratelimit_service.go` has 15 dedicated `ratelimit_service_*_test.go` files, and `account.go`'s surface is covered by ~20+ `account_*_test.go` files. The genuine question is `gateway_scheduling.go`, where grep found no matching test file at all despite 2,583 lines of scheduling logic on the hot request path.
- Priority: High for `backend/internal/service/gateway_scheduling.go` specifically — scheduling bugs manifest as misrouted or dropped traffic and are hard to diagnose in production. Low for the rest; verify coverage by behaviour name before adding tests.

**Verification evidence for the in-flight prompt-audit change:**
- What's not tested: `openspec/changes/add-openai-compatible-prompt-audit/tasks.md` shows 153/153 tasks checked with 0 unchecked, but `verification.md` is structured as an evidence-collection template whose status column uses `待实现`/`通过`/`失败`/`豁免` markers. It states explicitly that "仅写'人工验证通过'不算证据" — only reproducible test names, command output, SQL results, or screenshot paths count.
- Files: `openspec/changes/add-openai-compatible-prompt-audit/verification.md`, `openspec/changes/add-openai-compatible-prompt-audit/tasks.md`, implementation in `backend/internal/securityaudit/` (36 files, 15 tests)
- Risk: tasks being complete is not the same as verification evidence being filled in. Before treating this feature as shipped, confirm the Requirement → Evidence matrix in `verification.md` has no remaining `待实现` rows — particularly the leak gates, which assert that `prompt_audit_jobs`/`prompt_audit_events` contain no `raw_prompt`, `payload`, `token`, or `authorization` columns, and that no full prompt or Guard token appears in PostgreSQL, logs, admin APIs, or the frontend.
- Priority: High — these are data-leakage assertions on a feature that handles user prompts by design.

---

*Concerns audit: 2026-08-23*
