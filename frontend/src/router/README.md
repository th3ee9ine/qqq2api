# Vue Router Configuration

The frontend is an administrator-only panel. The router keeps the setup and
login shell public, requires an administrator session for every panel tool,
and redirects removed self-service URLs before a lazy view can be loaded.

## Active routes

### Public

| Path | Purpose |
| --- | --- |
| `/setup` | First-run setup wizard |
| `/home` | Public landing page |
| `/login` | Administrator password/TOTP login |
| `/key-usage` | API key usage lookup |
| `/legal/:documentId` | Legal document viewer |

### Administrator

| Path | Purpose |
| --- | --- |
| `/keys` | Global API key management |
| `/batch-image` | Batch image API guide |
| `/admin/dashboard` | System dashboard |
| `/admin/ops` | Operations monitoring |
| `/admin/audit-logs` | Audit logs |
| `/admin/groups` | Routing groups |
| `/admin/accounts` | Upstream accounts |
| `/admin/proxies` | Proxy management |
| `/admin/settings` | System settings |
| `/admin/risk-control` | Risk control |
| `/admin/prompt-audit` | Prompt audit |

`/` redirects to `/home`, and `/admin` redirects to `/admin/dashboard`.

## Removed routes

User registration, OAuth callbacks, password recovery, user dashboard/profile,
subscriptions, redemption, payment, affiliates, channel status/management,
announcements, user management, and the model plaza are not registered.
Legacy bookmarks for those paths are redirected to `/admin/dashboard` for an
authenticated administrator and to `/login` otherwise.

## Guard rules

1. Restore the persisted session once on initial navigation.
2. Redirect removed feature, user, and self-service authentication paths.
3. Keep `/setup` reachable only while initial setup is required.
4. Require an authenticated administrator for every protected route.
5. Apply administrator compliance and feature-specific checks.
6. In backend mode, expose only the small public allowlist to anonymous users.

Client-side guards are navigation controls only. The backend must continue to
enforce authentication and authorization for every API route.

## Adding a route

Add the route to `index.ts`, use a lazy component import, set
`requiresAuth`/`requiresAdmin` explicitly where useful, update this document,
and add a guard or route-table test under `__tests__`.
