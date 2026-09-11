package service

import (
	"sort"
	"strings"
)

// OpenAITestHeaderDetail documents policy, not additional wire headers. A
// conditional entry must not be inserted into a request just because it is
// listed here. Values stay in the separately redacted Headers map.
type OpenAITestHeaderDetail struct {
	Name            string `json:"name"`
	Purpose         string `json:"purpose"`
	Requirement     string `json:"requirement"`
	Condition       string `json:"condition"`
	Source          string `json:"source"`
	DefaultIncluded bool   `json:"default_included"`
}

func buildOpenAITestHeaderDetails(headers map[string]string) []OpenAITestHeaderDetail {
	details := []OpenAITestHeaderDetail{
		{Name: "Authorization", Requirement: "required", Purpose: "上游身份认证；Bearer Token 或 Agent Identity 断言。", Condition: "真实值由所选账号凭据生成；预览仅展示脱敏值。", Source: "账号凭据 / Agent Identity"},
		{Name: "Content-Type", Requirement: "required", Purpose: "声明请求体编码，当前测试使用 application/json。", Condition: "须与实际 body 一致；multipart 请求由编码器生成 boundary。", Source: "AccountTestService"},
		{Name: "Accept", Requirement: "recommended", Purpose: "声明期望的响应媒体类型，流式文本/Responses 测试使用 text/event-stream。", Condition: "配合 body.stream=true；普通 API Key 生图返回 JSON，不添加 SSE Accept。", Source: "AccountTestService"},
		{Name: "Accept-Language", Requirement: "conditional", Purpose: "传递客户端明确选择的语言偏好，不决定模型输出语言。", Condition: "正式 HTTP / WebSocket 网关支持客户端真实值；未配置时不生成默认语言。", Source: "原生客户端 / 正式网关"},
		{Name: "X-Client-Request-Id", Requirement: "recommended", Purpose: "关联一次上游请求，便于在超时或缺少响应 request-id 时排查。", Condition: "服务端每次生成独立 UUID；不是会话 ID，不用作幂等键。", Source: "AccountTestService / UUID"},
		{Name: "User-Agent", Requirement: "conditional", Purpose: "描述 Codex 客户端类型、版本和运行环境。", Condition: "Codex OAuth 和 Responses 探测使用项目身份配置；不是公共 API 的通用必填项。", Source: "Codex 身份配置"},
		{Name: "Originator", Requirement: "conditional", Purpose: "标识 Codex 调用端来源，与 User-Agent 配对。", Condition: "使用项目选定的客户端身份，不混用不同客户端的身份字段。", Source: "Codex 身份配置"},
		{Name: "Version", Requirement: "conditional", Purpose: "声明 Codex 客户端版本。", Condition: "与 User-Agent/Originator 保持一致；不等同于公共 API 的 OpenAI-Version。", Source: "Codex 身份配置"},
		{Name: "Chatgpt-Account-Id", Requirement: "conditional", Purpose: "指定 OAuth 凭据对应的 ChatGPT 账号或工作区。", Condition: "仅账号凭据中存在该 ID 时发送；影子账号使用母账号的身份。", Source: "OAuth 账号凭据"},
		{Name: "X-Openai-Fedramp", Requirement: "conditional", Purpose: "标明账号的 FedRAMP 路由属性。", Condition: "仅账号明确配置为 FedRAMP 时发送 true；不自行开启。", Source: "OAuth 账号属性"},
		{Name: "X-Codex-Beta-Features", Requirement: "conditional", Purpose: "声明会话已启用的 Codex 能力，例如 remote_compaction_v2。", Condition: "OAuth Codex 未显式声明时使用项目默认；已有非空能力声明保留；普通 API Key 测试不注入。", Source: "共享 Codex 能力规则"},
		{Name: "X-Codex-Routing-Hint", Requirement: "conditional", Purpose: "向 Codex 上游提示最终模型和有效服务档位。", Condition: "仅 OAuth Codex；从最终 body 派生 model=...，按需追加 tier；生图取 Responses 承载模型。", Source: "共享 Codex 路由规则"},
		{Name: "X-Codex-Window-Id", Requirement: "conditional", Purpose: "关联客户端窗口上下文，不是认证信息。", Condition: "当前 API Key Responses 合成探测逐请求生成；正式网关按客户端上下文和账号隔离规则处理。", Source: "Codex 探测 / 窗口上下文"},
		{Name: "Session_ID", Requirement: "conditional", Purpose: "关联会话及请求路由亲和性。", Condition: "有实际会话或 prompt_cache_key 时经账号隔离后生成；普通单次测试不伪造固定值。", Source: "正式网关会话上下文"},
		{Name: "Conversation_ID", Requirement: "conditional", Purpose: "关联对话上下文，与隔离后的会话标识保持一致。", Condition: "由真实对话/会话上下文派生，不用于代替 Responses body 的 conversation 参数。", Source: "正式网关会话上下文"},
		{Name: "X-Codex-Turn-State", Requirement: "conditional", Purpose: "回带上游产生的不透明回合状态，维持后续请求的回合连续性。", Condition: "仅同账号、同回合后续请求携带；不随机生成、不跨账号复用。", Source: "上游响应 / 回合状态缓存"},
		{Name: "X-Codex-Turn-Metadata", Requirement: "conditional", Purpose: "携带线程、回合、父子任务等结构化 JSON 元数据。", Condition: "仅有真实客户端上下文时发送，并与账号隔离后的相关 ID 一致。", Source: "原生客户端 / 正式网关"},
		{Name: "X-Codex-Installation-Id", Requirement: "conditional", Purpose: "关联客户端安装实例。", Condition: "来自客户端或明确启用的账号设备配置，不作为所有请求的默认头。", Source: "客户端 / 账号设备配置"},
		{Name: "Session-Id", Requirement: "conditional", Purpose: "客户端会话标识的连字符形式。", Condition: "仅原生上下文或已启用的账号会话配置使用；须与其他会话元数据一致。", Source: "客户端 / 账号会话配置"},
		{Name: "Thread-Id", Requirement: "conditional", Purpose: "关联当前任务线程。", Condition: "使用实际线程上下文并按账号隔离，不随每次重试随意重建。", Source: "客户端 / 正式网关"},
		{Name: "X-Codex-Parent-Thread-Id", Requirement: "conditional", Purpose: "关联子任务所属的父线程，维持任务派生关系。", Condition: "仅有真实父线程时发送；正式 HTTP / WebSocket 网关与 Thread-Id 使用相同账号隔离规则。", Source: "原生客户端子任务 / 正式网关"},
		{Name: "Turn-Id", Requirement: "conditional", Purpose: "标识当前线程中的一次回合。", Condition: "由真实回合上下文提供，与 Turn-Metadata 对齐。", Source: "客户端 / 正式网关"},
		{Name: "OpenAI-Organization", Requirement: "conditional", Purpose: "为 Platform API Key 请求指定组织归属。", Condition: "确需指定组织时在账号 HeaderOverrides 配置；不从 ChatGPT OAuth 的组织字段推导。", Source: "账号 HeaderOverrides"},
		{Name: "OpenAI-Project", Requirement: "conditional", Purpose: "为 Platform API Key 请求指定项目归属。", Condition: "确需指定项目时在账号 HeaderOverrides 配置，项目必须与 API Key 权限匹配。", Source: "账号 HeaderOverrides"},
		{Name: "OpenAI-Beta", Requirement: "conditional", Purpose: "在指定协议路径进行功能或版本协商。", Condition: "Responses WebSocket 版本由 WS 拨号器决定；普通 HTTP 不添加旧 responses=experimental。", Source: "WebSocket 协议配置"},
		{Name: "X-OpenAI-Internal-Codex-Residency", Requirement: "conditional", Purpose: "声明显式配置的数据驻留要求。", Condition: "当前项目仅透传单个规范 us 值；没有驻留配置时不发送。", Source: "原生客户端 / 正式网关"},
		{Name: "X-ResponsesAPI-Include-Timing-Metrics", Requirement: "conditional", Purpose: "请求原生 WebSocket 时序诊断指标。", Condition: "仅 WS 握手且明确请求 true；不加到普通 HTTP 测试。", Source: "WebSocket 握手"},
		{Name: "X-OpenAI-Subagent", Requirement: "conditional", Purpose: "说明原生子任务的类型。", Condition: "仅实际子任务上下文使用，不给普通对话伪造子任务身份。", Source: "原生客户端子任务"},
		{Name: "X-OpenAI-Memgen-Request", Requirement: "conditional", Purpose: "标记原生记忆整理请求。", Condition: "仅 true 且配套 X-OpenAI-Subagent: memory_consolidation 时使用。", Source: "原生记忆整理任务"},
		{Name: "X-OpenAI-Internal-Codex-Responses-Lite", Requirement: "conditional", Purpose: "标记项目支持的 Responses Lite 特定通路。", Condition: "仅对应 Lite/生图意图通路使用，不是所有生图请求的必需头。", Source: "特定 Responses Lite 路由"},
		{Name: "Host", Requirement: "transport", Purpose: "指明目标主机；HTTP/2 对应 :authority。", Condition: "由 URL 或 http.Request.Host 决定，不通过普通 HeaderOverrides 修改。", Source: "HTTP 请求构造"},
		{Name: "Content-Length", Requirement: "transport", Purpose: "描述请求体字节长度。", Condition: "由请求体及传输方式决定；分块传输/HTTP2 下不保证单独出现。", Source: "HTTP 传输层"},
		{Name: "Accept-Encoding", Requirement: "transport", Purpose: "协商响应内容压缩。", Condition: "由项目 HTTP 客户端和代理配置管理；不手工声明未支持的解压格式。", Source: "HTTP 传输层"},
		{Name: "Connection", Requirement: "transport", Purpose: "HTTP/1.x 逐跳连接控制。", Condition: "交给传输层管理；HTTP/2 不发送 Connection: keep-alive。", Source: "HTTP 传输层"},
		{Name: "Sec-WebSocket-Key", Requirement: "transport", Purpose: "WebSocket 升级握手的随机校验值。", Condition: "仅 WebSocket 拨号器生成，普通 HTTP 测试不发送。", Source: "WebSocket 拨号器"},
	}
	included := make(map[string]bool, len(headers))
	for name := range headers {
		included[strings.ToLower(name)] = true
	}
	known := make(map[string]bool, len(details))
	for i := range details {
		name := strings.ToLower(details[i].Name)
		details[i].DefaultIncluded = included[name]
		known[name] = true
	}
	var custom []string
	for name := range headers {
		if !known[strings.ToLower(name)] {
			custom = append(custom, name)
		}
	}
	sort.Strings(custom)
	for _, name := range custom {
		details = append(details, OpenAITestHeaderDetail{Name: name, Requirement: "conditional", Purpose: "账号配置的自定义请求头，用途由目标上游定义。", Condition: "仅启用且通过项目过滤的账号 HeaderOverrides 生效；值已脱敏。", Source: "账号 HeaderOverrides", DefaultIncluded: true})
	}
	return details
}
