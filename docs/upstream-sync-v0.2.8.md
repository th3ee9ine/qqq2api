# sub2api v0.2.8 选择性同步与平台退役记录

日期：2026-09-25
定制分支：`codex/sync-sub2api-v0.2.8-20260924`
回滚分支：`backup-before-v0.2.8-sync-20260924`

## 同步原则

本次以 [Wei-Shaw/sub2api releases](https://github.com/Wei-Shaw/sub2api/releases) 的正式 `v0.2.8` 为核对对象，没有把高度分叉的定制分支直接快进到上游，也没有使用 `--theirs`、批量覆盖或改写既有迁移。官方 tag 提交为 `fd80b08c90b55edcad5b00171b53f08721d30da1`，官方当时 `main` 为 `a3eb7ef302961cba716dc78b39b93b60c467db0e`；定制基线为 `c54d28b3b18143c8608b8ea7dd0bc3421752fce2`。本地保留了同步前回滚分支。

逐项审查后保留了与定制版兼容的上游修复：Antigravity schema cleaner、图片读取、代理过期与失效回退、支付 callback 尾斜杠、科学计数法价格、Responses oversized input ID、临时不可调度状态作用域、输入法组合期间的模型标签，以及内容审核元数据、reasoning effort 倍率和 affiliate ledger 的前向迁移 `242`、`243`、`244`。已有迁移文件没有被覆盖。

## 本次平台退役

随后按需求停止支持以下平台/集成：

- OpenCode Go
- Ollama Cloud 的专属用量、会话和自动刷新集成
- `kimi`
- `zhipu`
- `minimax`

移除内容包括 OpenCode/Ollama 专属用量服务、parser、probe/reset runner、仓储、管理 API、Wire 注入、生命周期任务、账号 enrich、前端设置/用量组件/API/类型/图标/翻译，以及 OpenCode 自动请求头、Kimi/Zhipu developer-role 改写和 GLM/MiniMax thinking/effort 改写。CN provider 管理入口也已移除。

平台策略现在只允许 `anthropic`、`openai`、`grok` 作为活跃账号；活跃分组另外允许 `composite`。新建、编辑、导入、复制、调度、网关、模型同步、pricing、monitor 和 DTO 校验都使用这一 allowlist。被退役的平台会被拒绝请求，不会再被视为 OpenAI-compatible。

保留了 Qwen thinking passback 与 effort fallback，因为它不属于本次退役范围。数据库中的旧账号、分组及平台值仍可读取和审计；前向迁移 `245_retire_removed_provider_platforms.sql` 会将未删除的 `opencode_go`、`kimi`、`zhipu`、`minimax` 账号设为不可调度、将 active 状态改为 disabled，并补齐空错误信息。迁移不执行 DELETE，也不修改历史迁移文件。Ollama 的通用 OpenAI/Anthropic base URL 配置仍可作为普通上游配置读取；移除的是专属 Ollama Cloud 集成。

历史 `accounts.extra` 中的旧用量字段会在仓储映射、批量更新、代理变更和 DTO 响应边界被过滤，以避免旧缓存重新暴露已退役集成的 session/snapshot。字段名仅作为兼容清理键保留。

## 验证

后端使用缓存的 Go 1.27 工具链执行；最新删除改动完成后应执行：

```bash
cd backend
GOSUMDB=sum.golang.org GOTOOLCHAIN=auto GOPROXY=https://goproxy.cn,direct \
  /Users/th3ee9ine/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.0.darwin-arm64/bin/go test ./...
```

本次变更已完成后端全包编译、Repository/middleware 测试和 thinking/gateway 定向测试；前端 `pnpm run typecheck`、`pnpm run lint`、`pnpm run build` 通过，针对账号编辑、模型白名单、请求头、分组 provider 和设置页的 94 个定向测试通过。前端全量测试保留两个定制版既有失败：`GroupsView.compositePlatforms.spec.ts` 的旧平台断言未包含 `grok`，以及 `UsageTable.spec.ts` 的 UA fixture 与旧文案断言不一致；没有修改这些基线差异来掩盖结果。

本次只保存本地代码和迁移改动，未推送 Git，未发布 Docker 镜像。发布前需要另行检查迁移 checksum、运行时版本和镜像 manifest。
