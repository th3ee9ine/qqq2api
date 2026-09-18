# Codex Turn State（全局覆盖与账号自动生命周期）

## 入口与操作

入口：**系统设置 → GPT → Codex Turn State（全局 / 实验性）**。两个开关独立，默认均关闭：

- **全局手动覆盖**：粘贴实际由上游签发的 `X-Codex-Turn-State`，开启后统一覆盖匹配请求。
- **自动采集、生成与续期**：无需粘贴 Token。采集真实上游响应；匹配模型的请求发现账号 Token 缺失或接近参考期限时，异步发起额外 Responses 探测，从上游响应头获取新值。

全局 Token 只写不回显：留空保留，输入新值替换，点击“清除”并保存才删除。重复保存相同值不会重置设置时间。关闭任一开关保留已存 Token；关闭自动开关停止自动采集、自动注入和后续探测。已经发出的探测可能执行到超时，关闭后其返回值会丢弃。

无需数据库迁移或重启。保存后当前进程配置缓存失效；其他共享数据库实例最多经过约 60 秒缓存周期读取新配置。

## 自动采集、生成与续期

1. **采集**：收集 Responses JSON / SSE、Compact 的实际下发响应头以及成功的 WebSocket 握手头。SSE 首输出守卫只在暂存头真正提交后采集；失败切换丢弃的暂存响应不会被采集为可用值。312 失效信号在收到上游响应头时立即处理，不等待 SSE 首输出或响应体解析。相同 Token 不重复写库，不重置采集时间。
2. **生成**：发出 `POST https://chatgpt.com/backend-api/codex/responses`，请求一个极短回复，使用 `stream: true`、`store: false`，删除旧的 `X-Codex-Turn-State`。使用触发请求的最终上游模型，未提供时默认 `gpt-5.5`。从 2xx 响应头采集有效 HTTP 头格式的新值。没有该头、非 2xx 或网络失败均算失败；不伪造本地 Fernet 签名。
3. **续期**：以公开 Fernet 时间戳加一小时作为参考期限，无法解析时使用首次采集时间；未来异常时间戳不会无限延长寿命。匹配请求在参考期限前十分钟触发异步续期，续期期间继续使用未到期值；超过参考期限的自动值不再注入。上游不保证每次都签发不同的新 Token；返回同一值不会重置它的年龄。
4. **触发方式**：由符合模型范围的网关请求触发，不扫描空闲账号、不做无人使用账号的定时保活。首个请求不会等待探测；本次无 Token 时正常转发，探测成功供后续请求使用。
5. **请求凭据和出口**：沿用 OpenAI 网关 HTTP 传输及账号代理。OAuth 使用现有 access-token provider；Setup Token 使用其 bearer 凭据；Agent Identity 使用现有 assertion / task 注册流程。探测前重新读取账号，跳过已禁用、暂停或限流等不可调度账号。
6. **并发和成本控制**：单进程每账号串行合并采集与探测，同时最多八个后台任务；常规及恢复失败后的每账号探测间隔至少五分钟；每次新的 312 失效事件允许一次立即恢复探测；一次探测最多二十秒，单次持久化最多三秒；只读取最多 128 KiB 探测响应体。该大小限制仅用于维护探测，不限制用户的 SSE 流。
7. **失败处理**：保留已有 Token，记录固定错误码和最后探测时间。Turn State 探测的 HTTP / 网络失败不写账号错误状态、不禁用账号。OAuth access-token 刷新、Agent Identity task 注册仍遵循项目原有凭据生命周期策略。
8. **隔离和保存**：状态按账号 ID 存储在 `account.extra`，后台线程不修改请求或调度器共享的账号对象。采集结果即时进入本地缓存并异步写库；同时到达的真实响应优先于更早发出的探测结果。普通账号编辑保留受管状态，导出 / 导入 / 复制不迁移该状态。更新时间和固定错误码可查看账号 API 的安全诊断字段。

额外探测**可能消耗上游模型额度**，无需普通用户请求等待不代表免费。持久化探测时间可以减少多实例重复请求，但不是严格分布式锁：同时首次触发的多个实例仍可能各探测一次。后台任务满载时按账号合并排队，空出任务槽后自动继续；数据库写入失败时保留本地状态，在至少五秒后的后续请求上重试持久化。

## 312 信号与重新获取 292

292 / 312 指 Fernet Token 的带填充 Base64URL 字符数，对应 10 / 11 个密文块。解析按实际块数判断，省略 `=` 的同一 Token 也按同一状态处理；不使用 HTTP 292 / 312 状态码或任意等长字符串作为信号。

开启自动模式后，收到 312 会立即清空该账号的可用自动值，记录失效时间和旧值摘要，并触发一次跳过常规五分钟冷却的恢复探测。即使旧 292 仍在一小时参考期限内，也不允许继续自动使用；匹配到已撤销的手动全局值、客户端回带或 WS 会话状态时同样剥离。

恢复必须拿到 **不同于已撤销值、签发时间不早于此次失效所在秒、尚未过期且时间合理的 10 块新状态**。未知封装、再次收到 312、旧 Token 或仅改变 `=` 填充不能解除恢复状态。首次探测失败后保留五分钟退避；重复 312 不无限重置冷却，不循环重放用户请求。新值尚未拿到时请求不携带旧状态正常转发。

HTTP 请求、WS 握手和后台探测记录发出时的恢复代次；失效前发出的响应即使晚到、甚至带另一个同秒签发的 292，也不能覆盖失效后的状态。新的真实请求可用其响应中的新 292 完成恢复。恢复元数据持久化并从普通 DTO 和导出中脱敏，进程重启加载后仍阻止旧值回灌；多实例感知仍取决于账号快照同步，不是跨实例实时屏障。

这些是应用基于用户指定信号的恢复策略，密文块数本身不构成上游模型质量或签名有效性的证明。

## 注入优先级与模型范围

优先级：**未被撤销的手动全局 Token → 未被撤销的原生客户端 / 会话续链状态 → 当前账号自动 Token**。开启自动模式后，312 失效规则优先于这些来源和 TTL。

手动全局值适用于 OpenAI OAuth / Setup Token 的 Codex 协议；API Key 和其他平台不注入。自动采集值仅用于同一账号，原有跨账号回带保护继续执行。手动全局覆盖不验证 Token 的归属或可跨账号使用性。

模型范围由两个模式共享：为空匹配全部；逗号或换行分隔，忽略大小写、去重，支持精确值和末尾 `*` 前缀匹配，规范化后最多 1024 字节。客户端模型或映射后上游模型任一命中即可；两者均未知时也允许。真实响应可被采集，但自动注入和主动探测只由匹配范围的请求触发。探测优先使用请求最终上游模型。

Token 去除首尾空白后仅允许可打印 ASCII，最多 4096 字节。Fernet 解析仅读取公开封装、密文块数和时间戳，不解密、不验证签名。10 块与一小时 TTL 是经验诊断，不是官方有效性证明。**手动全局值**保持参考项目的警告语义：未知封装、超基线、过期和未来时间戳只提示，不阻止显式注入；自动模式使用参考期限和 312 提前失效信号共同管理状态。

## 覆盖路径

- Responses HTTP（JSON / SSE）、Responses 透传和 `/responses/compact`。
- 经共同 Responses 链路转发的 Chat Completions、Messages 兼容请求，以及 HTTP → WebSocket 桥接。
- WebSocket 入站、池化 / 专用连接、passthrough 直连握手。获取和拨号时重新解析配置，自动值变化时按 Token 摘要区分新握手；关闭覆盖时恢复原生状态。
- 账号 Responses 测试与调试工作台默认请求头支持全局手动配置并脱敏。工作台默认模板生成只读，不会启动自动付费探测。

HTTP 头仅在 WebSocket 握手发送，不能改写已有连接的握手。普通配置或自动值变化不强制中断正在使用或已绑定续链的连接；新建 / 可轮换连接使用新值。收到 312 后改变账号的恢复代次，下一次获取连接时旧代次连接不能继续复用，固定续链也不能绕过该检查；无法迁移的固定续链走现有连接不可用处理，可能需要客户端重建会话。在途响应不自动重放。此功能不替换正常请求的模型、reasoning 或 HTTP 状态码，不添加无限 Overload 重试。

## 系统设置 API

仅启用自动能力：

```json
{
  "openai_codex_turn_state_auto_enabled": true,
  "openai_codex_turn_state_models": "gpt-5.5,gpt-5.5-*"
}
```

可选的手动覆盖：

```json
{
  "openai_codex_turn_state_enabled": true,
  "openai_codex_turn_state": "TOKEN",
  "openai_codex_turn_state_models": "gpt-5.5,gpt-5.5-*"
}
```

使用 `PUT /api/v1/admin/settings`。`TOKEN` 必须替换为实际值。字段省略或 `null` 表示保留；显式 `"openai_codex_turn_state": ""` 清除手动全局值。两个开关关闭都不会删除已保存的 Token。

GET / PUT 响应不返回明文 Token，只包含：

- `openai_codex_turn_state_enabled`、`openai_codex_turn_state_auto_enabled`
- `openai_codex_turn_state_configured`、`openai_codex_turn_state_models`、只读 `openai_codex_turn_state_set_at_ms`
- `openai_codex_turn_state_status`：手动配置诊断 `enabled/configured/active/reason/verdict/blocks/issued_at/expires_at`

账号详情和列表的 `codex_turn_state_auto` 为只读诊断：`configured/set_at_ms/probe_at_ms/expires_at_ms/due/last_error/recovery_pending/invalidated_at_ms`，不包含 Token。`due` 表示按参考寿命需要续期，不代表开关已开启或探测正在运行。`last_error` 仅允许固定码，例如 `state_312`、`recovery_requires_new_292`、`http_429`、`missing_state`、`transport_failed`；不包含上游错误正文、代理地址或凭据。受管 Extra 字段从普通 DTO 和账号导出中移除；设置审计只记录变更字段名。

## 验证边界

参考 [gpt-load 本地代码](/Users/th3ee9ine/Downloads/gpt-load-main/web/src/frontends/classic/lib/codex-turn-state.ts) 的显式注入、模型范围和公开封装诊断语义。自动生命周期在此基础上增加。公开时间与密文长度不能证明签名有效、上游必然接受、模型质量提升或避免 Overload。

测试使用模拟上游检查采集提交时机、账号隔离、并发去重、异步生成、提前续期、失败冷却、禁用行为、HTTP/Compact/WS 请求头策略、设置保留与脱敏以及前端保存。模拟测试通过不等于真实上游一定返回新的 Turn State，也不构成质量收益证明。

真实账号联调入口为 `TestCodexTurnStateLiveIntegration`，默认跳过。显式设置 `CODEX_TURN_STATE_LIVE_ACCOUNT_FILE` 为仅含一个 OpenAI OAuth 账号的导出 JSON 绝对路径后才会联网；`CODEX_TURN_STATE_LIVE_MODEL` 可覆盖默认模型。每轮最多八次 Responses 请求，使用真实服务请求构造、后台探测和恢复逻辑，内存仓库与标准 `net/http` 网络适配器。测试不导入生产数据库、不修改原账号文件、不刷新登录凭据；需提供仍有效的 access token。适配器遵循 `HTTPS_PROXY`，不会自动读取 macOS 系统代理。不要把该测试当作生产连接池、数据库、OAuth 刷新或 WS 的端到端验证。

联调先检查自动获取和携带，再通过调整本地采集计时触发真实续期探测；没有自然 312 时，仅向本地恢复逻辑注入明确标记的模拟信号，再向上游获取真实新值。模拟信号不会发送给上游。探测成功的定义是收到符合恢复策略的响应头 state，不能据此把 SSE 的 `response.failed` 计为推理完成。

2026-09-18 的实测结果和边界见 [脱敏联调报告](CODEX_TURN_STATE_LIVE_TEST_20260918.md)。
