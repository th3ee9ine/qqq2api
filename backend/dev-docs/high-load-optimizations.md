# 高峰负载优化与验证

本次修改针对 qqq2api 网关自身的重试放大、重复鉴权查询和连接缓存锁竞争。网关不控制上游模型的 GPU 容量；这些修改减少本地资源浪费，并使容量不足时的行为可预测。没有进行生产流量压测，以下局部测量不能作为端到端 TTFT 或全站吞吐量提升的结论。

## 1. 容量不足时共享重试预算

新增配置：

```yaml
gateway:
  openai_capacity_retry_max_attempts: 3
```

环境变量为 `GATEWAY_OPENAI_CAPACITY_RETRY_MAX_ATTEMPTS`，四个 Compose 模板均已接入，默认值为 `3`。取值范围为 `0–10`，`0` 使用默认值 `3`，`1` 表示识别到容量失败后不再重试。配置需要重启实例生效。

预算从同一请求第一次被识别为上游容量失败的转发调用开始计算，包含这次失败。之后处理器层的同账号重试与跨账号切换共享额度；即使下一次返回处理器的失败原因不同，也不会重置额度。首次容量失败之前的其他尝试不计入这一预算。既有账号切换上限、账号可用性和客户端取消仍可使请求更早结束。

容量错误允许最多一次同账号恢复重试，为其他账号保留机会；账号已有的更严格重试配置继续有效。此类请求级瞬时故障不计入账号调度失败，也不据此临时摘除健康账号。普通 OAuth 429 的既有等待策略继续适用。

覆盖 Responses、Chat Completions、Messages、Embeddings、Images、AlphaSearch 和 Responses WebSocket 回合。成功的 WS 回合清空该回合的容量及同账号重试计数。

HTTP Responses 经 WSv2 出站时，输出前的容量错误事件、失败终止事件、预热错误和握手容量错误直接返回处理器，不再被服务内部当作普通断线进行多轮重连。普通网络重连和请求兼容修复继续使用原有独立限制。因此，这一配置限制处理器转发调用数；在混合网络错误或兼容修复的场景中，不能将其解释为所有物理拨号或 `response.create` 帧数量的统一硬上限。

此配置是尝试次数预算，不是首输出总耗时上限，也不是 SSE/WS 流的时长上限。HTTP 响应尚未提交时，容量重试耗尽返回 503 与上游的可操作错误信息；可用的 `Retry-After` 继续传递。已升级的 WS 连接使用既有协议错误帧及关闭流程。应避免客户端在 503 后立即无限重试。

## 2. 合并鉴权 L2 缓存查询

L1 正缓存和负缓存保持快速返回。L1 未命中且 singleflight 开启时，相同 API Key 的并发请求共享一次 L2 查询及必要的 DB 回退；加入共享查询后再次检查 L1，避免已经填充缓存后的远端重复读取。数据库查询合并在原实现中已经存在，本次新增收益是合并 Redis 读取。

每个调用者独立响应取消。发起共享查询的调用者退出，不取消仍在等待的其他请求；共享上下文保留必要的 context value。L2 与 DB 使用独立的 3 秒 context 预算，DB 的预算在 L2 查询返回后才开始，避免慢 Redis 耗尽 DB 回退机会。Redis 实际网络等待仍受现有 go-redis 连接、读写及重试配置约束，context deadline 不能被当作严格的 Redis 墙钟上限，也不能将整个共享加载理解为最多 3 秒。

缓存失效时 Forget 对应共享查询，使失效后的请求不会继续加入失效前的查询。既有在途读取与失效消息的竞态仍需要更完整的版本化方案处理，本次不宣称实现强一致缓存失效。

## 3. 缩短 HTTP 客户端缓存锁占用

冷缓存路径由全表 TTL 扫描改为轮转检查，每次最多检查 64 个条目。普通 HTTP 与 TLS 指纹客户端都在锁内解除缓存引用、在解锁后关闭空闲连接；并发创建中未被采用的 transport 也在锁外清理。

正在处理请求的连接不因 TTL/容量清理被淘汰。缓存数量硬上限保留；达到硬上限时仍会扫描寻找最旧的空闲客户端，该路径仍是 O(N)。TTL 是可回收阈值，实际清理由后续缓存未命中逐步推动，没有新增后台清理协程。

## 4. 连接池实时诊断

新增管理员接口：`GET /api/v1/admin/ops/runtime/connection-pools`。

该接口只读取当前进程的 DB/Redis 客户端内存统计，不执行 SQL 或 Redis 探测命令，不等待可用连接。沿用管理员权限及 Ops 开关，响应禁止缓存。包含连接占用、累计等待和 Redis 当前等待请求数等指标；字段及计数器边界见 [连接池实时诊断](ops-connection-pools.md)。

多实例部署应按 `instance_id` 分组采样并计算累计值增量。DB 的 `wait_count_total` 不是实时队列人数，Redis `pool_size` 不是连接总量的硬上限。接口未增加历史存储或自动调参功能。

## 本地测量

对照版本为 `112ca28a4`，环境为 Apple M3 Pro、darwin/arm64、Go 1.27.0。鉴权对照使用 Go overlay 加载旧源码，未切换或覆盖工作区修改；HTTP 缓存使用相同基准函数，在修改前后分别测量。

| 测量场景 | 修改前 | 修改后 | 含义 |
| --- | ---: | ---: | --- |
| 同一 API Key 的 64 个并发请求，L1 冷、L2 命中 | 64 次 Redis GET | 1 次 Redis GET | 本场景 Redis 读取减少 98.44%，两者 DB 读取均为 0 |
| HTTP 客户端缓存冷未命中，驻留 128 个账号 | 2,221 ns/op | 736.2 ns/op | 局部冷缓存路径耗时 |
| HTTP 客户端缓存冷未命中，驻留 5,000 个账号 | 63,571 ns/op | 920.8 ns/op | 不包含到达缓存硬上限后的全表淘汰 |
| HTTP 客户端缓存并行命中，驻留 5,000 个账号 | 144.2 ns/op | 129.2 ns/op | 本轮测量未见命中路径退化，不把小幅波动作为确定收益 |

HTTP 缓存数字为各三轮测量中位数；修改前每轮 500 ms，修改后每轮 1 s。冷未命中为轮转链表增加 64 B 和 1 次分配；命中路径维持 182 B、6 次分配。测量不包含上游推理、网络、排队或完整鉴权中间件。

可在 backend 目录复现缓存基准：

```sh
GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go test ./internal/repository -run '^$' -bench '^BenchmarkHTTPUpstreamClientCache$' -benchmem -benchtime=1s -count=3
```

## 验证记录

受影响的六个包已运行完整测试，而非只编译或仅执行新增测试：`internal/config`、`internal/service`、`internal/handler`、`internal/repository`、`internal/handler/admin`、`internal/server/routes`。除 config 外均包含 `unit` 构建标签。服务包完整测试耗时 140.716 秒，处理器包 40.857 秒，所有包通过；后端 `go build ./...` 通过。

另外执行了鉴权 singleflight 的 10 轮竞态检测、HTTP/TLS 客户端缓存的竞态检测，以及容量重试、WSv2、账号健康和连接池诊断路径的联合竞态检测，均通过。配置默认值、环境变量覆盖和四个 Compose 模板的变量一致性检查通过，`git diff --check` 通过。

回归覆盖慢 Redis 后 DB 正常回退、leader/follower 独立取消、缓存失效后不加入旧查询、活跃上游连接保护、关闭空闲连接不阻塞其他缓存命中、HTTP 200 SSE 容量错误、WS 多回合预算重置、WS 容量失败不触发内部重连、输出后不重放，以及诊断接口的权限和无远端探测行为。

复现主要检查时，在 backend 目录运行：

```sh
GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go test -tags=unit ./internal/config ./internal/service ./internal/handler ./internal/repository ./internal/handler/admin ./internal/server/routes -count=1 -timeout=180s
GOTOOLCHAIN=auto GOSUMDB=sum.golang.org go build ./...
sh ../deploy/tests/docker-compose-gateway-env-test.sh
```

测试使用本地 fixture、录制型上游及受控连接池，没有请求生产模型服务，也没有测量 GPU 推理速度。

## 上线观察与回退

先在一个实例使用默认容量预算 3，使用同类模型及相近输入/输出 token 分布对照首输出 P50/P95/P99、成功率、容量 503 比例、每个入口请求对应的上游尝试数，以及 DB/Redis 池等待增量。对比时区分主动限制重试后更快返回的 503 与成功请求的延迟变化，不使用全体平均延迟判断收益。

确认长 SSE、WS 多回合和客户端取消正常后再逐步扩大部署。真实容量持续不足时，网关优化不能保证恢复成功率，需要结合上游容量与流量控制处理。

本次没有数据库迁移。回退可使用上一构建及原配置。提高容量预算最多到 10 只会放宽次数限制，并不恢复旧的所有调度行为；`0` 也不是关闭保护。精确回退应回滚构建。变更没有实现自适应准入、公平排队、统一首输出时间预算或持久化异步计费队列。
