# Layout components

The application provides an administrator console with a restricted account-administrator role.

- `AppLayout.vue` wraps authenticated administrator pages with the responsive sidebar and header.
- `AppSidebar.vue` shows the permitted account, proxy, key, group, usage, operations, audit, and settings routes. Navigation is filtered by administrator permissions.
- `AppHeader.vue` shows the current operator identity, role, page title, and logout action.
- `AuthLayout.vue` wraps administrator password login and its TOTP challenge.
- `TablePageLayout.vue` provides the shared table page shell.

Only registered routes in `src/router/index.ts` are available. The sidebar does not support custom menu entries or end-user pages.

```vue
<script setup lang="ts">
import AppLayout from '@/components/layout/AppLayout.vue'
</script>

<template>
  <AppLayout>
    <h1>Administrator workspace</h1>
  </AppLayout>
</template>
```
