# External Integrations

**Analysis Date:** 2026-08-23

Sub2API's core function is proxying to upstream AI providers, so the integration surface is unusually wide. Two distinct configuration channels exist and matter when adding an integration:

1. **Config file / env** — `deploy/config.example.yaml` → `backend/internal/config/config.go`. Used for infrastructure and boot-critical settings.
2. **`settings` DB table, edited from the admin UI** — `SettingKey*` constants in `backend/internal/service/setting_*.go`. Used for most third-party credentials (SMTP, captcha, GitHub/Google OAuth, payment providers).

Credentials below are named by config key or env var only.

## APIs & External Services

### Upstream AI providers (the gateway's reason for existing)

Platform identifiers are defined in `backend/internal/domain/constants.go` (`PlatformAnthropic`, `PlatformOpenAI`, `PlatformGemini`, `PlatformAntigravity`, `PlatformGrok`, `PlatformKimi`, `PlatformZhipu`, `PlatformDeepseek`, `PlatformComposite`). Credentials are stored per-account in the `accounts` table, not in config.

| Provider | Default base URL | Where defined |
|----------|------------------|---------------|
| Anthropic | `https://api.anthropic.com` | `backend/internal/service/upstream_models.go:271` |
| OpenAI / Codex | `https://api.openai.com` | `backend/internal/service/openai_gateway_cc_pipeline.go:143` |
| OpenAI accounts API | `https://auth.openai.com/api/accounts` | `backend/internal/service/openai_agent_identity.go:26` |
| Gemini (AI Studio) | `https://generativelanguage.googleapis.com` | `backend/internal/pkg/geminicli/constants.go:7` |
| Gemini (Code Assist) | `https://cloudcode-pa.googleapis.com` | `backend/internal/pkg/geminicli/constants.go:8` |
| Antigravity | `https://cloudcode-pa.googleapis.com` | `backend/internal/pkg/antigravity/oauth.go:56` |
| xAI / Grok | `https://api.x.ai/v1`, regional `us-east-1` / `us-west-2` / `eu-west-1`, CLI proxy `https://cli-chat-proxy.grok.com/v1` | `backend/internal/pkg/xai/oauth.go:29-33` |
| Grok accounts/OAuth | `https://accounts.x.ai` | `backend/internal/repository/grok_oauth_client.go:29` |
| Kimi / Moonshot | PayG `https://api.moonshot.cn/v1`, Coding `https://api.kimi.com/coding/v1`, Anthropic-protocol variants | `backend/internal/service/domain_constants.go:73-85` |
| Zhipu GLM | PayG `https://open.bigmodel.cn/api/paas/v4`, Coding `https://open.bigmodel.cn/api/coding/paas/v4` | `backend/internal/service/domain_constants.go:75-76` |
| DeepSeek | `https://api.deepseek.com` (+ `/anthropic`) | `backend/internal/service/domain_constants.go:77` |
| AWS Bedrock | SigV4 or API key, per `credentials.auth_mode` | `AccountTypeBedrock` in `backend/internal/domain/constants.go` |
| Google Vertex AI | service account credentials | `AccountTypeServiceAccount`; `backend/internal/service/batch_image_provider_vertex.go` |
| Azure OpenAI | `*.openai.azure.com` (allowlisted) | `security.url_allowlist.upstream_hosts` |

Account credential types: `oauth`, `setup-token`, `apikey`, `upstream` (arbitrary base URL + key), `bedrock`, `service_account`.

Upstream hosts can be constrained by `security.url_allowlist.upstream_hosts` (disabled by default — `enabled: false`). When the allowlist is off, `allow_insecure_http: true` permits plain `http://` upstreams.

**Auth:** per-account credentials in DB. OAuth token refresh runs as a background job governed by the `token_refresh` config block (`provider_concurrency`, `provider_qps`, `provider_failure_threshold`). OAuth service implementations: `backend/internal/service/gemini_oauth_service.go`, `grok_oauth_service.go`, `antigravity_oauth_service.go`.

**Gemini OAuth client:** `gemini.oauth.client_id` / `client_secret`. If both are blank, the built-in Gemini CLI public client is used and its secret must be injected via `GEMINI_CLI_OAUTH_CLIENT_SECRET`. The two fields must be both empty or both set.

**TLS fingerprint impersonation:** `gateway.tls_fingerprint.enabled` (default true) with uTLS (`backend/internal/pkg/tlsfingerprint/`). Default profile `claude_cli_v2` mimics Node.js 20.x so upstream providers see a CLI-like TLS handshake. Custom profiles are stored in the `tls_fingerprint_profiles` table.

**Egress proxies:** per-account HTTP/SOCKS5 proxies stored in the `proxies` table (`backend/ent/schema/proxy.go`), with probing in `backend/internal/pkg/proxyurl/` and `proxyutil/`. Probe targets are configurable (`security.proxy_probe.urls`, parsers `ip-api` / `ipify` / `chatgpt-trace`); default falls back to ip-api/ipify. `security.proxy_fallback.allow_direct_on_error` defaults to **false** — auxiliary services fail fast rather than leaking the host IP on proxy init failure.

### Payment providers

Registry: `backend/internal/payment/registry.go`, factory `backend/internal/payment/provider/factory.go`, types in `backend/internal/payment/types.go:12-20`. Multiple instances per type are supported with weighted selection (`backend/internal/payment/load_balancer.go`).

| Type | Implementation | Notes |
|------|----------------|-------|
| `alipay` | `backend/internal/payment/provider/alipay.go` | `github.com/smartwalle/alipay/v3` v3.2.29; `https://openapi.alipay.com` |
| `wxpay` | `backend/internal/payment/provider/wxpay.go` | `github.com/wechatpay-apiv3/wechatpay-go` v0.2.21 |
| `stripe` | `backend/internal/payment/provider/stripe.go` | `github.com/stripe/stripe-go/v85` v85.0.0 |
| `airwallex` | `backend/internal/payment/provider/airwallex.go` | Prod `https://api.airwallex.com/api/v1`, demo `https://api-demo.airwallex.com/api/v1` (`airwallex.go:24-25`) |
| `easypay` | `backend/internal/payment/provider/easypay.go` | Chinese aggregator; custom signing (`easypay_sign_test.go`) |

**Auth:** provider credentials live in the `payment_provider_instances` table (`backend/ent/schema/payment_provider_instance.go`), managed via `PUT /api/v1/admin/payment/providers`. Not in the config file. Signature/crypto helpers: `backend/internal/payment/crypto.go`.

Frontend SDKs: `@stripe/stripe-js` ^9.0.1 (lazy-loaded into a `vendor-stripe` chunk) and `@airwallex/components-sdk` ^1.30.2. Their script/frame origins are explicitly whitelisted in the default CSP (`security.csp.policy`).

### Captcha / bot protection

- **Cloudflare Turnstile** - `backend/internal/service/turnstile_service.go`. Settings `turnstile_enabled`, `turnstile_site_key`, `turnstile_secret_key`. Config flag `turnstile.required` forces it in release mode.
- **Aliyun Captcha** - `backend/internal/service/aliyun_captcha_service.go` via `github.com/alibabacloud-go/captcha-20230305` v1.1.3. Settings `aliyun_captcha_enabled`, `aliyun_captcha_access_key_id`, `aliyun_captcha_access_key_secret`, `aliyun_captcha_scene_id`, `aliyun_captcha_region`, `aliyun_captcha_prefix`.
- **Tencent Captcha** - `backend/internal/service/tencent_captcha_service.go` via `tencentcloud-sdk-go/.../captcha` v1.3.52. Settings `tencent_captcha_enabled`, `tencent_captcha_app_id`, `tencent_captcha_app_secret_key`, `tencent_captcha_cloud_secret_id`, `tencent_captcha_cloud_secret_key`, `tencent_captcha_region`.
- **YesCaptcha** - env-only: `YESCAPTCHA_API_KEY`, `YESCAPTCHA_CLIENT_KEY`.

### Other outbound services

- **Model pricing feed** - `pricing.remote_url` and `pricing.hash_url` point at `raw.githubusercontent.com/Wei-Shaw/model-price-repo`. Downloaded every `update_interval_hours` (24) with SHA-256 verification; local cache in `pricing.data_dir`, fallback bundled at `backend/resources/model-pricing/model_prices_and_context_window.json`. Host restricted by `security.url_allowlist.pricing_hosts`.
- **GitHub API** (`https://api.github.com`) - release/update checks. Optional `UPDATE_GITHUB_TOKEN` raises the rate limit; `update.proxy_url` routes the call through a proxy (http/https/socks5/socks5h).
- **CRS sync** - `backend/internal/service/crs_sync_service.go`. Pulls accounts/config from an external Claude Relay Service instance. Requires `security.url_allowlist.crs_hosts` to be populated when the allowlist is enabled (it is empty by default).
- **Content moderation** - `backend/internal/service/content_moderation.go`, default base URL `https://api.openai.com` (an OpenAI-compatible moderation endpoint). Configured via the `content_moderation_config` setting.
- **Channel monitor** - `backend/internal/service/` + `backend/ent/schema/channel_monitor*.go`. Periodically issues real requests to upstream accounts to measure health. Templates in `channel_monitor_request_template`; results in `channel_monitor_history` and `channel_monitor_daily_rollup`.

## Data Storage

**Databases:**
- **PostgreSQL** (primary, production)
  - Connection: `database.host`, `database.port`, `database.user`, `database.password`, `database.dbname`, `database.sslmode` (default `prefer`). Pool: `max_open_conns` 256, `max_idle_conns` 128.
  - Client: Ent ORM (`backend/ent/`) plus direct `github.com/lib/pq` for array types and error-code translation (`backend/internal/repository/error_translate.go`).
  - Schema: 39 entities in `backend/ent/schema/`; 280 ordered SQL migrations in `backend/migrations/` starting at `001_init.sql`.
  - Repositories: ~300 files in `backend/internal/repository/`.
- **SQLite** - `modernc.org/sqlite` v1.44.3, pure Go. Used for local/embedded and test paths, not production.

**File Storage:**
- **S3-compatible object storage** for async image-generation results. Config block `image_storage` (`enabled`, `endpoint`, `region`, `bucket`, `access_key_id`, `secret_access_key`, `prefix`, `force_path_style`, `public_base_url`, `presign_expiry_hours`, `max_download_bytes`). Implementation: `backend/internal/service/image_storage.go`, settings bridge `image_storage_settings.go`, SDK `aws-sdk-go-v2/service/s3`.
  - Works with AWS S3, Cloudflare R2 (`region: auto`), Aliyun OSS, MinIO (`force_path_style: true`).
  - `image_storage.enabled` doubles as the master switch for the async image endpoints: when false or credentials are incomplete, `/v1/images/generations/async` and friends return 404.
  - Extension point: implement `service.ImageStorage` (`Save(ctx, key, contentType, data) -> url`) for a non-S3 backend.
  - Rationale documented at `deploy/config.example.yaml:1172` — keeps large base64 image payloads out of Redis.
- **Local filesystem** - `DATA_DIR` (default `/app/data` in Docker) for logs, pricing cache, and DB backups.

**Caching:**
- **Redis** - `redis.host`, `redis.port`, `redis.username`, `redis.password`, `redis.db`, `redis.pool_size` (1024), `redis.min_idle_conns` (128), `redis.enable_tls`. Client `github.com/redis/go-redis/v9`.
  - Roles: API-key auth L2 cache, dashboard stats cache (`dashboard_cache.key_prefix` defaults to `sub2api:`), concurrency slots, session stickiness for WS routing, idempotency records, async image task state, panel rate limiting.
  - Session helpers: `backend/internal/pkg/redissession/`.
- **In-process L1** - Ristretto and go-cache. `api_key_auth_cache` tunes L1 size (65535), L1 TTL (15s), L2 TTL (300s), negative TTL (30s), jitter (10%), and singleflight coalescing.

## Authentication & Identity

**End-user auth (panel):**
- Local email + password, with optional verification (`email_verify_enabled`).
- JWT access tokens — `jwt.secret`, `jwt.expire_hour` (max 168), `jwt.access_token_expire_minutes`. Middleware: `backend/internal/server/middleware/`. Generator utility: `backend/cmd/jwtgen/`.
- TOTP 2FA — `totp.encryption_key` (must be set to a fixed value; a blank value regenerates a random key each boot and invalidates all existing TOTP enrollments).
- WebAuthn / Passkeys — `webauthn.enabled`, `rp_display_name`, `rp_id` (domain only), `rp_origins` (exact origins). Requires HTTPS in production. Repo: `backend/internal/repository/passkey_repo.go`.

**SSO / OAuth login providers** (all resolve to entries in `auth_identities` / `auth_identity_channels`):

| Provider | Callback route | Credentials |
|----------|----------------|-------------|
| LinuxDo Connect | `GET /api/v1/auth/oauth/linuxdo/callback` | `linuxdo_connect.client_id` / `client_secret` (config) |
| Generic OIDC | `GET /api/v1/auth/oauth/oidc/callback` | `oidc_connect.client_id` / `client_secret`, `issuer_url` / `discovery_url`, `jwks_url` |
| GitHub | `GET /api/v1/auth/oauth/github/callback` | settings `github_oauth_client_id` / `github_oauth_client_secret` |
| Google | `GET /api/v1/auth/oauth/google/callback` | settings `google_oauth_client_id` / `google_oauth_client_secret` |
| WeChat | `GET /api/v1/auth/oauth/wechat/callback` | env `WECHAT_OAUTH_MP_APP_ID/_SECRET`, `WECHAT_OAUTH_OPEN_APP_ID/_SECRET` |
| WeChat (payment) | `GET /api/v1/auth/oauth/wechat/payment/callback` | same as above |
| DingTalk | `GET /api/v1/auth/oauth/dingtalk/callback` | settings `dingtalk_connect_client_id` / `client_secret`; validation `backend/internal/config/validate_dingtalk.go` |

Routes registered in `backend/internal/server/routes/auth.go:83-220`. Flow logic: `backend/internal/service/auth_oauth_email_flow.go`, `auth_oauth_first_bind.go`, `auth_email_oauth_auto.go`.

PKCE is supported and required when `token_auth_method: none` (public client). OIDC ID-token validation is on by default (`validate_id_token: true`, `allowed_signing_algs: RS256,ES256,PS256`, `clock_skew_seconds: 120`).

Per-source signup grants (balance, concurrency, subscriptions) are configured with `auth_source_default_<provider>_*` settings.

**API consumer auth (gateway):**
- `sk-`-prefixed API keys (`default.api_key_prefix`) validated by `APIKeyAuthMiddleware` against a two-tier cache. Schema: `backend/ent/schema/api_key.go`.
- Admin API keys — `admin_api_key` setting, sent as `x-api-key`. Used by the bundled admin CLI (`skills/sub2api-admin/scripts/sub2api-admin.js`).
- Invalid-credential abuse protection: `api_key_auth_cache.invalid_abuse` (threshold 120 per window, 60s window, 60s block, 16384 tracked identities). Counts only genuinely invalid credentials — Redis/DB failures do not consume budget.

## Monitoring & Observability

**Error Tracking:**
- No external APM/error service (no Sentry, Datadog, or similar). Errors are captured in-app: `ops` tables plus an ingress-reject recorder (`middleware.SetIngressRejectRecorder` in `backend/internal/server/router.go`) and a cleanup job at `backend/cmd/cleanup-ingress-reject-logs/`.

**Logs:**
- zap via `backend/internal/pkg/logger/`. `log.level`, `log.format` (json/console), `log.service_name`, `log.env`, `log.caller`, `log.stacktrace_level`.
- Dual output: stdout (for container collectors) and rotating file (lumberjack) at `{DATA_DIR}/logs/sub2api.log`; rotation `max_size_mb` 100, `max_backups` 10, `max_age_days` 7, gzip.
- Optional sampling (`log.sampling`, off by default) to damp high-frequency repeats.
- Upstream error bodies are logged truncated and content-free: `gateway.log_upstream_error_body` (true), `log_upstream_error_body_max_bytes` (2048).

**Metrics / health:**
- `GET /health` returns `{"status":"ok"}` (`backend/internal/server/routes/common.go`).
- Built-in ops dashboard gated by `ops.enabled`; host stats via gopsutil. Aggregation job configured in `dashboard_aggregation` (60s interval, 120s lookback, retention 90d raw / 180d hourly / 730d daily).
- `Server-Timing` headers for authenticated admin-UI requests when `server.enable_server_timing` is on (`backend/internal/pkg/servertiming/`).
- OpenTelemetry packages are present in `backend/go.sum` but only as indirect dependencies (pulled in by testcontainers/grpc) — no tracing is wired up.
- `POST /api/event_logging/batch` is a deliberate no-op that swallows Claude Code telemetry (`backend/internal/server/routes/common.go`).

## CI/CD & Deployment

**Hosting:**
- Self-hosted. Docker Compose is the primary path (`deploy/docker-compose.yml`: `weishaw/sub2api:latest` + `postgres:18-alpine` + `redis:8-alpine`; only the app publishes ports). Variants for dev, local, and standalone (single-container).
- Alternatives: systemd units (`deploy/sub2api.service`, `deploy/sub2api-datamanagementd.service`), Apple Container (`deploy/apple-container.sh`), Caddy front proxy (`deploy/Caddyfile`).
- Install/deploy scripts: `deploy/install.sh`, `deploy/docker-deploy.sh`, `deploy/install-datamanagementd.sh`. Docs: `deploy/DOCKER.md`, `deploy/EDGE_SECURITY.md`.

**CI Pipeline (GitHub Actions):**
- `.github/workflows/backend-ci.yml` - four jobs: `shell` (macos-15, lints deploy scripts and runs `deploy/tests/*`), `test` (`make test-unit` then `make test-integration`), `frontend` (`make test-frontend`), `golangci-lint` (v2.9, 30m timeout). Go version is asserted to be exactly `go1.26.6`.
- `.github/workflows/release.yml` - GoReleaser-driven release; images from `Dockerfile.goreleaser`.
- `.github/workflows/security-scan.yml` - dependency/security scanning, with allowances in `.github/audit-exceptions.yml` and `frontend/audit.json`.
- `.github/workflows/cla.yml` - CLA check against `CLA.md`.

## Environment Configuration

**Required to boot:**
- `database.*` (host, port, user, password, dbname) and `redis.*` (host, port).
- `jwt.secret` — must be replaced; the example ships an obvious placeholder. Generate with `openssl rand -hex 32`.
- `totp.encryption_key` — required if 2FA is used; blank means enrollments break on every restart.
- `default.admin_email` / `default.admin_password` — bootstrap admin, created on first run. The example values (`admin@example.com` / `admin123`) must be changed.
- `CONFIG_FILE` if the YAML lives outside the default search path.

**Feature-dependent:**
- Gateway upstreams: per-account credentials in the DB (not config), plus `GEMINI_CLI_OAUTH_CLIENT_SECRET` when using the built-in Gemini CLI OAuth client.
- Payments: credentials per provider instance in the DB.
- Email: `smtp_host`, `smtp_port`, `smtp_username`, `smtp_password`, `smtp_from`, `smtp_from_name`, `smtp_use_tls` (all DB settings).
- Captcha: see the captcha section — DB settings, except YesCaptcha which is env-only.
- Object storage: `image_storage.access_key_id` / `secret_access_key` / `bucket`.
- Web-facing correctness: `server.frontend_url` (used to build links in emails), `cors.allowed_origins` (empty disables cross-origin entirely), `webauthn.rp_id` / `rp_origins`.

**Secrets location:**
- Config YAML at `/etc/sub2api/config.yaml` or `DATA_DIR`-relative; git-ignored at `backend/config.yaml` and `deploy/config.yaml`.
- Environment variables / Docker Compose env.
- The `settings` and `payment_provider_instances` DB tables hold most third-party credentials. Account credentials live in `accounts`; `security_secrets` (`backend/ent/schema/security_secret.go`) holds additional managed secrets.
- No `.env` file exists in the repo; `.env*` is git-ignored (`.gitignore:51-55`).
- `accounts export` via the admin API includes credentials and tokens — the bundled skill warns to write it to a file rather than echo it (`skills/sub2api-admin/SKILL.md`).

## Webhooks & Callbacks

**Incoming — payment webhooks** (unauthenticated by design; each verifies a provider signature). Registered in `backend/internal/server/routes/payment.go:60-69`, handled by `PaymentWebhookHandler`:
- `GET|POST /api/v1/payment/webhook/easypay` — GET variant exists because EasyPay sends query-param callbacks
- `POST /api/v1/payment/webhook/alipay`
- `POST /api/v1/payment/webhook/wxpay`
- `POST /api/v1/payment/webhook/stripe`
- `POST /api/v1/payment/webhook/airwallex`

**Incoming — OAuth callbacks:** the seven `/api/v1/auth/oauth/*/callback` routes listed in the Authentication section.

**Incoming — public payment recovery** (unauthenticated, `backend/internal/server/routes/payment.go:53-57`):
- `POST /api/v1/payment/public/orders/resolve` — signed resume-token lookup (preferred)
- `POST /api/v1/payment/public/orders/verify` — legacy anonymous `out_trade_no` verify, retained for staggered upgrades

**Incoming — AI gateway (API-key authenticated):** registered in `backend/internal/server/routes/gateway.go`. These are the client-facing proxy endpoints, not third-party callbacks.
- Anthropic-shaped: `POST /v1/messages`, `POST /v1/messages/count_tokens`, `GET /v1/models`
- OpenAI-shaped: `POST /v1/responses` (+ `/*subpath`), `POST /v1/chat/completions`, `POST /v1/embeddings`, `POST /v1/alpha/search`
- Codex direct: `/backend-api/codex/responses`, `/backend-api/codex/models`, `/backend-api/codex/realtime/calls`
- Gemini native: `/v1beta/models/*modelAction`, `GET /v1beta/models`
- Images: `/v1/images/generations`, `/edits`, async `/generations/async` + `GET /v1/images/tasks/:task_id`, batches under `/v1/images/batches`
- Video: `/v1/videos`, `/videos/generations`, `/edits`, `/extensions` with status and content sub-routes
- Voice: `/v1/tts`, `/v1/stt`, `/v1/custom-voices`
- Realtime/live: `GET /v1/realtime` (WebSocket), `POST /v1/live`, `GET /v1/live/:call_id`
- Search: `POST /v1/web_search`, `POST /v1/x_search`
- Billing introspection: `GET /v1/sub2api/billing`, `GET /v1/usage`
- Root-level aliases for CLI compatibility: `POST /responses`, `POST /chat/completions`, `POST /messages/count_tokens`, `GET /models`

**Outgoing:**
- Upstream AI provider calls (the primary egress) — HTTP/2, HTTP/1.1 fallback, and WebSocket, optionally through per-account proxies with uTLS fingerprinting.
- Payment provider APIs (order creation, query, refund).
- SMTP for transactional and notification email — `backend/internal/service/email_service.go`, queue in `email_queue_service.go`, templates in `email_message.go`. Triggers include balance-low alerts (`balance_notify_service.go`), account quota alerts (`account_quota_notify_emails`), and moderation notices (`content_moderation_email.go`).
- Pricing feed and GitHub release checks (both proxy-able via `update.proxy_url`).
- Captcha verification callbacks to Cloudflare/Aliyun/Tencent/YesCaptcha.
- CRS sync pulls.
- Channel-monitor probe requests against configured upstream accounts.
- Proxy health probes to ip-api / ipify / `chatgpt.com/cdn-cgi/trace`.

**Egress hardening worth knowing when adding an integration:**
- `security.url_allowlist` gates upstream, pricing, and CRS hosts. It is **off by default**; `allow_private_hosts: true` and `allow_insecure_http: true` in the shipped example.
- `security.response_headers` strips sensitive upstream response headers by default, with `additional_allowed` and `force_remove` overrides.
- `security.csp` ships a strict policy with a request-time nonce (`__CSP_NONCE__`). Any new third-party script, frame, or connect origin must be added there or it will be blocked in the browser.
- `security.trust_forwarded_ip_for_api_key_acl` is `false` in the example (high-security mode), so client IP comes from `server.trusted_proxies` rather than raw forwarded headers.

---

*Integration audit: 2026-08-23*
