# Codex Turn State 历史真实账号联调记录：2026-09-18

> 历史报告，不是当前恢复结论。报告中的 `gpt-5.5` 默认值、旧探测契约和 state 长度只描述当日实验，不能作为当前配置、state 有效性或模型质量的判据。当前实现必须同时核对 `response.created`、`response.completed`、usage log 原始上游模型和同账号回放结果。

本次使用用户提供的一个真实 OpenAI OAuth 账号，在本地测试进程内完成 HTTP Responses 联调。没有导入生产数据库，没有改写原账号 JSON，报告及日志不包含账号标识、邮箱、登录凭据或 Turn State 明文。

当日账号测试和 Turn State 探测模型使用了 `gpt-5.5`；这不是当前默认值，也不代表该模型与当前 Astra/auto-review 家族规则互通。

## 前置观察

- 原始直连请求在 10 秒建连超时处失败；本机 macOS 已配置 `127.0.0.1:7890` 代理，后续测试通过 `HTTPS_PROXY` 使用这个出口。
- 原默认 `gpt-5.4` 的两次请求均收到 HTTP 400，错误为该模型不支持此 ChatGPT 账号。
- 真实模型清单请求返回 HTTP 200，列出了 `gpt-5.5`。修改默认值后进行以下完整联调。
- access token 的公开到期时间为 2026-09-28，本次未触发 OAuth 登录凭据刷新。下面的续期专指 Turn State。

## 完整联调记录

执行时间：2026-09-18 13:51:57 至约 13:53:20（Asia/Shanghai）；耗时 83.40 秒，共六次 `gpt-5.5` Responses 请求。

| 次序 | 场景 | 发出 state | HTTP / 新响应 state | SSE 结果 | 响应头耗时 |
| --- | --- | --- | --- | --- | --- |
| 1 | 首次后台自动探测 | 无 | 200 / 292、10 块 | `response.completed`，输出 OK | 3417 ms |
| 2 | 普通请求携带初始 state | 10 块 | 200 / 无新 state | `response.completed`，输出 OK | 1472 ms |
| 3 | 再次携带初始 state | 10 块 | 200 / 无新 state | `response.completed`，输出 OK | 1100 ms |
| 4 | 模拟接近续期时间后真实探测 | 无 | 200 / 不同的新 292 | `response.completed`，输出 OK | 1228 ms |
| 5 | 本地模拟 312 后真实恢复探测 | 无 | 200 / 不同的新 292 | **`response.failed`，没有完成推理** | 2562 ms |
| 6 | 携带恢复后的新 state | 10 块 | 200 / 无新 state | `response.completed`，输出 OK | 679 ms |

首次触发自动探测的调用在不到 1 ms 内返回，没有等待后台网络请求。真实 state 的公开签发时间分别为 05:51:59Z、05:52:38Z、05:52:49Z。续期返回值和初始值不同；模拟失效后，旧值立即不再允许注入，恢复值也与被撤销值不同，最终 `configured=true`、`recovery_pending=false`。

当日没有自然收到 312。报告只证明了当日测试请求的网络行为；292、312 等长度不能证明 state 有效、失效、账号类型或恢复成功，也没有等待一小时自然到期。

第 5 次请求的响应头发放了新 292，但推理流失败。旧探测契约曾按 2xx 响应头采集 state；按当前规则，这不足以完成恢复，必须通过 created/completed 模型和 usage log 验证。本报告保留这个区别：取得 state、完成一次推理和通过模型/usage 验证是不同结果。

## 验证范围

- 真实测试复用了服务的普通请求构造、HTTP 响应 state 采集、后台自动探测、续期和撤销恢复逻辑。
- 持久化使用内存仓库，真实网络使用标准 `net/http` 适配器；本次未覆盖生产数据库、生产 HTTP 连接池、多实例同步、WS 或 OAuth 刷新。
- 真实测试默认跳过，必须显式设置 `CODEX_TURN_STATE_LIVE_ACCOUNT_FILE`；单轮上限八次 Responses 请求，所有探测均有超时。
- 后端相关测试使用 `-race` 验证通过：Turn State、OpenAI 测试默认值、探测、账号请求与相关 admin / DTO 路径。该结果不是后端全量测试结论。
- 前端五个测试文件共 87 项通过：账号测试弹窗、调试工作台、系统设置、Turn State 工具函数、中英文键完整性。

脱敏原始输出：[CODEX_TURN_STATE_LIVE_TEST_20260918.txt](CODEX_TURN_STATE_LIVE_TEST_20260918.txt)。输出已检查未包含所提供账号的凭据值。日志中的行号对应当次测试源文件，不是上游响应位置。
