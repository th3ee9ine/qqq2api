package service

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/th3ee9ine/qqq2api/internal/pkg/openai"
)

// codexUpstreamMinVersion 上游 /backend-api/codex 接受的最低 version 头：
// 若请求携带 version 且低于该值，上游直接 404（issue #3901，2026-07 实测）。
const codexUpstreamMinVersion = "0.144.0"

// codexClientVersionMaxLen 官方版本号均为短 ASCII 串，远低于此上限。
const codexClientVersionMaxLen = 64

// codexClientVersionPattern 允许 0.146.0 与 0.147.0-alpha.4 两类官方形态。
var codexClientVersionPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]+){1,3}(-[0-9A-Za-z.]+)?$`)

// codexStableClientVersionPattern 对应官方 rust-v 的稳定 tag 形态。
// CompareVersions 只比较 major/minor/patch，因此同步值必须固定为三段，避免
// 第四段被比较器忽略后产生错误的相等判断。
var codexStableClientVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// NormalizeCodexClientVersion 校验并归一化 Codex 客户端版本号，非法值返回空串。
// 该值会被拼进出站 User-Agent 首段与 Responses/WS Version 头，必须拒绝
// 任意字节，避免管理员误填或自动同步拿到异常值时把不可控内容透给上游。
func NormalizeCodexClientVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || len(version) > codexClientVersionMaxLen || !codexClientVersionPattern.MatchString(version) {
		return ""
	}
	return version
}

// NormalizeStableCodexClientVersion validates a stable official Codex release
// version in the exact X.Y.Z form. It is exported for API-boundary validation;
// runtime code uses the same implementation so accepted settings cannot later
// be silently discarded as prereleases.
func NormalizeStableCodexClientVersion(version string) string {
	return normalizeStableCodexClientVersion(version)
}

// normalizeStableCodexClientVersion 只接受不带预发布后缀的正式版。
// 自动同步、缓存和 Responses/WS Version 必须共用这一规则；否则数据库中的
// 0.200.1-alpha.1 会被版本比较器视作与 0.200.1 相等，阻塞同 core 的正式版写入。
func normalizeStableCodexClientVersion(version string) string {
	version = NormalizeCodexClientVersion(version)
	if version == "" || !codexStableClientVersionPattern.MatchString(version) {
		return ""
	}
	for _, part := range strings.Split(version, ".") {
		if _, err := strconv.Atoi(part); err != nil {
			return ""
		}
	}
	return version
}

// buildCodexCLIUserAgent 按官方稳定版拼出规范 Codex Desktop User-Agent。
// 函数名为历史内部名称；首段 engine 与 Responses/WS Version 同源，
// Desktop 宿主 build 则固定在独立 trailer 中。
func buildCodexCLIUserAgent(version string) string {
	if version = NormalizeCodexClientVersion(version); version == "" {
		return codexCLIUserAgent
	}
	return openai.CodexDefaultOriginator + "/" + version + codexDesktopUserAgentSuffix
}

// codexIdentityEnforcement 控制 enforceCodexIdentityHeaders 是否强制统一出站身份，
// 由 gateway.disable_codex_identity_enforcement 在服务构造时取反发布。
// 默认开启：上游在容量紧张时按客户端身份分优先级降载，被降载的请求会拿到
// HTTP 200 + 流内 server_is_overloaded，本次请求即失败；强制统一出口可确保没有
// 请求带着第三方或陈旧身份出站。关闭后退回「仅按最终 UA 配对 originator」的收口语义。
var codexIdentityEnforcement = func() *atomic.Bool {
	v := &atomic.Bool{}
	v.Store(true)
	return v
}()

// SetCodexIdentityEnforcementEnabled 发布 Codex 出站身份强制统一开关。
// enforceCodexIdentityHeaders 是所有出站路径共用的纯函数收口点，无法在热路径注入配置，
// 故由持有配置的服务在构造时发布进程级快照。
func SetCodexIdentityEnforcementEnabled(enabled bool) {
	codexIdentityEnforcement.Store(enabled)
}

// codexCanonicalUserAgentResolver 返回当前生效的规范 Codex User-Agent（后台设置 / 自动同步版本号）。
// 由 SettingService 在装配时注入；解析器内部自带 TTL 缓存，热路径不触库。
type codexCanonicalUserAgentResolver func() string

// codexCanonicalResponsesVersionResolver 返回当前应声明的官方稳定版。
// UA engine 与 Responses/WS Version 都消费该值，保证身份版本不会漂移。
type codexCanonicalResponsesVersionResolver func() string

var (
	codexCanonicalUAMu               sync.RWMutex
	codexCanonicalUAResolver         codexCanonicalUserAgentResolver
	codexCanonicalResponsesVersionMu sync.RWMutex
	codexCanonicalResponsesVersion   codexCanonicalResponsesVersionResolver
)

// SetCodexCanonicalUserAgentResolver 注入规范 User-Agent 解析器。
// 未注入或解析结果非法时回退到编译期常量 codexCLIUserAgent。
func SetCodexCanonicalUserAgentResolver(resolver func() string) {
	codexCanonicalUAMu.Lock()
	defer codexCanonicalUAMu.Unlock()
	codexCanonicalUAResolver = resolver
}

// SetCodexCanonicalResponsesVersionResolver 注入 Responses/WS Version 解析器。
// 未注入、返回非法值或返回值低于内置新版下限时，使用
// codexResponsesVersionFallback。
func SetCodexCanonicalResponsesVersionResolver(resolver func() string) {
	codexCanonicalResponsesVersionMu.Lock()
	defer codexCanonicalResponsesVersionMu.Unlock()
	codexCanonicalResponsesVersion = resolver
}

// CodexCanonicalUserAgent 返回当前生效的规范 Codex User-Agent。
// 取值走与推理相同的解析链：面板 UA 指纹 + 官方同步版本号 + 编译期兜底。
// 供无账号句柄的出站路径（OAuth 换 Token / 刷新）使用。
func CodexCanonicalUserAgent() string {
	userAgent, _ := resolveCodexOutboundUserAgentIdentity("")
	return userAgent
}

// CodexCanonicalAuthIdentity 返回凭据面（auth.openai.com：换 Token / 刷新 / whoami）
// 出站请求的身份对：规范 User-Agent 与配套 originator，与推理解析链同源。
// 凭据面不发 version 头——真实 Codex 客户端在该面只携带 originator 与 User-Agent
// （codex-rs login/default_client.rs 的 default_headers()），version 门槛
// （issue #3901）只存在于 /backend-api/codex 推理面。
func CodexCanonicalAuthIdentity() (userAgent, originator string) {
	return resolveCodexOutboundUserAgentIdentity("")
}

// ApplyCodexCanonicalAuthIdentity 为凭据面出站请求写入身份对（不含 version）。
func ApplyCodexCanonicalAuthIdentity(h http.Header) {
	if h == nil {
		return
	}
	userAgent, originator := CodexCanonicalAuthIdentity()
	h.Set("user-agent", userAgent)
	h.Set("originator", originator)
	// 凭据面的官方 default_headers() 不包含 Version；即使调用方复用了
	// Responses 的 Header 容器，也不应把该字段泄漏到 auth.openai.com。
	h.Del("version")
}

// CodexCanonicalClientVersion 返回 Responses/WS 使用的当前 Codex 版本。
func CodexCanonicalClientVersion() string {
	return currentCodexResponsesVersion()
}

// codexCanonicalUserAgent 返回出站规范 User-Agent。
func codexCanonicalUserAgent() string {
	codexCanonicalUAMu.RLock()
	resolver := codexCanonicalUAResolver
	codexCanonicalUAMu.RUnlock()
	if resolver != nil {
		if ua := strings.TrimSpace(resolver()); ua != "" {
			return ua
		}
	}
	return codexCLIUserAgent
}

// currentCodexResponsesVersion 返回当前 Responses/WS Version。自动同步值
// 只能向前推进，不得把进程内置的已知新版降级。
func currentCodexResponsesVersion() string {
	codexCanonicalResponsesVersionMu.RLock()
	resolver := codexCanonicalResponsesVersion
	codexCanonicalResponsesVersionMu.RUnlock()
	if resolver != nil {
		if version := normalizeStableCodexClientVersion(resolver()); version != "" &&
			CompareVersions(version, codexResponsesVersionFallback) >= 0 {
			return version
		}
	}
	return codexResponsesVersionFallback
}

// codexOutboundIdentity 是出站身份三元组：originator 与 User-Agent 首段必须配套，
// User-Agent engine 与 Version 使用同一官方最新稳定 rust-v。
type codexOutboundIdentity struct {
	userAgent  string
	originator string
	version    string
}

// resolveCodexOutboundUserAgentIdentity 由候选 User-Agent 推导自洽的
// User-Agent/originator 身份对。凭据面虽然不发送 Version 头，但 User-Agent
// 的 engine 版本仍与 Responses/WS 使用同一官方稳定版。
// candidateUA 为空时使用规范 User-Agent；推导不出官方身份时整体回退为规范 Desktop 身份。
//
// 候选 UA（面板 / 账号级的管理员显式配置）只贡献客户端名与 OS / 架构 / 终端指纹，
// 其自带的版本段一律用当前生效版本重建：一条填写于某个历史版本的 UA 否则会把出站身份
// 永久钉死在陈旧版本上，绕过版本自动同步，落回上游优先降载的那一侧。
// 需要固定 UA engine 版本请填「Codex 客户端版本号」。
func resolveCodexOutboundUserAgentIdentity(candidateUA string) (userAgent, originator string) {
	return resolveCodexOutboundUserAgentIdentityWithVersion(candidateUA, currentCodexResponsesVersion())
}

// resolveCodexOutboundUserAgentIdentityWithVersion 使用调用方已解析的统一版本重建 UA，
// 让一次请求中的 UA engine 与 Version 即使恰逢同步切换也保持原子一致。
func resolveCodexOutboundUserAgentIdentityWithVersion(candidateUA, version string) (userAgent, originator string) {
	canonical := codexCanonicalUserAgent()
	ua := strings.TrimSpace(candidateUA)
	if ua == "" {
		ua = canonical
	}
	originator, pairedUA, ok := openai.PairCodexClientIdentity(ua)
	if !ok {
		if originator, pairedUA, ok = openai.PairCodexClientIdentity(canonical); !ok {
			originator, pairedUA = openai.CodexDefaultOriginator, codexCLIUserAgent
		}
	}
	// UA engine 与 Responses/WS Version 使用同一解析器；Originator 由最终 UA
	// 的客户端名配对，Desktop trailer 的 app build 保持独立。
	if rebuilt := openai.SetCodexUserAgentVersion(pairedUA, version); rebuilt != "" {
		pairedUA = rebuilt
	}
	return pairedUA, originator
}

// resolveCodexOutboundIdentity 组合 User-Agent/originator 与同源官方稳定版 Version，
// 供推理面统一收口。
func resolveCodexOutboundIdentity(candidateUA string) codexOutboundIdentity {
	version := currentCodexResponsesVersion()
	userAgent, originator := resolveCodexOutboundUserAgentIdentityWithVersion(candidateUA, version)
	return codexOutboundIdentity{
		userAgent:  userAgent,
		originator: originator,
		version:    version,
	}
}

// ensureCodexIdentityHeaders 补齐 OAuth（ChatGPT 内部接口）出站请求所需的 Codex 身份头。
// 已有 User-Agent 与 version 保持不变，交给紧随其后的 enforceCodexIdentityHeaders 收口。
// 本函数只管理客户端身份，不注入或改写上游能力协商头。
func ensureCodexIdentityHeaders(h http.Header) {
	if h == nil {
		return
	}
	identity := resolveCodexOutboundIdentity("")
	if strings.TrimSpace(h.Get("user-agent")) == "" {
		h.Set("user-agent", identity.userAgent)
	}
	if strings.TrimSpace(h.Get("originator")) == "" {
		h.Set("originator", identity.originator)
	}
	if strings.TrimSpace(h.Get("version")) == "" {
		h.Set("version", identity.version)
	}
}

// applyOpenAICodexProbeHeaders 为合成探测请求补齐 Codex 身份和引擎指纹。
func applyOpenAICodexProbeHeaders(h http.Header) {
	if h == nil {
		return
	}
	version := currentCodexResponsesVersion()
	userAgent, originator := resolveCodexOutboundUserAgentIdentityWithVersion(h.Get("user-agent"), version)
	h.Set("user-agent", userAgent)
	h.Set("originator", originator)
	h.Set("version", version)
	h.Set("X-Codex-Window-ID", uuid.NewString())
}

// enforceCodexIdentityHeaders 收口 OAuth（ChatGPT 内部接口）出站请求的客户端身份头。
// 见 enforceCodexIdentityHeadersWithUA；无账号级自定义 User-Agent 时使用本函数。
func enforceCodexIdentityHeaders(h http.Header) {
	enforceCodexIdentityHeadersWithUA(h, "")
}

// enforceCodexIdentityHeadersWithUA 强制统一 OAuth 出站身份：User-Agent / originator / version
// 一律改写为网关的规范身份，客户端自报身份不参与构造。上游在容量紧张时按客户端身份分优先级
// 降载，被降载的请求会拿到 HTTP 200 + 流内 server_is_overloaded；统一出口可确保没有请求带着
// 第三方或陈旧身份出站，也天然满足 originator 与 UA 首段配套的上游校验（issue #3901）。
//
// overrideUA 是账号级自定义 User-Agent：管理员的显式配置仍然生效，但只贡献客户端名与
// OS / 架构 / 终端指纹——版本段与 originator 都由规范身份重建，不允许出现自相矛盾或陈旧的身份。
//
// 强制统一被 gateway.disable_codex_identity_enforcement 关闭时，退回「按最终 User-Agent
// 配对 originator」的语义；Responses Version 仍收口到当前官方稳定版。
//
// 仅对携带 originator 的请求生效：compat 桥接等非 ChatGPT 内部接口路径会显式删除 originator，
// 不应被补回。需要从缺失身份头恢复的调用方应先调用 ensureCodexIdentityHeaders。
// 必须在所有 User-Agent 改写之后调用。
func enforceCodexIdentityHeadersWithUA(h http.Header, overrideUA string) {
	if h == nil || h.Get("originator") == "" {
		return
	}
	if !codexIdentityEnforcement.Load() {
		pairCodexIdentityHeaders(h)
		return
	}
	identity := resolveCodexOutboundIdentity(overrideUA)
	h.Set("user-agent", identity.userAgent)
	h.Set("originator", identity.originator)
	h.Set("version", identity.version)
}

// pairCodexIdentityHeaders 是关闭强制统一后的兜底收口：保留客户端真实 UA，
// 配对 originator，并将 Responses/WS Version 同步到当前官方稳定版。
func pairCodexIdentityHeaders(h http.Header) {
	version := currentCodexResponsesVersion()
	originator, pairedUA, ok := openai.PairCodexClientIdentity(h.Get("user-agent"))
	if !ok {
		pairedUA, originator = resolveCodexOutboundUserAgentIdentityWithVersion("", version)
	} else if rebuilt := openai.SetCodexUserAgentVersion(pairedUA, version); rebuilt != "" {
		pairedUA = rebuilt
	}
	h.Set("user-agent", pairedUA)
	h.Set("originator", originator)
	h.Set("version", version)
}
