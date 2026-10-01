# Administrator layout integration

Register authenticated pages in `src/router/index.ts` and wrap their views in `AppLayout`. Use `requiresAdmin: true` for global administrator functionality, or an explicit `requiredPermission` for delegated account/proxy operations. Route checks accompany server-side authorization.

```typescript
{
  path: '/admin/accounts',
  name: 'AdminAccounts',
  component: () => import('@/views/admin/AccountsView.vue'),
  meta: {
    requiresAuth: true,
    requiredPermission: 'accounts.manage',
    title: 'Account Management',
    titleKey: 'admin.accounts.title',
  },
}
```

Keep public routes limited to the homepage, administrator login, setup wizard, legal documents, and API-key usage query. Removed end-user and third-party panel-login URLs are handled by the retired-route guard and must not load feature components.
