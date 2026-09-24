# sub2api v0.2.8 选择性同步记录

日期：2026-09-25
定制分支：`codex/sync-sub2api-v0.2.8-20260924`
回滚分支：`backup-before-v0.2.8-sync-20260924`

## 来源与分叉证据

同步来源为 [Wei-Shaw/sub2api releases](https://github.com/Wei-Shaw/sub2api/releases)，本次核对的正式版为 `v0.2.8`。本地保存了官方 tag 提交 `fd80b08c90b55edcad5b00171b53f08721d30da1`，并单独保存了官方最新 `main` 提交 `a3eb7ef302961cba716dc78b39b93b60c467db0e`，避免把开发分支变化误当作正式版发布内容。

定制版基线为 `c54d28b3b18143c8608b8ea7dd0bc3421752fce2`（QQQ2API 3.5.0），双方共同基线为 `1a9d49e16f7a22c432b428fce4af8d731f1fa364`。当前定制分支与 `v0.2.8` 之间存在大量双向独有提交（本地相对 tag：211 个、官方相对本地：236 个），不满足快进条件。

曾在隔离状态实际试合并官方 tag；冲突覆盖后端、前端、生成代码和迁移文件，随即 `git merge --abort`。因此本次同步没有使用整分支合并、`--theirs`、批量覆盖，也没有改写 `main`。

## 已同步内容

### 低冲突修复

以下上游修复按路径交集逐项审查后，以独立提交保留，未触及 QQQ2API 的核心定制行为：

- `a207d16c2`、`5e9c43b6c`：Antigravity Schema Cleaner 的 `prefixItems`、元组数组回退和审查修复。
- `891bec310`：取消被新读取替代的图片上传读取。
- `a6db5be0d`、`64b81e545`：代理过期标记和失效回退目标处理。
- `857beb230`：支付回调基础 URL 尾斜杠处理。
- `48a0c5aa7`：科学计数法价格边界解析。
- `630fdeadf`：清理过长的 Responses 输入 item ID。
- `0af69cf8d`：临时不可调度状态限定到当前账号。
- `4b06833f5`：输入法组合期间保留模型标签。

### OpenCode Go 官方用量窗口

这是 v0.2.8 中与当前定制版最匹配、且可以隔离接入的完整功能，采用受管字段和 CAS 身份保护，保留了 QQQ2API 的账号管理员作用域与自定义账号模型：

- 后端新增 OpenCode Go 用量服务、仓储、管理员 Handler、Wire 注入和生命周期清理。
- 支持官方 `rolling`、`weekly`、`monthly` 用量窗口，按同一 API Key 共享快照和自动刷新开关。
- 支持账号级/全局自动刷新，刷新由模型请求活动触发，具有 debounce、最大等待、失败退避和手动刷新限流。
- 账号凭证、平台、账号模式、代理变化会清理对应受管状态；普通账号编辑不能伪造或覆盖快照。
- `/zen/go` 与 `/zen/go/v1` 统一请求 `/zen/go/v1/usage`。
- 挂载白名单按当前定制版已有平台收敛为 `openai`、`anthropic`、`kimi`、`zhipu`、`minimax`；没有引入定制版不存在的 `PlatformDeepseek`。
- 管理端账号用量单元格、账号编辑弹窗、全局设置页和中英文文案均已接入。

### 数据库迁移

本地已经存在并可能被部署记录的 `238`、`239`、`240`、`241` 迁移保持原样。上游 v0.2.8 的三个独立数据库功能改为新的前向迁移编号，避免复用本地编号或改写历史 checksum：

- `242_content_moderation_engine_meta.sql`：增加内容审核引擎元数据列，保持旧数据和旧写入可空。
- `243_channel_reasoning_effort_multipliers.sql`：增加 reasoning effort 倍率并只迁移明确配置的旧值，保留显式清空语义。
- `244_affiliate_ledger_operation_id.sql`：为线下提现登记增加幂等 operation id 及唯一索引。

三个迁移均使用幂等 SQL，未修改已有迁移文件；静态迁移测试覆盖关键保护条件。

## 明确未整合的功能

TypeSafe 内容审核引擎配置档、更多计费/推荐/Codex 管理页面、Claude Code 版本自动同步、滚动日志策略、简易模式消费窗口和月度备份归档等较大功能暂未直接带入。它们涉及定制版已有安全审核、权限、账单、备份或运维模型，当前版本先保留为后续逐项评估对象，避免通过上游页面或 SQL 直接改变现有业务语义。

## 验证

后端完整测试通过：

```text
GOSUMDB=sum.golang.org GOTOOLCHAIN=auto GOPROXY=https://goproxy.cn,direct \
  go test ./...
```

前端针对本次功能的类型检查、OpenCode Go API/组件测试和 i18n 完整性测试通过；生产构建通过：

```text
pnpm run typecheck
pnpm exec vitest run \
  src/api/__tests__/admin.accounts.opencodeGoUsage.spec.ts \
  src/components/account/__tests__/OpenCodeGoUsageCell.spec.ts \
  src/i18n/__tests__/localeKeyCompleteness.spec.ts
pnpm run build
```

完整前端测试还保留两个与本次改动路径无关的定制版基线失败：`GroupsView.compositePlatforms.spec.ts` 的 retained platform 断言包含 `grok`，以及 `UsageTable.spec.ts` 的 User-Agent `Not sent` 文案断言。未修改这些既有功能来掩盖基线差异。

本次仅完成本地分支和提交准备，没有推送 Git，也没有发布 Docker 镜像；Git 远程和镜像发布需要另行执行并独立验证。
