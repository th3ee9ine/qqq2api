import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import CodexTurnStateProxyUrlEditor from '../CodexTurnStateProxyUrlEditor.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({
    t: (key: string, values?: Record<string, unknown>) => `${key}${values ? ` ${Object.values(values).join(' ')}` : ''}`,
  }),
}))

describe('CodexTurnStateProxyUrlEditor', () => {
  it('keeps loaded URLs editable and masks credentials by default', async () => {
    const wrapper = mount(CodexTurnStateProxyUrlEditor, {
      props: { modelValue: ['socks5://existing:secret@proxy.example:1080'] },
      global: { stubs: { Icon: true } },
    })

    const input = wrapper.get<HTMLInputElement>('[data-testid="codex-turn-state-proxy-url-0"]')
    expect(input.attributes('type')).toBe('password')
    expect(input.element.value).toBe('socks5://existing:secret@proxy.example:1080')

    await wrapper.get('[data-testid="codex-turn-state-proxy-url-visibility-0"]').trigger('click')
    expect(wrapper.get('[data-testid="codex-turn-state-proxy-url-0"]').attributes('type')).toBe('text')
    await wrapper.get('[data-testid="codex-turn-state-proxy-url-0"]').setValue('socks5://edited:secret@proxy.example:1081')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['socks5://edited:secret@proxy.example:1081']])
  })

  it('adds, validates, and removes URL rows', async () => {
    const wrapper = mount(CodexTurnStateProxyUrlEditor, {
      props: { modelValue: [] },
      global: { stubs: { Icon: true } },
    })

    await wrapper.get('[data-testid="codex-turn-state-proxy-url-add"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([['']])
    await wrapper.setProps({ modelValue: [''] })
    expect(wrapper.get('[data-testid="codex-turn-state-proxy-url-error-0"]').text()).toContain('errors.required')
    expect(wrapper.emitted('validity-change')?.at(-1)).toEqual([false])

    await wrapper.get('[data-testid="codex-turn-state-proxy-url-0"]').setValue('http://user:pass@proxy.example:1080')
    await wrapper.setProps({ modelValue: ['http://user:pass@proxy.example:1080'] })
    expect(wrapper.get('[data-testid="codex-turn-state-proxy-url-error-0"]').text()).toContain('errors.scheme')

    await wrapper.get('[data-testid="codex-turn-state-proxy-url-remove-0"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
  })

  it('prevents a 257th dedicated URL', () => {
    const urls = Array.from({ length: 256 }, (_, index) => `socks5://user:password@proxy-${index}.example:1080`)
    const wrapper = mount(CodexTurnStateProxyUrlEditor, {
      props: { modelValue: urls },
      global: { stubs: { Icon: true } },
    })

    expect(wrapper.get<HTMLButtonElement>('[data-testid="codex-turn-state-proxy-url-add"]').element.disabled).toBe(true)
  })
})
