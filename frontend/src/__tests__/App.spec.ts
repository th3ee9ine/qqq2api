import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import App from '@/App.vue'
import i18n, { setLocale } from '@/i18n'

const mocks = vi.hoisted(() => ({
  documentTitle: 'Accounts - Test Gateway',
  fetchPublicSettings: vi.fn(),
  getSetupStatus: vi.fn(),
  replace: vi.fn(),
  resolveRouteDocumentTitle: vi.fn(),
  updateFavicon: vi.fn(),
}))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRouter: () => ({ replace: mocks.replace }),
    useRoute: () => ({
      fullPath: '/admin/accounts',
      path: '/admin/accounts',
      name: 'AdminAccounts',
      params: {},
      meta: {
        title: 'Accounts',
        titleKey: 'admin.accounts.title',
      },
    }),
  }
})

vi.mock('@/api/setup', () => ({
  getSetupStatus: mocks.getSetupStatus,
}))

vi.mock('@/router/title', () => ({
  resolveRouteDocumentTitle: mocks.resolveRouteDocumentTitle,
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    siteLogo: '',
    siteName: 'Test Gateway',
    cachedPublicSettings: null,
    fetchPublicSettings: mocks.fetchPublicSettings,
  }),
  useAuthStore: () => ({ isAdmin: false }),
  useAdminSettingsStore: () => ({ customMenuItems: [] }),
}))

vi.mock('@/utils/branding', () => ({
  updateFavicon: mocks.updateFavicon,
}))

describe('App document title', () => {
  beforeEach(() => {
    localStorage.clear()
    i18n.global.locale.value = 'en'
    mocks.documentTitle = 'Accounts - Test Gateway'
    mocks.fetchPublicSettings.mockReset().mockResolvedValue(undefined)
    mocks.getSetupStatus.mockReset().mockResolvedValue({ needs_setup: false })
    mocks.replace.mockReset()
    mocks.resolveRouteDocumentTitle.mockReset().mockImplementation(() => mocks.documentTitle)
    mocks.updateFavicon.mockReset()
  })

  afterEach(() => {
    i18n.global.locale.value = 'en'
  })

  it('re-resolves the title after the locale changes', async () => {
    const wrapper = mount(App, {
      global: {
        plugins: [i18n],
        stubs: {
          NavigationProgress: true,
          RouterView: true,
          Toast: true,
        },
      },
    })
    await flushPromises()

    const callsBeforeSwitch = mocks.resolveRouteDocumentTitle.mock.calls.length
    mocks.documentTitle = '账号 - Test Gateway'

    await setLocale('zh')
    await nextTick()

    expect(mocks.resolveRouteDocumentTitle).toHaveBeenCalledTimes(callsBeforeSwitch + 1)
    expect(document.title).toBe('账号 - Test Gateway')

    wrapper.unmount()
  })
})
