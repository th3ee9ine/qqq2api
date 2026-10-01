# Layout examples

Administrator table views use `TablePageLayout` inside `AppLayout`. See `AccountsView.vue` and `ProxiesView.vue` for permission checks, responsive controls, filtering, pagination, and bulk actions. Global settings use `SettingsView.vue` and administrator password/TOTP login uses `LoginView.vue`.

Use registered administrator routes for navigation and `useAuthStore().hasPermission()` for delegated controls. The backend remains authoritative for access control. End-user profile, registration, payment, subscription, and custom-menu examples have been removed.
