# 连接池实时诊断

管理员接口 `GET /api/v1/admin/ops/runtime/connection-pools` 返回处理当前请求的实例的 DB、Redis 连接池内存快照。沿用管理员认证与账号管理员范围限制，响应使用 `Cache-Control: no-store`。Ops 被禁用时返回 404；服务未初始化时返回 503。

该接口不执行 SQL、Redis 命令或健康探测，不等待可用连接，不写入数据库，不受分钟级指标采集 leader 的限制。现有 Ops 仪表盘、历史指标及可靠性工作台的响应保持原有结构。

响应仍使用标准 `{ "code": 0, "data": ... }` 包装。`data` 字段如下：

| 字段 | 含义 |
| --- | --- |
| `scope` | 固定为 `instance`，不是集群总量 |
| `instance_id` | 当前进程的随机标识，重启后变化，不包含主机名或凭证 |
| `collected_at` | UTC 采集时间 |
| `counter_scope` | 固定为 `client_lifetime`；累计计数随底层客户端重建而归零 |
| `database` | PostgreSQL 客户端池状态；未提供该能力时为 `null` |
| `redis` | 共享 Redis 客户端的主连接池状态；未提供该能力时为 `null` |

`database` 包含：

| 字段 | 含义 |
| --- | --- |
| `max_open_connections` | 客户端配置的打开连接上限，0 表示未限制 |
| `open_connections` / `in_use` / `idle` | 当前打开、占用及空闲连接数 |
| `wait_count_total` | 累计等待连接次数，含最终被上下文取消的等待 |
| `wait_duration_ms_total` | 已结束等待的累计耗时，单位毫秒，保留小数 |

`redis` 包含：

| 字段 | 含义 |
| --- | --- |
| `pool_size` | go-redis 的基础池大小 |
| `max_active_conns` | 可选连接总数硬上限，0 表示未限制 |
| `total_conns` / `idle_conns` | 主池当前总连接数及空闲连接数 |
| `pending_requests` | 当前等待取得连接的请求数量 |
| `wait_count_total` | 当前 go-redis 版本记录的累计成功等待次数 |
| `wait_duration_ms_total` | 成功等待的累计耗时，单位毫秒，保留小数 |
| `timeouts_total` | 累计连接池等待超时次数；不代表所有 Redis 命令/网络超时 |

建议按实例每 5–15 秒采样，以累计计数增量和实时连接占用判断拥塞。不要将 `wait_count_total` 当成当前队列人数；DB 标准池统计没有提供等价的实时等待人数。Redis 等待超时并不计入成功等待次数，分析时同时查看 `timeouts_total`。

负载均衡入口可能轮换到不同实例，计算增量时必须按 `instance_id` 分组；计数下降时应视为客户端重建或计数器回绕。收集所有实例才能得到完整集群视图。接口没有历史存储，也不会自动改变连接池或准入上限。
