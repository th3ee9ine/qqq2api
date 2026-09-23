# QQQ2API v0.2.5 同步记录

本次在 QQQ2API 3.0.5 定制版上合并官方 sub2api v0.2.5，保留现有管理界面、功能裁剪和性能优化。

## 来源与版本

- 定制版基线：`db023814d8e921527609dd422795d034ba9c6351`。
- 官方发布：[sub2api v0.2.5](https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.5)，发布时间为 2026-09-15 09:02:34 UTC。
- 官方标签所指提交：`86f93c28ee34cc74b629dafb748bd5ac5ca8c5ea`。
- 本地分支：`codex/sync-sub2api-v0.2.5`。
- 两处定制版本文件 `VERSION`、`backend/cmd/server/VERSION` 均保持 `3.0.5`。

仓库原有 `v0.2.5` 标签与官方标签不是同一个对象。本次将官方标签单独取到 `refs/remotes/upstream/tags/v0.2.5` 后合并，没有覆盖原有同名标签，也没有引入官方标签之后的 main 提交。

## 主要同步内容

| 范围 | 合并后的行为 |
| --- | --- |
| OpenAI WebSocket | 同步上下文池容量计算、账号池变更后等待者重新选号、常驻 reader 应答 ping、脏连接及上游已关闭连接清理、失败重试换连接等修复。 |
| 会话隔离 | WS/HTTP 按原始客户端身份及执行作用域隔离状态与抢占，保留 Codex 不同线程、不同请求类型的隔离。 |
| OAuth 图片 | 支持的图片模型走原生 Codex Images；保留 Responses image_generation 路径及正式转发中的 404/405 回退。调试工作台模板同步实际 URL、请求体及 Accept 头。 |
| 协议兼容 | 同步 Responses Lite 命名空间工具、done/终止事件文本恢复、agent_message 正文、system 消息合并和 cache_control 保留等修复。 |
| 性能与调度 | 同步大体积图片请求内存优化、Codex 配额规范窗口和重置时间、Anthropic 调度阈值快照元数据修复。 |
| Codex 身份及模型 | 在解析身份前校验原始 User-Agent，保留管理员合法自定义 UA/Originator；同步模型目录上下文窗口、显示名称及新版套餐名称。 |
| API Key 管理 | 支持批量编辑、创建时按保留的平台过滤分组；配额重置后同步 Key 状态。继续使用定制的全局 Key 管理与分组绑定。 |
| 代理管理 | 区分未提交凭据与显式空字符串，支持清空用户名/密码；筛选条件变化回到第一页，并保留定制账号绑定和 MaxAccounts。 |
| 运维与费用 | 请求详情增加 TTFT，用量费用显示提升至 8 位小数，合并错误详情及统计修复。 |
| Fast 策略 | 全局策略支持匹配未指定 service_tier 的请求，并按显式配置提升为 priority。继续采用定制的全局规则语义。 |

WS 的 OAuth/API Key 连接上限系数默认值由 1.0 调整为 5.0，并继续受 `max_conns_per_account` 限制；显式配置值优先。默认压缩模型由 `gpt-5.4` 更新为 `gpt-5.5`。这些是本次继承的上游默认行为变化。

## 定制行为的保留

- 保留 Anthropic、OpenAI 入口，组合分组继续只支持这两类平台。
- 原有订阅、支付、用户管理及其他已退役页面继续关闭。上游新增的平台兼容代码及数据库约束不构成管理入口重新开放。
- 保留定制后台布局、账号与代理绑定、账号会话管理、调试工作台、审计和全局 API Key 管理。
- 保留 Codex 管理员自定义 UA/Originator、固定或同步客户端版本及账号身份处理。
- 保留上游 HTTP Transport 的并发初始化优化、API Key 缓存处理、Redis MGET/Pipeline、64 KiB 初始 SSE 缓冲池、TLS/代理建连超时和流结束后的脱离请求取消的限时持久化。
- Go 模块导入继续使用 `github.com/th3ee9ine/qqq2api`，并重新生成依赖注入代码以整合服务参数变化。

## 数据库迁移

仅增加以下官方迁移，未修改已有迁移的内容：

- `238_opencode_go_platform.sql`：扩展平台 CHECK 约束，包含 OpenCode。
- `238_purge_unlimited_user_platform_quotas.sql`：删除日、周、月限额均为 NULL 的无限额占位记录；无记录继续表示不限额。

迁移执行器按完整文件名排序和记录校验，两份文件前缀同为 238 不会互相覆盖。本次执行了迁移包测试，没有连接部署数据库执行迁移。

## 验证记录

验证日期：2026-09-16。基线取自独立工作树中的 `db023814d`，避免将原有失败误归因于本次同步。

- 前端 `pnpm run typecheck`、`pnpm run lint:check`、`pnpm run build` 均通过。
- 前端完整测试：1,544 个用例，1,515 通过，29 失败；基线为 1,463 个用例，1,434 通过，29 失败。失败断言及失败文件集合完全一致。
- Go 静态检查通过，范围覆盖 service、repository、handler、server、setup、pkg。
- 带前端嵌入资源的后端生产编译通过，运行 `--version` 返回 `QQQ2API 3.0.5`。
- Codex 自定义身份、原始 UA 校验、图片测试模板及真实请求头、可见模型检索等针对性回归通过。
- WS 连接池、执行作用域、线程抢占、规范配额窗口、代理凭据更新，以及 Redis/TLS/流式性能回归通过。带 `unit` 标签的部分使用下述临时 overlay。

### 首次同步时的基线问题

前端失败文件包括代理 API 旧断言、旧 Grok 账号测试、已删除 SupportedModelChip/CustomPageView 的测试、旧注册入口断言，以及 AccountActionMenu 定位和 Spark shadow 测试。此次未扩大到这些历史问题的修复。

首次同步时 Go 默认完整测试：按顶层测试统计，6,404 通过、11 跳过、8 失败，失败集合与基线完全一致。11 个跳过项涉及外部 PostgreSQL/Redis 和插件运行时集成环境；不将它们计为通过。以下 8 个失败已在后续修复中处理，原因和处理方式见下一节。

以下 8 个失败在同步前基线均可复现（账号成本用例列出当前通用化后的名称）：

- `TestOpenAIResponsesWebSocket_SessionUpdateToAllowedModelStillWorks`
- `TestGatewayRoutesGroupModelAllowlistMountedOnEveryGatewayRoute`
- `TestGatewayRoutesGroupModelAllowlistCoversRootAliasRoutes`
- `TestAPIKeyAuthSnapshotProfitControlRoundtrip`
- `TestOpenAIGatewayServiceRecordUsage_AccountStatsUsesUpstreamModel`
- `TestCanonicalOpenAIAccountSchedulingModelMatchesForwardSemantics`
- `TestQueryUsageResetCreditDetails401NonFatal`
- `TestGetOpenAIUsage_SparkShadow_WritesExtraAndReturnsNonEmptyWindows`

### 后续修复：8 个后端基线失败

| 失败项 | 原因与修复 |
| --- | --- |
| 路由挂载与根路径覆盖两项 | 根路径图片生成、编辑和异步接口未经过分组模型白名单。统一接入 `rootRoute` 中间件链，补上异步图片及单模型检索的行为断言；已退役的 Gemini、Antigravity、视频、语音路由改为断言仍不可用。 |
| WebSocket 会话更新 | 测试桩将 `session.update` 错误回复为 `response.completed`，把控制帧当成第三次计费调用。改为 `session.updated`，分别统计控制帧与实际请求，并验证切换到另一个白名单模型后仅有两笔正确的用量记录。 |
| API Key 认证快照 | 测试硬编码旧版 v23，实际模型白名单快照已是 v24。按当前版本和最低兼容版本校验，并补充白名单 JSON 往返保真断言。 |
| 上游账号成本 | 旧测试仍要求扣用户钱包，与定制全局 API Key 行为不符。保留上游模型和账号成本断言，并验证记录费用但不扣钱包。 |
| 调度模型规范化 | 测试依赖进程中残留的 Grok 跨客户端映射开关。显式设置并恢复运行时配置，同时覆盖关闭和开启两种状态。 |
| 重置积分详情返回 401 | 配额查询会独立保存付费积分缓存，旧断言错误要求完全没有写入。改为验证不覆盖重置积分快照，且缓存不完整详情时不会额外写库。 |
| Spark 影子账号用量 | 旧测试假设首个 Extra 更新就是配额窗口，实际付费积分快照先写入。按更新内容等待窗口持久化，并校验写入影子账号及返回的 5 小时、7 天利用率。 |

修复后的验证结果：

- 无缓存完整回归 `go test -p 4 -count=1 -json ./...` 通过：6,413 个顶层测试通过、0 失败、11 跳过，原 8 个失败均在完整回归中通过。
- 相关 WebSocket、Spark 用量、积分缓存、模型映射及路由用例使用 `-race -count=3 -shuffle=on` 通过；最终会话更新断言再次通过相同竞态检查。
- service、handler、routes 的 `go vet`，以及 `gofmt`、`git diff --check` 通过。
- 带前端资源的服务端生产编译通过，版本仍为 `3.0.5`。

以上完整回归使用默认构建标签；下节所述 `unit` 标签的历史编译问题不在这 8 项修复范围内。

### unit 标签的验证边界

基线 `go test -tags=unit ./...` 已存在编译问题：重复的 `mockSettingRepo` 定义、旧代理测试引用已不存在的 `ClearBackupID`/指针类型 `ExpiryWarnDays`，以及调度快照测试的 `count`/`buckets` 变量错误。

为验证本次新增的 WS 和代理回归，使用 Go `-overlay` 在临时目录中去除重复 mock、排除两个不兼容的旧代理测试断言并修正上述变量名；仓库中的原有坏测试没有因此被改写。故针对性回归通过不等价于原始 `-tags=unit ./...` 全套通过。

常规复核命令如下，Go 命令在 `backend` 目录运行：

```sh
GOMAXPROCS=4 GOSUMDB=sum.golang.org GOTOOLCHAIN=auto go test -p 4 ./...
GOMAXPROCS=4 GOSUMDB=sum.golang.org GOTOOLCHAIN=auto go vet -p 4 ./internal/service ./internal/repository ./internal/handler/... ./internal/server/... ./internal/setup ./internal/pkg/...
GOMAXPROCS=4 GOSUMDB=sum.golang.org GOTOOLCHAIN=auto CGO_ENABLED=0 go build -p 4 -tags embed -ldflags '-X main.Version=3.0.5' -o /tmp/qqq2api-v025-server ./cmd/server
```

## 后续定制清理：移除 DeepSeek 专用支持

移除平台常量、默认 API 地址、余额解析、监控供应商注册、数据库实体枚举，以及专用模型识别、原生 Responses 适配、Codex 模型描述和 Ollama 模型特例。删除 4 条内置价卡、官方价格强制覆盖、峰谷倍率和 Pro 转 Flash 的日期切换逻辑。

前端同步清除平台类型、颜色、图标、中英文接入说明和模型示例。通用的 OpenAI/Anthropic 协议转换、reasoning_content 回传、工具调用和模型映射继续保留，并用通用模型测试数据验证。

历史 SQL 迁移及对应校验测试保持不变，避免已安装实例升级时发生 checksum mismatch。生产代码中仅保留旧平台拒绝标识与 OpenAI OAuth 外部模型过滤前缀；相关拒绝、隐藏和无内置能力的回归测试保留，以防历史账号重新进入调度。没有删除数据库中的历史账号或用量数据。

本轮补充验证：

- 默认标签完整后端回归 `go test -json ./...` 通过：6,408 个顶层测试通过、0 失败、11 跳过；此前修复的 8 项回归再次全部通过。
- 前端 `typecheck`、lint、生产构建通过；完整测试为 1,514 通过、29 失败，失败集合与同步后基线完全一致。移除一个专用平台参数化用例，因此通过数比此前少 1。
- 带 `unit` 标签的受影响文件补充测试使用前述临时 overlay：484 个顶层测试通过，仅 `TestGatewayServiceRecordUsage_GeminiFlashThinkingTierUsesCatalogPrice` 失败（两个 Gemini 模型子用例）。该失败已用修改前 HEAD 的源文件 overlay 独立复现；没有新增失败。原始 `-tags=unit ./...` 仍有上节记录的历史编译问题。
- `go vet ./...`、Ent 实体重新生成、带 `embed` 的服务端构建通过；集成测试源码可编译，未连接真实数据库运行。
- 前端构建产物与内置价卡没有该供应商条目，历史迁移目录没有改动。生成的服务端二进制版本检查为 `3.0.5`。

## 发布状态

本记录对应本地代码同步与验证，未推送 Git 远端、发布 Docker 镜像或部署服务。
