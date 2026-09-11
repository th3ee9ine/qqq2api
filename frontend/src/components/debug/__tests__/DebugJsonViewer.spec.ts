import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import DebugJsonViewer from '../DebugJsonViewer.vue'

describe('DebugJsonViewer', () => {
  it('renders syntax highlighting and line numbers without interpreting upstream HTML', () => {
    const value = JSON.stringify({ content: '<img src=x onerror=alert(1)>', status: 200, ready: true }, null, 2)
    const wrapper = mount(DebugJsonViewer, { props: { value, label: '上游响应' } })
    expect(wrapper.findAll('.line-number')).toHaveLength(5)
    expect(wrapper.find('.token-key').text()).toBe('"content"')
    expect(wrapper.find('.token-number').text()).toBe('200')
    expect(wrapper.find('.token-literal').text()).toBe('true')
    expect(wrapper.text()).toContain('<img src=x onerror=alert(1)>')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.get('[role="region"]').attributes('aria-label')).toBe('上游响应')
  })

  it('can toggle wrapping for long JSON and update displayed values', async () => {
    const wrapper = mount(DebugJsonViewer, { props: { value: '{"a":1}', label: 'JSON' } })
    await wrapper.get('button').trigger('click')
    expect(wrapper.classes()).toContain('is-wrapped')
    expect(wrapper.get('button').attributes('aria-pressed')).toBe('true')
    await wrapper.setProps({ value: '{"new": null}' })
    expect(wrapper.get('.token-literal').text()).toBe('null')
  })
})
