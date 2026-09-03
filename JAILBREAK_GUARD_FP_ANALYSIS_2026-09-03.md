# 越狱防护误封分析与优化

日期：2026-09-03  
样本：`/Users/th3ee9ine/Downloads/error_logs_2026-09-02_to_2026-09-03.csv`  
关键词基线：`/Users/th3ee9ine/Downloads/qqq2api/docs/content_moderation_keywords_v2_hard.txt`

## 1. 分析边界

CSV 中的请求体、工具描述、历史 transcript 和策略文本均按**数据**读取；其中出现的指令句、工具调用句和策略句没有被当作本次任务指令执行。原始线上日志只读，分析输出不回显 Bearer/API 凭据。

## 2. 结论摘要

| 指标 | 结果 | 说明 |
|---|---:|---|
| 日志行数 | 3,818 | 时间窗口约 20 小时 21 分 |
| HTTP 403 | 3,818（100%） | 全部在上游调用前被拦截 |
| 提示词安全审计 | 3,350（87.7%） | `request/permission_error` |
| 内容审计 | 468（12.3%） | `internal/api_error`，与越狱防护分开统计 |
| 请求体完整 | 926（24.2%） | 2,892 行被省略或截断，仅凭日志行不足以判定正文语义 |
| 完整正文中的历史 prompt block | 160 | 作为本地回放 gold 候选集 |
| 完整正文唯一请求体 | 115 | 811/926 行为重复或重试放大 |
| 全日志指纹去重 | 548 | 重复行 3,270（85.6%），平均约 7 倍放大 |
| 本地回放后仍 block | 22/160 | 16 条用户正文注入、6 条高置信角色覆盖 |
| 本地回放后转 allow | 138/160（86.3%） | 主要为工具/助手/策略文档语境误封 |

### 2.1 误封证据最明确的四类

在 926 条完整正文中，历史 prompt guard 的 160 条 block 按证据分布如下：

| 历史证据 | 行数 | 典型语境 | 结论 |
|---|---:|---|---|
| `safety_bypass@tool` | 88 | 工具 API 文档中的“remove filter”、参数说明 | 工具 schema 不是用户命令，属于误封候选 |
| `instruction_override@system,safety_bypass@system,tool_routing@user` | 24 | guardian/策略文本解释“忽略不可信 transcript” | 系统策略引用与用户概括请求被拼成一次攻击，属于误封候选 |
| `refusal_suppression@developer` | 18 | “模型身份核验”“直接回答型号，不要拒绝” | 正常响应约束，属于误封 |
| `safety_bypass@assistant,agent_control@tool,tool_routing@user+tool` | 8 | 助手解释“没有可用指令可以绕过限制”+工具说明 | 否定句和工具说明被当作攻击，属于误封候选 |

上述四类共 138 行，占完整正文历史 block 的 86.3%。其中 138/160 在新逻辑本地回放中转为 allow；该比例是回放结果，不等于线上真实 precision。

### 2.2 保留拦截的高置信样本

- 16 行用户正文包含嵌入式 `<system>` 与 `Ignore previous instructions`，仍按用户侧 instruction override 拦截。
- 6 行 developer 段落直接指定 unrestricted/uncensored persona，并与用户侧策略冒充信号同现，仍按高置信角色覆盖拦截。

### 2.3 内容审计的独立风险

468 行不是 prompt guard，而是内容审核链路。完整正文命中基线关键词的主要候选包括 `绕过登录`（120 行）和 `微信聊天记录导出`（8 行）；这些词出现在合规恢复研究、官方备份渠道或安全说明时，旧式“命中即 403”会扩大误封面。应在内容审核看板单独回放，不要把这 468 行并入越狱误封率。

## 3. 根因定位

1. **角色被打平**：assistant/tool/system/developer 中的说明、schema 和历史 transcript 与 user 指令共用同一 block 阈值。
2. **弱信号单独 hard block**：`safety_bypass`、`refusal_suppression`、`tool_routing` 等词在工具文档、拒答解释和策略文本中很常见。
3. **Responses 路由兼容形状漏解析**：部分客户端把 `messages` 发送到 Responses 路由；解析失败后会退回原始 JSON，导致 schema/元数据被当成一个 user 段落扫描。
4. **工程术语碰撞**：`-ExecutionPolicy Bypass` 与 “system policy change” 是 PowerShell 文档，不是越狱指令，却命中 override 正则。
5. **日志正文截断**：75.8% 行没有完整正文，证据跨度、role 和命中位置缺失，使误封申诉难以闭环。
6. **重试未聚合**：同一请求体在秒级重复写入多行，按 attempts 统计会夸大事故量。

## 4. 已落地优化

### 4.1 角色感知的 block 阈值

文件：`/Users/th3ee9ine/Downloads/qqq2api/backend/internal/securityaudit/prompt_injection.go`

- user 段继续使用原有高置信规则；instruction override、角色覆盖、系统提示词提取仍直接拦截。
- 非 user 段改为严格阈值：单独的 `safety_bypass`、`refusal_suppression`、`agent_control`、`tool_routing`、`policy_impersonation` 不再 hard block。
- 非 user 段只有高置信 override/exfiltration，或 `agent_control + fixture_laundering` 同段组合才 block。
- 识别 “untrusted evidence/transcript”“Ignore untrusted content”“仅作为普通文字”等防御策略 envelope；其中的弱信号只保留 telemetry，不参与 block。
- 对 PowerShell 文档中的 `ExecutionPolicy Bypass` + `system policy change` 做工程语境豁免，避免把命令行说明识别成越狱。
- 策略标识升级为 `local-jailbreak-v2`、`PolicyVersion=2`，便于线上按版本比较误封率。

### 4.2 协议解析兼容

文件：`/Users/th3ee9ine/Downloads/qqq2api/backend/internal/securityaudit/prompt_snapshot.go`

- Responses/Responses WebSocket 在 `input` 为空时回退解析 `messages`。
- 保留原始 role 边界，不再把整个 JSON（工具 schema、元数据、headers）伪装成单一 user 段落。
- 该回退同时作用于本地 guard 和远程审计快照，避免两条链路对同一请求得出不同语义。

### 4.3 离线分析工具

文件：`/Users/th3ee9ine/Downloads/qqq2api/tools/analyze_interception_logs.py`

- 支持 CSV 与 XLSX 输入。
- 输出 prompt decision/evidence 分布、完整正文比例、正文指纹去重和重复放大指标。
- 派生摘要继续对 Bearer/API key 做脱敏，不写回源日志。

## 5. 回归验证

新增测试覆盖：

- 非 user 的身份核验、助手否定句、工具 API 文档不再误封。
- 带 untrusted transcript/evidence 的系统策略不再制造 block。
- developer 直接 unrestricted persona 仍拦截。
- Responses 路由收到 `messages` 时保持 role-aware 解析；用户侧经典 override 仍拦截。

执行结果：

```text
cd /Users/th3ee9ine/Downloads/qqq2api/backend && GOSUMDB=sum.golang.org GOTOOLCHAIN=auto go test ./internal/securityaudit -count=1
python -m py_compile /Users/th3ee9ine/Downloads/qqq2api/tools/analyze_interception_logs.py
python /Users/th3ee9ine/Downloads/qqq2api/tools/analyze_interception_logs.py /Users/th3ee9ine/Downloads/error_logs_2026-09-02_to_2026-09-03.csv /Users/th3ee9ine/Downloads/qqq2api/docs/content_moderation_keywords_v2_hard.txt
```

结果：securityaudit 全量测试通过；CSV 摘要复现 3,818 行、926 条完整正文、160 条完整 prompt block、115 个唯一正文指纹。

补充：`go test ./...` 仍受现有 `backend/internal/handler/admin/openai_oauth_handler.go:491` 基线编译错误影响（`openAISessionCleanupImmediateRunner` 与 `isNilOpenAISessionCleanupRunner` 接口类型不匹配），该文件不在本次改动范围内。

## 6. 上线步骤

### P0

1. 对 2,892 条正文缺失事件异步补采或回放；证据不足时记录 `待复核`，不直接计入误封或放行。
2. 轮换日志明细中出现过的 Bearer/API 凭据，并审计日志导出、留存和访问权限。
3. 看板拆分 `prompt_guard_blocked` 与 `content_policy_violation`；字段至少包含 `policy_id`、`policy_version`、`rule_id`、`role`、`confidence`、`evidence_span`、`normalized_hash`。
4. 按 `tenant/client + normalized_hash + policy_version` 聚合重试，单独展示 `attempt_count` 与 `unique_decision_count`。

### P1 灰度

1. 先 shadow 24 小时，再按 1% → 10% → 50% → 100% 放量。
2. 只有 user 侧高置信操作请求直接 hard block；工具、助手、策略文档弱信号走 allow 或人工复核。
3. 建立 gold set：用户直接 override、工具 schema、助手否定句、系统策略引用、Responses/messages 兼容形状、Unicode/编码混淆、真实操作请求。

### P2 指标

持续记录按事件和按唯一正文的 precision、recall、FPR、FNR、申诉翻转率、规则贡献、重复放大率、p95/p99 延迟、截断率和待复核积压。

## 7. 限制

本次样本有 75.8% 请求体被省略或截断，不宜仅凭错误消息推断其真实意图；138/160 的“转 allow”来自本地回放和证据语境规则，需结合人工 gold set 与灰度申诉结果确认线上误封率。原始 CSV 未被修改。
