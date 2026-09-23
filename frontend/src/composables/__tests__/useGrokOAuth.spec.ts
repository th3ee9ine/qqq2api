import { describe, expect, it, vi } from 'vitest'

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn()
  })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => {
      const messages: Record<string, string> = {
        'admin.accounts.oauth.grok.failedToExchangeCode': 'Grok 授权码兑换失败',
        'admin.accounts.oauth.grok.errors.GROK_OAUTH_INVALID_STATE':
          'Grok OAuth state 与当前会话不匹配。请粘贴同一次生成的授权链接返回的回调 URL。'
      }
      return messages[key] ?? key
    }
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    grok: {
      generateAuthUrl: vi.fn(),
      exchangeCode: vi.fn(),
      refreshGrokToken: vi.fn()
    }
  }
}))

import { useGrokOAuth } from '@/composables/useGrokOAuth'
import { adminAPI } from '@/api/admin'

describe('useGrokOAuth.exchangeAuthCode', () => {
  it('shows a state mismatch recovery hint from structured backend errors', async () => {
    vi.mocked(adminAPI.grok.exchangeCode).mockRejectedValueOnce({
      status: 400,
      reason: 'GROK_OAUTH_INVALID_STATE',
      message: 'invalid oauth state'
    })
    const oauth = useGrokOAuth()

    const tokenInfo = await oauth.exchangeAuthCode({
      code: 'code',
      sessionId: 'session-id',
      state: 'wrong-state'
    })

    expect(tokenInfo).toBeNull()
    expect(oauth.error.value).toBe(
      'Grok OAuth state 与当前会话不匹配。请粘贴同一次生成的授权链接返回的回调 URL。'
    )
  })
})

describe('useGrokOAuth.buildCredentials', () => {
  it('builds OAuth credentials without forcing base_url or leaking sso/password', () => {
    const oauth = useGrokOAuth()

    const credentials = oauth.buildCredentials({
      access_token: 'access-token',
      token_type: 'Bearer',
      expires_at: 1_900_000_000,
      client_id: 'client-id',
      scope: 'openid grok-cli:access',
      email: 'grok@example.com',
      password: 'super-secret',
      sso_token: 'sso-cookie',
      sso: 'sso-cookie',
      'sso-rw': 'sso-cookie'
    } as any)

    expect(credentials.access_token).toBe('access-token')
    expect(credentials.email).toBe('grok@example.com')
    // System/CLI mode chooses the correct host; do not pin public API URL.
    expect(credentials.base_url).toBeUndefined()
    expect(credentials).not.toHaveProperty('password')
    expect(credentials).not.toHaveProperty('sso_token')
    expect(credentials).not.toHaveProperty('sso')
    expect(credentials).not.toHaveProperty('sso-rw')
  })
})

describe('useGrokOAuth stale authorization responses', () => {
  it('does not apply a refresh-token response after the proxy or method resets the flow', async () => {
    let complete!: (value: { access_token: string }) => void
    vi.mocked(adminAPI.grok.refreshGrokToken).mockImplementationOnce(() => new Promise(resolve => { complete = resolve }))
    const oauth = useGrokOAuth()
    const pending = oauth.validateRefreshToken('old-refresh', 1)
    oauth.resetState()
    complete({ access_token: 'old-access' })
    expect(await pending).toBeNull()
    expect(oauth.loading.value).toBe(false)
    expect(oauth.error.value).toBe('')
  })

  it('keeps the newer OAuth session when an earlier URL generation finishes late', async () => {
    let complete!: (value: { auth_url: string; session_id: string; state: string }) => void
    vi.mocked(adminAPI.grok.generateAuthUrl)
      .mockImplementationOnce(() => new Promise(resolve => { complete = resolve }))
      .mockResolvedValueOnce({ auth_url: 'https://example.com/new', session_id: 'new-session', state: 'new-state' })
    const oauth = useGrokOAuth()
    const pending = oauth.generateAuthUrl(1)
    oauth.resetState()
    await oauth.generateAuthUrl(2)
    complete({ auth_url: 'https://example.com/old', session_id: 'old-session', state: 'old-state' })
    expect(await pending).toBe(false)
    expect(oauth.sessionId.value).toBe('new-session')
    expect(oauth.state.value).toBe('new-state')
  })

  it('ignores credentials after the component scope has been disposed', async () => {
    const { effectScope } = await import('vue')
    let complete!: (value: { access_token: string }) => void
    vi.mocked(adminAPI.grok.refreshGrokToken).mockImplementationOnce(() => new Promise(resolve => { complete = resolve }))
    const scope = effectScope()
    const oauth = scope.run(() => useGrokOAuth())!
    const pending = oauth.validateRefreshToken('refresh-token')
    scope.stop()
    complete({ access_token: 'discard-me' })
    expect(await pending).toBeNull()
  })
})
