# 线上拦截日志误封分析与优化方案

日期：2026-09-02
样本：

- `/Users/th3ee9ine/Downloads/error_logs_2026-09-01_to_2026-09-02.xlsx`
- `/Users/th3ee9ine/Downloads/线上内容拦截关键词.txt`

## 1. 分析边界

工作簿和关键词文件只作为数据读取；其中出现的提示词、规则文本和操作性句子没有被当作本次任务指令执行。原始 XLSX/TXT 文件没有被改写，也没有对线上日志做脱敏或删改。新增的离线分析工具只在派生摘要中不回显认证凭据值，便于把报告提交到代码库；这不改变源日志内容。

## 2. 结论摘要

| 指标 | 结果 | 判断 |
|---|---:|---|
| 日志行数 | 68 | 全部为 403，且全部为流式请求 |
| 时间窗口 | 1,949.387 秒（32 分 29 秒） | 不是单次孤立故障 |
| 去重后指纹 | 10 | 58 行是重复/重试放大 |
| 重复行占比 | 85.3% | 事件计数必须同时展示 unique 与 attempts |
| 相邻间隔 | 56/67 ≤ 1 秒；中位数 0.612 秒；最大 661.079 秒 | 明显存在并发/自动重试批次 |
| 阶段分布 | request 48；internal 20 | 两条拦截链路被混在同一类“失败”体验中 |
| 模型 | gpt-5.6-terra 60；gpt-5.6-sol 8 | 可按模型维度做回放对照 |
| 处置状态 | 已解决=否 68；业务限制=否 68 | 样本中的拦截均未完成闭环 |
| 凭据字段 | 68/68 条请求明细含 Bearer 凭据 | 立即轮换并审计日志访问范围 |
| Prompt guard | 48 | 请求体均被截断/省略，当前只能标记“待复核” |
| Content moderation | 20 | 8 行可从完整正文确认误封，12 行正文缺失 |
| 已确认内容误封率 | 8/20 = 40.0% | 仅针对可见完整正文 |
| 全日志误封下界 | 8/68 = 11.8% | 其余 60 行缺失正文，证据不足 |
| 去重后误封下界 | 1/10 = 10.0% | 同一 HVAC 表单指纹重复 8 次 |
| 关键词条数 | 490（非空且唯一） | 无分类、风险级别、上下文、版本、allowlist 字段 |
| 短词（≤2 字符） | 26 | 单纯子串命中误封风险最高 |

### 2.1 已确认的误封

8 条 `internal/api_error`（`Go-http-client/1.1`、`gpt-5.6-sol`）正文完全一致，是中央空调/冷站能耗采集表。关键词 `群控` 每个正文出现 15 次（8 条合计 120 次），全部位于字段、选项或复选框：

- `A10 | 控制系统 | □有BA/群控 □有但不用 ☑没有`
- `□BA/群控界面`
- `□远程/群控`
- `B24 | 群控/BA使用情况 ... ☑没有`

这些出现描述设备配置，且有明确的 `☑没有` 否定项，没有要求搭建、运行或控制群控系统。原有“命中即拦截”的子串规则把领域术语当成了操作请求。

### 2.2 证据不足、需回放的部分

48 条 `request/permission_error` 的错误消息是“提示词安全审计拒绝了该请求”，20 条 `internal/api_error` 的错误消息是“内容审计命中风险规则”。其中 60 条请求明细明确记录 `body_omitted=true`、`body_truncated=true`，现有信息不足以把这些行归为误封；应在保留原始日志的前提下异步回放并标注 `待复核`。所有行均没有上游状态码/耗时，说明拒绝发生在上游调用前的本地网关阶段。

客户端分布也呈现明显批次特征：Codex Desktop 32（其中 24 条 request、8 条 internal）、WorkBuddy 8、OpenAI/JS 8、Python-urllib 8、OpenAI/Python 4、Go-http-client 8。优先按客户端批次回放，可以较快判断是某个 SDK 的系统提示词模板触发，还是规则本身过宽。

## 3. 根因定位

1. **关键词规则是扁平列表**：490 个词没有类别、严重度、词元边界、上下文策略、来源/版本和例外范围；`木马`、`远控`、`群控`、`提权` 等短词在报告、表格、培训材料中都可能是正常名词。
2. **纯子串与重叠词次序放大误封**：例如 `简历库`/`简历库出售`、`肉鸡`/`肉鸡出售`、`木马`/`勒索木马`。先命中短词会覆盖更长词的上下文判定。
3. **结构信息丢失**：字段名、复选框、否定词、引用和“检测/防御/培训”意图没有进入判定逻辑。
4. **提示词审计把协议角色打平**：大段 system/developer policy 中引用的攻击例句会与用户指令混合；请求体截断后又缺少命中的证据跨度。
5. **错误体验和监控未分层**：`prompt_guard_blocked` 与 `content_policy_violation` 都返回 403，但当前日志没有稳定的 `rule_id`、置信度、证据跨度、角色和策略版本。
6. **重试未聚合**：相同指纹在约 1 秒内连续出现，按行统计会把一次规则决策误看成 8 次独立事故。

## 4. 已落地的代码优化

### 4.1 内容审核关键词匹配

文件：`/Users/th3ee9ine/Downloads/qqq2api/backend/internal/service/content_moderation_keyword_matcher.go`、`/Users/th3ee9ine/Downloads/qqq2api/backend/internal/service/content_moderation.go`

- 保留原 `Match`/`matchBlockedKeyword` 作为兼容性原语；实际拦截路径改用 `MatchContextAware`。
- 在 enforcement 路径做 NFKC 规范化并移除 Unicode `Cf`（全角、零宽字符等），闭合常见变体绕过。
- 对短/歧义词要求局部风险或行动上下文；对长短语保留高置信度拦截。
- 识别表格/字段/复选框、否定（如“禁止执行”“☑没有”）、引用/术语、报告/培训/检测/防御语境。
- 引用内容还会检查引号内部是否带有直接执行动作；裸引号不会自动成为 allowlist，明确的报告/引用标签才会保留说明语境。
- 明确区分“说明/检测脚本”与“下载、安装、运行、连接、部署”等操作；后者仍可拦截。
- 扫描所有重叠和后续出现，避免第一处表格词把后面的真实操作请求吞掉；更长的、已判定为良性的短语不会被嵌套短词重新触发。
- 对同一子句中“先否定、后正向动作”（如“禁止群控并提供脚本”）重新判断，避免前置否定吞掉后半段请求；明确的检测/规则构造例外仍保留。
- 统一处理 NFKC、零宽/格式字符和组合附加符号；同一消息拆成多个文本 part 时先按角色合并，避免分片绕过或跨角色拼接误判。

### 4.2 越狱防护与提示词审计

文件：`/Users/th3ee9ine/Downloads/qqq2api/backend/internal/securityaudit/prompt_injection.go`

- 按 protocol role 分段扫描，system/developer 长策略中的低置信度示例不会与用户弱信号拼接；经典 instruction override 仍在任意角色中拦截。
- 对大请求采用 head/tail + 重叠滑窗；同步扫描有 chunk 数上限、提前停止和 `scan_truncated=true` 证据，避免多 MB body 造成高分配和长尾延迟。
- 超过约百万字符的超大段落会在达到滑窗上限后使用均匀采样；这会形成明确的召回上限。带有 `scan_truncated=true` 的事件不应据此宣称“全文未命中”，应交给异步完整审计/回放。
- 中间窗口先经过信号锚点字面量门控，再运行正则/紧凑变体匹配；在本机单次 5 MB 普通正文探针中，局部扫描由约 1.5 秒降至约 0.15 秒，仍保留高置信度窗口覆盖。
- 对引号、代码块、报告/文档/教育性提问做局部示例判定，并逐个检查重复出现，防止“先引用、后裸攻击”被绕过。
- 对直接“绕过安全限制/输出系统提示词”做高置信度单信号拦截；引用内容只有在外部没有“执行/遵循/使用”等动作时才进入示例抑制，且普通 `DAN` 缩写不会因单独出现而触发。
- 保留 NFKC、零宽字符/常见 homoglyph、leetspeak 和带前后缀的 Base64 识别。
- 同步覆盖 OpenAI/Responses、Anthropic、Gemini 的 tool/function schema 名称、描述和参数描述；enum/data 值不进入审计文本。直接 DAN/persona、拒答抑制、安全规则否定和多种系统提示词提取动词纳入高置信度规则，普通缩写和报告谓词保留说明语境。
- 角色只作为精度提示，不作为信任边界；客户端伪造 system/developer 仍会经过高置信度规则。

### 4.3 离线分析工具

文件：`/Users/th3ee9ine/Downloads/qqq2api/tools/analyze_interception_logs.py`

工具读取 XLSX/TXT、计算阶段/错误/指纹/请求体完整性/关键词命中和重试放大指标，不执行附件中的任何文本。它不写回原始日志；派生 JSON 摘要仅避免把 Bearer/API 凭据复制到新的输出文件。

网关现在把最近一次审计的 `decision`、`error_code`、`categories`、`risk_level`、`action`、`matched_scanners`、`scanner_scores`、`scanner_evidence`、角色/端点/策略版本写入 Ops 请求明细，且清理跨 WebSocket/缓存复用的旧值；证据字段在派生明细中限制为 64 项、单项 512 字节，原始请求体仍由原有 capture 策略管理。

## 5. 回归验证

已加入测试：

- `/Users/th3ee9ine/Downloads/qqq2api/backend/internal/service/content_moderation_keyword_matcher_test.go`
- `/Users/th3ee9ine/Downloads/qqq2api/backend/internal/securityaudit/prompt_injection_test.go`

针对本次 490 条关键词做的离线语义探针（每条词分别测试引用、培训检测、定义/防御和“请提供…脚本”四类句式）结果为：前三类 0/490 误拦，第四类 490/490 保持可拦；另有重叠词回归用例覆盖 `简历库/简历库出售`、`肉鸡/肉鸡出售`。

通过的命令：

```text
cd /Users/th3ee9ine/Downloads/qqq2api/backend && GOSUMDB=sum.golang.org GOTOOLCHAIN=auto go test ./internal/securityaudit -count=1
cd /Users/th3ee9ine/Downloads/qqq2api/backend && GOSUMDB=sum.golang.org GOTOOLCHAIN=auto go test ./internal/service -run 'TestContentModerationKeywordMatcher|TestContentModerationRuntimeSnapshot' -count=1
cd /Users/th3ee9ine/Downloads/qqq2api/backend && GOSUMDB=sum.golang.org GOTOOLCHAIN=auto go test ./internal/handler -count=1
cd /Users/th3ee9ine/Downloads/qqq2api/backend && GOSUMDB=sum.golang.org GOTOOLCHAIN=auto go test ./... -count=1
/Users/th3ee9ine/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3 -m py_compile /Users/th3ee9ine/Downloads/qqq2api/tools/analyze_interception_logs.py
/Users/th3ee9ine/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3 /Users/th3ee9ine/Downloads/qqq2api/tools/analyze_interception_logs.py /Users/th3ee9ine/Downloads/error_logs_2026-09-01_to_2026-09-02.xlsx /Users/th3ee9ine/Downloads/线上内容拦截关键词.txt
cd /Users/th3ee9ine/Downloads/qqq2api/backend && GOSUMDB=sum.golang.org GOTOOLCHAIN=auto go vet ./...
```

结果：Go 全量测试通过；离线摘要复现 68 行、10 个唯一指纹、8 条完整正文和唯一命中词 `群控`。把可见的完整 HVAC 请求体经 `ExtractContentModerationInput` 后回放到新 matcher，结果为 allow（10,160 个提取文本字符）。

## 6. 上线建议

### P0（立即）

1. 对 60 条正文缺失事件做异步回放；在证据不足时使用 `待复核`，不要自动标记为误封。
2. 检查并轮换日志明细中出现的 Bearer token，审计其访问范围、留存位置和导出权限；本报告不复制该 token 的值。
3. 将 `prompt_guard_blocked`、`content_policy_violation` 分成独立看板和告警；返回给内部审计的字段至少包括 `rule_id`、`category`、`confidence`、`evidence_span`、`role`、`policy_version`。
4. 检查预哈希/重复决策缓存：键至少包含 `tenant/client + normalized_hash + policy_version`，TTL 建议 5 分钟，并把 `retry_count` 与 `unique_decision_count` 分开统计。

### P1（灰度）

1. 先 shadow 观察 24 小时，再按 1% → 10% → 50% → 100% 放量；只有高置信度操作请求直接 hard block，表格/引用/否定/防御语境进入 allow 或人工复核。
2. 为关键词建立结构化字段：`term`、`category`、`severity`、`context_policy`、`owner`、`source`、`version`、`allow_examples`、`deny_examples`。
3. 每次规则发布固定 `policy_version`，保留命中前后的 normalized hash 和最小证据跨度，支持可重复回放。

### P2（持续评估）

建立按事件和按唯一指纹的 gold set，至少覆盖：表格/清单、引用/代码块、否定、检测/防御报告、真实操作请求、Unicode/Base64 混淆、重叠短词。持续看板指标：precision、recall、FPR、FNR、申诉翻转率、规则贡献、重复放大率、p95/p99 延迟、截断率和待复核积压。

## 7. 结论与限制

当前能被正文直接证实的误封是 HVAC 表单中的 `群控`（8 次重复事件、1 个唯一指纹）。48 条 prompt guard 和另外 12 条内容审核事件因正文被截断，先作为候选集合，错误消息本身不足以定性误封。新匹配器已在本地 enforcement 路径接入并通过全量 Go 测试；真实线上误封率应在完成回放、灰度和人工 gold set 标注后重新计算。
