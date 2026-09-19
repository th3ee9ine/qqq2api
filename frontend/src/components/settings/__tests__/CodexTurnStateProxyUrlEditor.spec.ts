import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import CodexTurnStateProxyUrlEditor from '../CodexTurnStateProxyUrlEditor.vue'
import { CODEX_TURN_STATE_PROXY_BATCH_MAX_LINES } from '@/utils/codexTurnState'

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
    expect(wrapper.get<HTMLButtonElement>('[data-testid="codex-turn-state-proxy-url-batch-open"]').element.disabled).toBe(true)
  })

  it('batch-appends normalized URLs and skips duplicates without changing current entries', async () => {
    const wrapper = mount(CodexTurnStateProxyUrlEditor, {
      props: { modelValue: ['socks5://existing:secret@proxy.example:1080'] },
      global: {
        stubs: {
          Icon: true,
          BaseDialog: {
            props: ['show', 'title'],
            template: '<section v-if="show" data-testid="batch-dialog"><slot /><footer><slot name="footer" /></footer></section>',
          },
        },
      },
    })

    await wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-open"]').trigger('click')
    await wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-input"]').setValue([
      'socks5://existing:secret@PROXY.EXAMPLE:1080',
      ' socks5://new:secret@NEW.EXAMPLE:1081 ',
      '',
      'socks5://other:secret@other.example:1082',
      'socks5://new:secret@new.example:1081',
    ].join('\r\n'))

    const summary = wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-summary"]').text()
    expect(summary).toContain('batchReady 2')
    expect(summary).toContain('batchDuplicate 2')
    expect(wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-input"]').classes()).toContain('is-masked')
    await wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-visibility"]').trigger('click')
    expect(wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-input"]').classes()).not.toContain('is-masked')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="codex-turn-state-proxy-url-batch-apply"]').element.disabled).toBe(false)

    await wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-apply"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[
      'socks5://existing:secret@proxy.example:1080',
      'socks5://new:secret@new.example:1081',
      'socks5://other:secret@other.example:1082',
    ]])
    expect(wrapper.find('[data-testid="batch-dialog"]').exists()).toBe(false)

    await wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-open"]').trigger('click')
    expect(wrapper.get<HTMLTextAreaElement>('[data-testid="codex-turn-state-proxy-url-batch-input"]').element.value).toBe('')
    expect(wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-input"]').classes()).not.toContain('is-masked')
  })

  it('atomically blocks a batch with invalid rows and reports line-level errors', async () => {
    const wrapper = mount(CodexTurnStateProxyUrlEditor, {
      props: { modelValue: [] },
      global: {
        stubs: {
          Icon: true,
          BaseDialog: {
            props: ['show', 'title'],
            template: '<section v-if="show"><slot /><footer><slot name="footer" /></footer></section>',
          },
        },
      },
    })

    await wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-open"]').trigger('click')
    await wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-input"]').setValue([
      'socks5://valid:secret@proxy.example:1080',
      'http://invalid:secret@proxy.example:1080',
    ].join('\n'))

    expect(wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-error-2"]').text()).toContain('errors.scheme')
    expect(wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-input"]').attributes('aria-describedby')).toContain('codex-turn-state-proxy-url-batch-errors')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="codex-turn-state-proxy-url-batch-apply"]').element.disabled).toBe(true)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('atomically blocks a batch that would exceed the 256 URL limit', async () => {
    const urls = Array.from({ length: 255 }, (_, index) => `socks5://user:password@proxy-${index}.example:1080`)
    const wrapper = mount(CodexTurnStateProxyUrlEditor, {
      props: { modelValue: urls },
      global: {
        stubs: {
          Icon: true,
          BaseDialog: {
            props: ['show', 'title'],
            template: '<section v-if="show"><slot /><footer><slot name="footer" /></footer></section>',
          },
        },
      },
    })

    await wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-open"]').trigger('click')
    await wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-input"]').setValue([
      'socks5://new:password@new-1.example:1080',
      'socks5://new:password@new-2.example:1080',
    ].join('\n'))

    expect(wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-overflow"]').text()).toContain('batchOverflow 1 256')
    expect(wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-input"]').attributes('aria-describedby')).toContain('codex-turn-state-proxy-url-batch-overflow')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="codex-turn-state-proxy-url-batch-apply"]').element.disabled).toBe(true)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('deduplicates the current pool before applying a batch at the raw row limit', async () => {
    const urls = Array.from({ length: 255 }, (_, index) => `socks5://user:password@proxy-${index}.example:1080`)
    urls.push('socks5://%75ser:password@PROXY-0.EXAMPLE:1080')
    const wrapper = mount(CodexTurnStateProxyUrlEditor, {
      props: { modelValue: urls },
      global: {
        stubs: {
          Icon: true,
          BaseDialog: {
            props: ['show', 'title'],
            template: '<section v-if="show"><slot /><footer><slot name="footer" /></footer></section>',
          },
        },
      },
    })

    expect(wrapper.get<HTMLButtonElement>('[data-testid="codex-turn-state-proxy-url-add"]').element.disabled).toBe(false)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="codex-turn-state-proxy-url-batch-open"]').element.disabled).toBe(false)
    await wrapper.get('[data-testid="codex-turn-state-proxy-url-visibility-255"]').trigger('click')
    expect(wrapper.get('[data-testid="codex-turn-state-proxy-url-255"]').attributes('type')).toBe('text')
    await wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-open"]').trigger('click')
    await wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-input"]').setValue('socks5://new:password@new.example:1080')
    await wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-apply"]').trigger('click')

    const emitted = wrapper.emitted('update:modelValue')?.at(-1)?.[0] as string[]
    expect(emitted).toHaveLength(256)
    expect(emitted.at(-1)).toBe('socks5://new:password@new.example:1080')
    await wrapper.setProps({ modelValue: emitted })
    expect(wrapper.get('[data-testid="codex-turn-state-proxy-url-255"]').attributes('type')).toBe('password')
  })

  it('blocks oversized physical-line input and recovers after it is replaced', async () => {
    const wrapper = mount(CodexTurnStateProxyUrlEditor, {
      props: { modelValue: [] },
      global: {
        stubs: {
          Icon: true,
          BaseDialog: {
            props: ['show', 'title'],
            template: '<section v-if="show"><slot /><footer><slot name="footer" /></footer></section>',
          },
        },
      },
    })

    await wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-open"]').trigger('click')
    const input = wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-input"]')
    await input.setValue('\n'.repeat(CODEX_TURN_STATE_PROXY_BATCH_MAX_LINES))

    expect(wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-input-error"]').text()).toContain(`batchTooManyLines ${CODEX_TURN_STATE_PROXY_BATCH_MAX_LINES}`)
    expect(input.attributes('aria-describedby')).toContain('codex-turn-state-proxy-url-batch-input-error')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="codex-turn-state-proxy-url-batch-apply"]').element.disabled).toBe(true)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    await input.setValue('socks5://new:password@new.example:1080')
    expect(wrapper.find('[data-testid="codex-turn-state-proxy-url-batch-input-error"]').exists()).toBe(false)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="codex-turn-state-proxy-url-batch-apply"]').element.disabled).toBe(false)
  })
})
