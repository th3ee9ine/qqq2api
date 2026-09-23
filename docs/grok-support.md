# Grok 接入与使用

QQQ2API 支持 Grok API Key 和 OAuth 账号，可用于独立 Grok 分组，也可作为 Composite 分组的目标平台。实现参考本地 `sub2api-main`，保留 QQQ2API 的账号管理员归属、请求审核、代理及计费逻辑。

## 接入步骤

1. 在「账号管理 → 添加账号」选择 **Grok**。
2. 选择凭据方式：
   - **API Key**：填写 xAI API Key，默认上游为 `https://api.x.ai`，也可配置兼容上游地址。
   - **OAuth**：支持授权链接与回调码、Refresh Token、SSO Token 导入。系统只保存兑换后的 OAuth 凭据，清除 SSO/密码残留。
   - 密码授权默认关闭。需要时由部署者设置 `GATEWAY_GROK_PASSWORD_AUTH_ENABLED=true`；前端根据服务端能力显示入口。
3. 将账号加入 Grok 分组，或为 Composite 分组配置目标平台为 `grok` 的模型路由。
4. 为调用方 API Key 绑定相应分组。在账号测试中验证模型；OAuth 账号可主动查询配额与媒体可用状态。

OAuth 账号的上游地址留空时遵循系统设置，默认使用 CLI 通道。API Key 账号默认使用官方 API 地址。系统设置可调整默认文本模型、跨客户端模型映射和默认上游通道；账号显式配置优先。

模型目录包含 `grok-4.7`、`grok-4.6` 等文本模型，以及 Grok Imagine 图片和视频模型。内置默认文本模型为 `grok-4.6`，可在系统设置改为其他可用模型。目录列出模型不代表账号一定具有该模型权限，实际可用性由上游账号和配额决定。

## 请求入口

下表以 `/v1` 为前缀。除 Messages 请求需要使用 `/v1/messages` 外，表中其他入口也提供对应的根路径兼容路由。

| 能力 | 入口 |
| --- | --- |
| 模型发现 | `GET /v1/models`、`GET /v1/models/:model` |
| OpenAI Chat Completions | `POST /v1/chat/completions` |
| Responses 与流式响应 | `POST /v1/responses`、`GET /v1/responses` WebSocket 桥接 |
| Claude Messages 兼容 | `POST /v1/messages`、`POST /v1/messages/count_tokens` |
| 图片生成与编辑 | `POST /v1/images/generations`、`POST /v1/images/edits` |
| 视频生成 | `POST /v1/videos`、`POST /v1/videos/generations` |
| 视频编辑与延长 | `POST /v1/videos/edits`、`POST /v1/videos/extensions` |
| 视频任务与内容 | `GET /v1/videos/:request_id`、`GET /v1/videos/:request_id/content` |
| 语音生成与识别 | `POST /v1/tts`、`POST /v1/stt` |
| 自定义语音 | `/v1/custom-voices` 及 `/:voice_id`、`/:voice_id/audio` 子资源 |
| 实时音频 | `GET /v1/realtime` WebSocket |
| 搜索 | `POST /v1/web_search`、`POST /v1/x_search` |

图片与视频受分组「允许图片生成」媒体开关和账号媒体资格控制。Grok Realtime 使用独立语音入口；分组的 OpenAI Live 开关不控制此入口。图片、视频、搜索和语音的价格可在分组中配置。Composite 路由需匹配目标模型；语音、搜索等专用入口使用独立 Grok 分组。

示例请求（`HOST`、`PORT`、`TOKEN` 替换为当前部署与调用方 API Key）：

```sh
curl "http://HOST:PORT/v1/chat/completions" \
  -H "Authorization: Bearer TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"model":"grok-4.7","messages":[{"role":"user","content":"你好"}],"stream":true}'
```

## 配额与运维

- OAuth 刷新已接入后台刷新服务及共享 token 缓存，导入后会进行有并发限制的配额探测。
- 明确识别为免费套餐的 OAuth 账号默认启用本地滚动配额保护：24 小时 500,000 token，在 95% 处停止新调度。未知或付费套餐不按免费套餐判断。此阈值是本地调度策略，不代表 xAI 的实时官方额度。
- 主动配额查询保留冷却期间的诊断能力。xAI 没有对应的 OAuth 配额重置接口，重置操作会返回不支持。
- 受限账号管理员可维护自己名下 Grok 账号；全局 OAuth reconciliation 和运行时诊断仅超级管理员可访问。
- 媒体、音频和搜索请求沿用现有计费与错误处理。文本审核在调度、计费检查及上游调用之前执行。

本次变更不需要修改既有数据库迁移。验证使用本地测试及模拟上游；真实 xAI 授权、账号权限与网络连通性需通过部署后的账号测试确认。
