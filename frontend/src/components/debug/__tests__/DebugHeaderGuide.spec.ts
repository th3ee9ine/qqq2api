import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import DebugHeaderGuide from '../DebugHeaderGuide.vue'
import type { UpstreamHeaderDetail } from '@/api/admin/debugWorkbench'

const details: UpstreamHeaderDetail[] = [
  { name: 'Authorization', purpose: '鉴别调用账号', requirement: 'required', condition: '所有请求', source: '账号凭据', default_included: true },
  { name: 'User-Agent', purpose: '标识客户端', requirement: 'recommended', condition: 'Codex 请求', source: '客户端配置', default_included: true },
  { name: 'X-Codex-Turn-State', purpose: '延续轮次状态', requirement: 'conditional', condition: '仅回传上游返回的状态', source: '前序响应', default_included: false },
  { name: 'Content-Length', purpose: '请求体长度', requirement: 'transport', condition: '由 HTTP 传输层确定', source: 'HTTP 客户端', default_included: false }
]

describe('DebugHeaderGuide', () => {
  it('keeps the guide collapsed and explains requirement, purpose, condition and source', () => {
    const wrapper = mount(DebugHeaderGuide, { props: { details, headers: '{"Authorization":"redacted"}' } })
    expect(wrapper.attributes('open')).toBeUndefined()
    expect(wrapper.get('summary').text()).toContain('请求头用途与适用条件')
    expect(wrapper.get('summary').text()).toContain('4 项说明')
    expect(wrapper.findAll('.header-requirement').map((item) => item.text())).toEqual(['必要', '建议', '按条件', '自动'])
    const auth = wrapper.get('[data-header="Authorization"]')
    expect(auth.text()).toContain('鉴别调用账号')
    expect(auth.text()).toContain('所有请求')
    expect(auth.text()).toContain('账号凭据')
    expect(wrapper.text()).toContain('不代表请求已发送')
  })

  it('matches current JSON keys case-insensitively without inserting conditional headers', async () => {
    const headers = '{"authorization":"redacted"}'
    const wrapper = mount(DebugHeaderGuide, { props: { details, headers } })
    expect(wrapper.get('[data-header="Authorization"] .header-presence').text()).toBe('默认包含')
    expect(wrapper.get('[data-header="X-Codex-Turn-State"] .header-presence').text()).toBe('未配置')
    expect(wrapper.get('[data-header="Content-Length"] .header-presence').text()).toBe('传输层')
    await wrapper.setProps({ headers: '{"x-codex-turn-state":"opaque"}' })
    expect(wrapper.get('[data-header="Authorization"] .header-presence').text()).toBe('未配置')
    expect(wrapper.get('[data-header="X-Codex-Turn-State"] .header-presence').text()).toBe('已配置')
    expect(wrapper.emitted()).toEqual({})
  })

  it.each(['{invalid', '[]', 'null', '{"Authorization":123}'])('does not claim header presence for invalid JSON %s', (headers) => {
    const wrapper = mount(DebugHeaderGuide, { props: { details, headers } })
    expect(wrapper.get('[data-header="Authorization"] .header-presence').text()).toBe('待校验')
    expect(wrapper.get('[data-header="Content-Length"] .header-presence').text()).toBe('传输层')
  })

  it('supports missing metadata and a separate loading state', async () => {
    const wrapper = mount(DebugHeaderGuide, { props: { details: [], headers: '{}' } })
    expect(wrapper.text()).toContain('本次后端未返回请求头说明')
    expect(wrapper.findAll('.header-guide-item')).toHaveLength(0)
    await wrapper.setProps({ loading: true })
    expect(wrapper.get('[role="status"]').text()).toContain('正在同步当前账号与接口')
    expect(wrapper.text()).not.toContain('未返回请求头说明')
  })

  it('renders metadata as plain text, never upstream HTML', () => {
    const wrapper = mount(DebugHeaderGuide, { props: { details: [{ ...details[0], purpose: '<img src=x onerror=alert(1)>' }], headers: '{}' } })
    expect(wrapper.text()).toContain('<img src=x onerror=alert(1)>')
    expect(wrapper.find('img').exists()).toBe(false)
  })
})
