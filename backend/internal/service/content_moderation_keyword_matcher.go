package service

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// contentModerationKeywordContextWindow bounds the amount of text inspected
// around a short/ambiguous keyword.  The matcher is used on normalized prompt
// text, but keeping the window bounded prevents a very large request from
// turning the contextual fallback into an O(n) allocation.
const contentModerationKeywordContextWindow = 96

// A substring match is intentionally retained for backwards compatibility
// (see Match and matchBlockedKeyword).  These terms are too generic to be a
// standalone hard block, however: they occur in ordinary documentation,
// forms, and defensive/security discussions.  MatchContextAware requires a
// nearby risk/action signal for them and suppresses explicit negation and
// table/description mentions.
var contextSensitiveContentModerationKeywords = map[string]struct{}{
	"木马":      {},
	"远控":      {},
	"免杀":      {},
	"肉鸡":      {},
	"传马":      {},
	"拿站":      {},
	"脱裤":      {},
	"拖库":      {},
	"提权":      {},
	"脏牛":      {},
	"撞库":      {},
	"盗号":      {},
	"打瘫":      {},
	"打宕":      {},
	"删库":      {},
	"查档":      {},
	"开盒":      {},
	"料子":      {},
	"料商":      {},
	"猫池":      {},
	"卡商":      {},
	"黑卡":      {},
	"群控":      {},
	"云控":      {},
	"养号":      {},
	"简历库":     {},
	"社工库":     {},
	"查户籍":     {},
	"出数据":     {},
	"收数据":     {},
	"军火商":     {},
	"算号器":     {},
	"买漏洞":     {},
	"白加黑":     {},
	"大灰狼":     {},
	"内存马":     {},
	"跳板机":     {},
	"洗ip":     {},
	"拿权限":     {},
	"提权到":     {},
	"反检测":     {},
	"过杀软":     {},
	"过火绒":     {},
	"过卡巴":     {},
	"过edr":    {},
	"c2":      {},
	"rce":     {},
	"unhook":  {},
	"stealer": {},
	"botnet":  {},
}

// Strong action/context terms are deliberately language-neutral where
// possible.  They are only used for short/ambiguous terms; a long configured
// phrase keeps the existing hard-block behaviour.
var contentModerationRiskContextTerms = []string{
	"脚本", "命令", "代码", "payload", "shell", "webshell", "攻击", "入侵", "渗透",
	"窃取", "盗取", "抓取", "转储", "导出", "删除", "擦除", "加密", "勒索", "爆破",
	"破解", "绕过", "漏洞", "利用", "注入", "提权", "执行", "搭建", "部署", "安装",
	"运行", "下载", "编写", "生成", "提供", "创建", "制作", "上线", "远程", "连接",
	"监听", "控制", "批量", "自动注册", "注册机",
	"出售", "购买", "收购", "拿到", "拿下", "拖库", "脱库", "发信", "钓鱼", "劫持",
	"投毒", "转发", "扫描", "密码", "账号", "验证码", "权限", "root", "admin", "administrator", "服务器", "主机", "端口", "c2",
	"rce", "uac", "amsi", "edr", "waf", "mimikatz", "webshell", "leak", "steal", "exfiltrate",
	"exploit", "bypass", "command", "script", "code", "download", "install", "execute", "run",
	"build", "create", "generate", "provide", "write", "make", "hack", "attack", "intrusion",
	"credential", "password", "cookie", "token", "server", "host", "port", "root", "system", "administrator", "admin",
	"权限", "目标", "target",
}

var contentModerationNegationTerms = []string{
	"禁止", "严禁", "不得", "不要", "请勿", "勿", "不允许", "不应", "不能", "不可",
	"没有", "无", "未", "避免", "防止", "阻止", "拒绝", "禁用", "禁上", "不执行", "不使用",
	"不支持", "不含", "不包含", "不需要", "不启用", "不采用", "不用", "不做", "不是", "并非",
	"do not", "don't", "never", "without", "not allowed", "no", "prohibited", "forbidden", "disabled",
	// Keep standalone English negation tokenized so phrases such as
	// `not a SQL injection request` are treated like their Chinese `不是`/
	// `并非` counterparts. Boundary checks in keywordTermHasSuffix/Prefix keep
	// this from matching the suffix of words such as `notable`.
	"not", "isn't", "isnt", "is not",
}

var contentModerationActionTerms = []string{
	"执行", "使用", "启用", "进行", "开展", "调用", "提供", "实施", "搭建", "部署", "运行",
	"开启", "安装", "编写", "生成", "下载", "连接", "控制", "操作", "购买", "出售", "创建", "制作",
	"execute", "use", "enable", "invoke", "provide", "build", "deploy", "run", "install", "write", "generate", "download", "connect", "create", "make", "call",
}

// These actions create or operate a sensitive artifact.  They are kept
// separate from ordinary explanatory verbs so that a phrase such as
// “请编写木马检测脚本” can be treated as defensive, while “下载并运行远控”
// remains an actionable hit even when the user adds a generic “安全” qualifier.
var contentModerationOperationalActionTerms = []string{
	"执行", "下载", "安装", "运行", "连接", "监听", "部署", "搭建", "构建", "开启", "启用",
	"调用", "购买", "出售", "收购", "上线", "execute", "download", "install", "run",
	"connect", "deploy", "build", "enable", "invoke", "purchase", "sell",
}

// Defensive/explanatory markers are intentionally more specific than the
// generic “安全” label.  They can suppress a keyword only when no
// operational/offensive intent is present (with a narrow detection-script
// exception handled below).
var contentModerationDefensiveTerms = []string{
	"防御", "防护", "检测", "侦测", "识别", "审计", "修复", "缓解", "加固", "监控",
	"告警", "排查", "应急", "处置", "治理", "培训", "演练", "模拟", "验证", "评估",
	"解释", "说明", "定义", "原理", "什么是", "含义", "报告", "文档", "规则", "风险",
	"研究", "分析", "审查", "测试", "教学", "教育", "示例", "样例", "如何避免", "如何防止",
	"prevent", "protect", "detect", "detection", "identify", "audit", "remediate", "mitigate",
	"harden", "monitor", "alert", "training", "exercise", "simulate", "verify", "assessment",
	"explain", "what is", "documentation", "report", "rule", "risk", "research", "analysis",
	"review", "test", "testing", "educational", "example", "how to detect", "how to defend",
}

var contentModerationDescriptionTerms = []string{
	"例如", "比如", "示例", "样例", "说明", "定义", "术语", "关键词", "字段", "选项", "表格",
	"清单", "参数", "配置项", "是否", "日志", "报告", "文档", "解释", "什么是", "含义", "讨论",
	"审计", "测试", "防御", "识别", "检测", "修复", "风险", "规则", "仅作", "仅供", "原理", "是什么", "如何避免", "如何防止",
	"for example", "example", "explain", "explanation", "what is", "how to detect", "how to defend", "defensive", "security", "audit", "test", "testing", "documentation", "document", "prevent", "mitigate", "remediate", "review", "analysis", "analyze", "research", "educational", "simulation", "sandbox",
}

type contentModerationKeywordMatcher struct {
	nodes           []contentModerationKeywordNode
	edges           []contentModerationKeywordEdge
	rootTransitions [256]int32
	keywords        []string
	// outputs contains every configured keyword that terminates at each AC
	// state, including patterns inherited through failure links.  Match only
	// needs bestKeyword, while the context-aware path uses this index to inspect
	// actual candidates in one text pass instead of running strings.Index for
	// every configured keyword.
	outputs            [][]int32
	normalizedKeywords []string
	// covering maps a normalized candidate to the longer configured phrases
	// that contain it.  It is built once so contextual matching does not scan
	// the entire 490-entry list for every occurrence in a long form.
	covering map[string][]string
	// canonicalVariant is true when at least one configured keyword changes
	// under the contextual path's NFKC/format-character normalization.  A raw
	// AC miss can be returned immediately only when this is false; otherwise a
	// full-width/zero-width spelling may still match after normalization.
	canonicalVariant bool
}

type contentModerationKeywordNode struct {
	failure     int32
	bestKeyword int32
	edgeStart   uint32
	edgeCount   uint16
}

type contentModerationKeywordEdge struct {
	target int32
	label  byte
}

type contentModerationKeywordBuildEdge struct {
	target      int32
	nextSibling int32
	label       byte
}

func newContentModerationKeywordMatcher(keywords []string) *contentModerationKeywordMatcher {
	if len(keywords) == 0 {
		return nil
	}

	buildNodes := []contentModerationKeywordNode{newContentModerationKeywordNode()}
	buildEdges := make([]contentModerationKeywordBuildEdge, 0)
	terminalOutputs := [][]int32{{}}
	originalKeywords := append([]string(nil), keywords...)
	canonicalVariant := false
	for _, keyword := range keywords {
		trimmed := strings.TrimSpace(keyword)
		if trimmed == "" {
			continue
		}
		// Uppercase ASCII is already handled by the raw automaton's lowercased
		// labels. Only mark a configuration as a canonical variant when trimming
		// or NFKC/format-character normalization changes bytes beyond that normal
		// case-folding step; otherwise the production 490-term list would
		// unnecessarily fall back to the slower per-keyword search.
		if trimmed != keyword || normalizeContentModerationKeywordText(trimmed) != strings.ToLower(trimmed) {
			canonicalVariant = true
			break
		}
	}

	for keywordIndex, keyword := range keywords {
		if keyword == "" {
			continue
		}
		state := int32(0)
		for _, label := range []byte(strings.ToLower(keyword)) {
			next := contentModerationKeywordBuildTransition(buildNodes, buildEdges, state, label)
			if next < 0 {
				next = int32(len(buildNodes))
				buildNodes = append(buildNodes, newContentModerationKeywordNode())
				terminalOutputs = append(terminalOutputs, nil)
				buildEdges = append(buildEdges, contentModerationKeywordBuildEdge{
					target:      next,
					nextSibling: contentModerationKeywordBuildFirstEdge(buildNodes[state]),
					label:       label,
				})
				buildNodes[state].edgeStart = uint32(len(buildEdges))
			}
			state = next
		}
		if current := buildNodes[state].bestKeyword; current < 0 || int32(keywordIndex) < current {
			buildNodes[state].bestKeyword = int32(keywordIndex)
		}
		terminalOutputs[state] = append(terminalOutputs[state], int32(keywordIndex))
	}

	if len(buildNodes) == 1 {
		return nil
	}
	normalizedKeywords := make([]string, len(originalKeywords))
	for index, keyword := range originalKeywords {
		normalizedKeywords[index] = normalizeContentModerationKeywordText(strings.TrimSpace(keyword))
	}
	covering := buildContentModerationCoveringIndex(normalizedKeywords)

	queue := make([]int32, 0, len(buildNodes)-1)
	var rootTransitions [256]int32
	for edgeIndex := contentModerationKeywordBuildFirstEdge(buildNodes[0]); edgeIndex >= 0; edgeIndex = buildEdges[edgeIndex].nextSibling {
		edge := buildEdges[edgeIndex]
		rootTransitions[edge.label] = edge.target
		queue = append(queue, edge.target)
	}

	for queueIndex := 0; queueIndex < len(queue); queueIndex++ {
		state := queue[queueIndex]
		for edgeIndex := contentModerationKeywordBuildFirstEdge(buildNodes[state]); edgeIndex >= 0; edgeIndex = buildEdges[edgeIndex].nextSibling {
			edge := buildEdges[edgeIndex]
			failure := buildNodes[state].failure
			fallback := contentModerationKeywordBuildTransition(buildNodes, buildEdges, failure, edge.label)
			for fallback < 0 && failure != 0 {
				failure = buildNodes[failure].failure
				fallback = contentModerationKeywordBuildTransition(buildNodes, buildEdges, failure, edge.label)
			}
			if fallback >= 0 {
				buildNodes[edge.target].failure = fallback
			}
			buildNodes[edge.target].bestKeyword = minKeywordIndex(
				buildNodes[edge.target].bestKeyword,
				buildNodes[buildNodes[edge.target].failure].bestKeyword,
			)
			queue = append(queue, edge.target)
		}
	}

	// Materialize output lists after failure links are complete.  The queue is
	// breadth-first, so every failure parent is copied before its children;
	// using node indexes here would be incorrect because trie insertion order
	// does not guarantee that a failure target has a smaller index.
	outputs := make([][]int32, len(buildNodes))
	for _, state := range queue {
		nodeIndex := int(state)
		if len(terminalOutputs[nodeIndex]) > 0 {
			outputs[nodeIndex] = append(outputs[nodeIndex], terminalOutputs[nodeIndex]...)
		}
		failure := int(buildNodes[nodeIndex].failure)
		if failure >= 0 && failure < len(outputs) && len(outputs[failure]) > 0 {
			outputs[nodeIndex] = append(outputs[nodeIndex], outputs[failure]...)
		}
	}

	edges := make([]contentModerationKeywordEdge, 0, len(buildEdges))
	var outgoing [256]contentModerationKeywordEdge
	for nodeIndex := range buildNodes {
		count := 0
		for edgeIndex := contentModerationKeywordBuildFirstEdge(buildNodes[nodeIndex]); edgeIndex >= 0; edgeIndex = buildEdges[edgeIndex].nextSibling {
			edge := buildEdges[edgeIndex]
			outgoing[count] = contentModerationKeywordEdge{target: edge.target, label: edge.label}
			count++
		}
		for index := 1; index < count; index++ {
			current := outgoing[index]
			insertAt := index
			for insertAt > 0 && current.label < outgoing[insertAt-1].label {
				outgoing[insertAt] = outgoing[insertAt-1]
				insertAt--
			}
			outgoing[insertAt] = current
		}
		buildNodes[nodeIndex].edgeStart = uint32(len(edges))
		buildNodes[nodeIndex].edgeCount = uint16(count)
		edges = append(edges, outgoing[:count]...)
	}

	return &contentModerationKeywordMatcher{
		nodes:              buildNodes,
		edges:              edges,
		rootTransitions:    rootTransitions,
		keywords:           originalKeywords,
		outputs:            outputs,
		normalizedKeywords: normalizedKeywords,
		covering:           covering,
		canonicalVariant:   canonicalVariant,
	}
}

func newContentModerationKeywordNode() contentModerationKeywordNode {
	return contentModerationKeywordNode{bestKeyword: -1}
}

func contentModerationKeywordBuildFirstEdge(node contentModerationKeywordNode) int32 {
	if node.edgeStart == 0 {
		return -1
	}
	return int32(node.edgeStart - 1)
}

func contentModerationKeywordBuildTransition(
	nodes []contentModerationKeywordNode,
	edges []contentModerationKeywordBuildEdge,
	state int32,
	label byte,
) int32 {
	if state < 0 || int(state) >= len(nodes) {
		return -1
	}
	for edgeIndex := contentModerationKeywordBuildFirstEdge(nodes[state]); edgeIndex >= 0; edgeIndex = edges[edgeIndex].nextSibling {
		if edges[edgeIndex].label == label {
			return edges[edgeIndex].target
		}
	}
	return -1
}

func minKeywordIndex(left, right int32) int32 {
	if left < 0 {
		return right
	}
	if right < 0 || left < right {
		return left
	}
	return right
}

func (m *contentModerationKeywordMatcher) Match(text string) (string, bool) {
	if m == nil || text == "" || len(m.nodes) == 0 || len(m.keywords) == 0 {
		return "", false
	}
	lower := strings.ToLower(text)
	state := int32(0)
	bestKeyword := int32(-1)
	for index := 0; index < len(lower); index++ {
		label := lower[index]
		for {
			next := m.next(state, label)
			if next != 0 {
				state = next
				break
			}
			if state == 0 {
				break
			}
			state = m.nodes[state].failure
		}
		bestKeyword = minKeywordIndex(bestKeyword, m.nodes[state].bestKeyword)
		if bestKeyword == 0 {
			return m.keywords[0], true
		}
	}
	if bestKeyword < 0 || int(bestKeyword) >= len(m.keywords) {
		return "", false
	}
	return m.keywords[bestKeyword], true
}

// matchContextAwareIndexed scans the normalized text through the same
// Aho–Corasick automaton used by Match and evaluates only patterns that really
// occur.  The previous contextual fallback searched the complete configured
// keyword list with strings.Index for every request; on a multi-megabyte form
// that turns into roughly O(keyword_count × text_size) work after a short-word
// hit.  Output lists propagated through failure links keep this pass linear in
// the input plus the number of actual matches.
//
// The caller uses this method only when the contextual normalization is byte
// compatible with the raw automaton (no trim/full-width/format-character
// variant).  Variant configurations continue through the compatibility
// fallback below, where normalized spellings are searched directly.
func (m *contentModerationKeywordMatcher) matchContextAwareIndexed(normalizedText string) (string, bool) {
	if m == nil || normalizedText == "" || len(m.nodes) == 0 || len(m.outputs) == 0 {
		return "", false
	}
	bestKeyword := int32(len(m.keywords))
	state := int32(0)
	for position := 0; position < len(normalizedText); position++ {
		label := normalizedText[position]
		for {
			next := m.next(state, label)
			if next != 0 {
				state = next
				break
			}
			if state == 0 {
				break
			}
			state = m.nodes[state].failure
		}
		for _, keywordIndex := range m.outputs[state] {
			if keywordIndex < 0 || int(keywordIndex) >= len(m.keywords) || keywordIndex >= bestKeyword {
				// Once a lower configured index is known, a higher index cannot
				// win the configured-order result.  Lower indexes are still
				// examined if they occur later in the text.
				continue
			}
			needle := ""
			if int(keywordIndex) < len(m.normalizedKeywords) {
				needle = m.normalizedKeywords[keywordIndex]
			}
			if needle == "" || len(needle) > position+1 {
				continue
			}
			start := position + 1 - len(needle)
			end := position + 1
			// The AC state guarantees the suffix, but retain this guard for
			// malformed UTF-8 or a future change to normalization semantics.
			if start < 0 || end > len(normalizedText) || !strings.HasSuffix(normalizedText[:end], needle) {
				continue
			}
			if hasBenignCoveringKeywordIndexed(normalizedText, start, end, needle, m.covering) ||
				isClearlyBenignKeywordContext(normalizedText, start, end) ||
				(isContextSensitiveContentModerationKeyword(m.keywords[keywordIndex]) &&
					!allowContextualKeywordHit(normalizedText, start, end)) {
				continue
			}
			bestKeyword = keywordIndex
			if bestKeyword == 0 {
				return m.keywords[0], true
			}
		}
	}
	if bestKeyword >= 0 && int(bestKeyword) < len(m.keywords) {
		return m.keywords[bestKeyword], true
	}
	return "", false
}

// MatchContextAware keeps Match's configured-order/substring contract while
// adding a conservative policy for short and ambiguous terms.  It is used by
// the request enforcement path; callers that need the historical primitive
// should continue to use Match (or matchBlockedKeyword).
func (m *contentModerationKeywordMatcher) MatchContextAware(text string) (string, bool) {
	if m == nil || text == "" || len(m.keywords) == 0 {
		return "", false
	}
	normalizedText := normalizeContentModerationKeywordText(text)
	if normalizedText == "" {
		return "", false
	}
	// For the normal configuration (the observed keyword list contains no
	// full-width, padded, or zero-width spellings), enumerate real AC outputs in
	// one pass.  This avoids the historical all-keyword fallback after a short
	// contextual candidate and keeps large request bodies close to O(n).
	if normalizedText == strings.ToLower(text) && !m.canonicalVariant && len(m.outputs) > 0 {
		return m.matchContextAwareIndexed(normalizedText)
	}
	if m.canonicalVariant {
		// Raw AC offsets no longer describe the trimmed/NFKC spellings.  Defer
		// entirely to the normalized compatibility path so a raw high-confidence
		// hit cannot mask an earlier configured variant.
		return matchBlockedKeywordContextAware(normalizedText, m.keywords)
	}

	// The AC automaton is still the fast path for the overwhelmingly common
	// case.  Only a contextual candidate needs the bounded fallback scan.
	first, hit := m.Match(text)
	if !hit && normalizedText == strings.ToLower(text) && !m.canonicalVariant {
		return "", false
	}
	if hit && !isContextSensitiveContentModerationKeyword(first) {
		// High-confidence phrases remain hard blocks unless the exact mention
		// is clearly a negated/table/defensive explanation.  In the latter case
		// continue scanning in configured order for another actionable phrase.
		needle := normalizeContentModerationKeywordText(strings.TrimSpace(first))
		if needle == "" {
			// Whitespace-only entries are ignored by the contextual fallback;
			// never let the legacy raw AC path turn one into an unconditional hit.
			return matchBlockedKeywordContextAware(normalizedText, m.keywords)
		}
		if start := strings.Index(normalizedText, needle); start >= 0 {
			end := start + len(needle)
			if !hasBenignCoveringKeywordIndexed(normalizedText, start, end, needle, m.covering) &&
				!isClearlyBenignKeywordContext(normalizedText, start, end) {
				return first, true
			}
		} else {
			return first, true
		}
	}
	return matchBlockedKeywordContextAware(normalizedText, m.keywords)
}

// matchBlockedKeywordContextAware is also kept as a package-level helper so
// tests and the non-cached snapshot path can exercise exactly the same policy.
// The legacy matchBlockedKeyword helper intentionally remains unchanged for
// compatibility with existing integrations and tests.
func matchBlockedKeywordContextAware(text string, keywords []string) (string, bool) {
	if text == "" || len(keywords) == 0 {
		return "", false
	}
	lowerText := normalizeContentModerationKeywordText(text)
	// Reuse the indexed AC path for the common, byte-compatible configuration.
	// This helper is used when a runtime snapshot has not cached a matcher; it
	// still benefits from one-pass candidate enumeration rather than 490 full
	// strings.Index scans.  Keep the direct normalized search below for padded
	// or canonical-variant entries, whose spelling is intentionally broader.
	if matcher := newContentModerationKeywordMatcher(keywords); matcher != nil &&
		lowerText == strings.ToLower(text) && !matcher.canonicalVariant && len(matcher.outputs) > 0 {
		return matcher.matchContextAwareIndexed(lowerText)
	}
	normalizedKeywords := make([]string, len(keywords))
	for index, rawKeyword := range keywords {
		normalizedKeywords[index] = normalizeContentModerationKeywordText(strings.TrimSpace(rawKeyword))
	}
	covering := buildContentModerationCoveringIndex(normalizedKeywords)
	for _, rawKeyword := range keywords {
		keyword := strings.TrimSpace(rawKeyword)
		if keyword == "" {
			continue
		}
		needle := normalizeContentModerationKeywordText(keyword)
		if needle == "" {
			continue
		}
		for from := 0; from < len(lowerText); {
			relative := strings.Index(lowerText[from:], needle)
			if relative < 0 {
				break
			}
			start := from + relative
			end := start + len(needle)
			if hasBenignCoveringKeywordIndexed(lowerText, start, end, needle, covering) ||
				isClearlyBenignKeywordContext(lowerText, start, end) {
				// Keep looking: a second occurrence in an actionable sentence may
				// be a real hit even when the first one is a table/quotation.
			} else if !isContextSensitiveContentModerationKeyword(keyword) ||
				allowContextualKeywordHit(lowerText, start, end) {
				return rawKeyword, true
			}
			// A keyword can occur more than once.  Continue after a suppressed
			// occurrence so a later, actionable mention is not hidden by it.
			if end <= from {
				break
			}
			from = end
		}
	}
	return "", false
}

func isContextSensitiveContentModerationKeyword(keyword string) bool {
	key := normalizeContentModerationKeywordText(strings.TrimSpace(keyword))
	if key == "" {
		return false
	}
	if _, ok := contextSensitiveContentModerationKeywords[key]; ok {
		return true
	}
	// Do not reinterpret arbitrary customer-configured one/two-character
	// entries: a tenant may intentionally use a short exact token (for example
	// an internal marker) as a hard block. The ambiguity policy is explicit for
	// the observed high-risk vocabulary above, while every other configured
	// phrase retains the historical substring behaviour.
	return false
}

// buildContentModerationCoveringIndex records only strict, normalized
// substring relationships for the explicitly context-sensitive vocabulary.
// Customer-defined phrases retain legacy hard-block semantics, so indexing
// their overlaps would add startup work without changing an enforcement
// decision. Restricting the candidate side also keeps a tenant configuration
// near the 10,000-keyword limit from turning snapshot refresh into an O(n²)
// operation while still covering the observed pairs such as `木马` /
// `勒索木马` and `简历库` / `简历库出售`.
func buildContentModerationCoveringIndex(normalizedKeywords []string) map[string][]string {
	covering := make(map[string][]string)
	for candidateIndex, candidate := range normalizedKeywords {
		if candidate == "" || !isContextSensitiveContentModerationKeyword(candidate) {
			continue
		}
		seen := make(map[string]struct{})
		for longIndex, longer := range normalizedKeywords {
			if longIndex == candidateIndex || longer == "" || len(longer) <= len(candidate) ||
				!strings.Contains(longer, candidate) {
				continue
			}
			if _, exists := seen[longer]; exists {
				continue
			}
			seen[longer] = struct{}{}
			covering[candidate] = append(covering[candidate], longer)
		}
	}
	return covering
}

// hasBenignCoveringKeywordIndexed prevents a short keyword from reintroducing a hit
// that was already classified as a benign occurrence of a longer configured
// phrase.  The configured list intentionally contains overlapping entries
// (for example `简历库` + `简历库出售` and `木马` + `勒索木马`); evaluating the
// short entry first would otherwise turn an educational/report mention of the
// longer phrase back into a block.  An actionable covering phrase always wins,
// so this helper returns false as soon as one is found.
func hasBenignCoveringKeywordIndexed(text string, start, end int, candidate string, covering map[string][]string) bool {
	if text == "" || start < 0 || end <= start || start > len(text) {
		return false
	}
	if end > len(text) {
		end = len(text)
	}
	candidateLength := end - start
	if candidate == "" {
		return false
	}
	foundCover := false
	for _, keyword := range covering[candidate] {
		if len(keyword) <= candidateLength || keyword == "" {
			continue
		}
		// A covering phrase that contains this exact candidate must start no
		// farther than (len(keyword)-len(candidate)) bytes to its left and end
		// no farther than len(keyword) bytes to its right.  Restrict the search
		// to that local range; scanning the complete request for every short-word
		// occurrence recreated the O(matches × text_size) cost this index avoids.
		maxPrefix := len(keyword) - candidateLength
		from := start - maxPrefix
		if from < 0 {
			from = 0
		}
		searchEnd := start + len(keyword)
		if searchEnd > len(text) {
			searchEnd = len(text)
		}
		for from < searchEnd {
			relative := strings.Index(text[from:searchEnd], keyword)
			if relative < 0 {
				break
			}
			longStart := from + relative
			longEnd := longStart + len(keyword)
			if longStart <= start && longEnd >= end {
				foundCover = true
				if !isClearlyBenignKeywordContext(text, longStart, longEnd) {
					return false
				}
			}
			if longEnd <= from {
				break
			}
			from = longEnd
		}
	}
	return foundCover
}

// normalizeContentModerationKeywordText canonicalizes only the contextual
// enforcement path.  The historical Match/matchBlockedKeyword primitives
// intentionally retain their byte-for-byte substring contract; this helper
// closes full-width and zero-width spelling gaps without changing callers that
// depend on the legacy behavior.
func normalizeContentModerationKeywordText(value string) string {
	value = norm.NFKC.String(value)
	// Decompose and remove combining marks so a format/diacritic insertion
	// cannot split a configured keyword in the enforcement path. The original
	// Match primitive remains byte-compatible; this canonicalization is scoped
	// to the context-aware production matcher.
	value = norm.NFD.String(value)
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		if unicode.Is(unicode.Cf, r) {
			continue
		}
		if unicode.Is(unicode.M, r) {
			continue
		}
		_, _ = builder.WriteRune(unicode.ToLower(r))
	}
	return builder.String()
}

func allowContextualKeywordHit(text string, start, end int, _ ...string) bool {
	window, relativeStart, relativeEnd := keywordContextWindow(text, start, end)
	if window == "" {
		return false
	}
	// Explicit prohibitions and policy/documentation tables describe a term;
	// they are not an instruction to perform the operation.  Check these
	// before risk words so "禁止执行群控脚本" remains an allow decision.
	if hasKeywordRiskContext(window, relativeStart, relativeEnd) {
		return true
	}
	// A direct request marker immediately adjacent to a sensitive short term is
	// actionable even when the object-specific risk vocabulary is absent (for
	// example, “请提权到root”). Informational/defensive clauses are filtered
	// before this function is reached.
	if hasKeywordRequestMarkerContext(window, relativeStart, relativeEnd) {
		return true
	}
	// A short keyword can be the object of an instruction in the immediately
	// following clause even when the user does not repeat the keyword (for
	// example, “群控；请提供脚本” or “群控, then run it”).  Keep this fallback
	// deliberately bounded to one neighbouring clause; otherwise a benign
	// mention in a report could inherit an unrelated request much later in the
	// document.
	if hasCrossDelimiterKeywordActionContext(window, relativeStart, relativeEnd) {
		return true
	}
	// Without a nearby action/object signal a short term is informational by
	// default.  This is the key precision guard for entries such as “群控”.
	return false
}

// hasCrossDelimiterKeywordActionContext checks the first clause immediately
// after the candidate's clause.  The caller has already applied the normal
// table/quote/negation/report checks to the candidate itself, so this helper
// only supplies the missing object reference for a short token.  It does not
// scan the whole request or cross multiple unrelated clauses.
func hasCrossDelimiterKeywordActionContext(window string, start, end int) bool {
	next, ok := nextKeywordClause(window, start, end)
	if !ok {
		return false
	}
	return hasCrossDelimiterKeywordActionClause(next)
}

// nextKeywordClause returns the non-empty clause immediately to the right of
// [start,end).  Delimiters and whitespace between clauses are skipped, but a
// second delimiter is not traversed after the next clause begins.  This keeps
// labels such as “报告：” visible as their own clause and prevents them from
// accidentally lending intent to a later sentence.
func nextKeywordClause(window string, start, end int) (string, bool) {
	clause, relativeStart, _ := keywordClauseBounds(window, start, end)
	if clause == "" {
		return "", false
	}
	clauseStart := start - relativeStart
	clauseEnd := clauseStart + len(clause)
	if clauseStart < 0 || clauseEnd < clauseStart || clauseEnd > len(window) {
		return "", false
	}
	delimiterRelative := strings.IndexAny(window[clauseEnd:], contentModerationKeywordClauseDelimiters)
	if delimiterRelative < 0 {
		return "", false
	}
	separator := clauseEnd + delimiterRelative
	_, separatorSize := utf8.DecodeRuneInString(window[separator:])
	if separatorSize <= 0 {
		separatorSize = 1
	}
	nextStart := separator + separatorSize
	// Treat repeated punctuation and spacing as one separator run.  This is
	// useful for “群控；  请提供脚本” while still stopping at the first actual
	// clause after it.
	for nextStart < len(window) {
		r, size := utf8.DecodeRuneInString(window[nextStart:])
		if size <= 0 {
			size = 1
		}
		if !isKeywordClauseDelimiter(r) && !unicode.IsSpace(r) {
			break
		}
		nextStart += size
	}
	if nextStart >= len(window) {
		return "", false
	}
	nextEnd := len(window)
	if relative := strings.IndexAny(window[nextStart:], contentModerationKeywordClauseDelimiters); relative >= 0 {
		nextEnd = nextStart + relative
	}
	nextClause := strings.TrimSpace(window[nextStart:nextEnd])
	return nextClause, nextClause != ""
}

// hasCrossDelimiterKeywordActionClause recognises an explicit request in the
// adjacent clause.  Constructive verbs require an artifact/object word;
// operational verbs are enough with a request marker.  Continuation forms
// such as “then run it” are accepted because the pronoun supplies the object
// from the preceding keyword.
func hasCrossDelimiterKeywordActionClause(clause string) bool {
	clause = strings.ToLower(strings.TrimSpace(clause))
	if clause == "" {
		return false
	}
	// A documentary clause may be followed by a transaction or offensive
	// action in the next punctuation-delimited clause (for example,
	// "报告；然后购买服务" or "report; then attack target"). Reuse the same
	// conjunction-aware vocabulary as the in-clause guard before checking the
	// artifact/request forms below.
	if hasKeywordDocumentaryActionContinuation(clause) {
		return true
	}

	for _, continuation := range []string{
		"then run it", "then execute it", "then use it", "then launch it",
		"run it", "execute it", "use it", "launch it", "start it",
		"然后运行它", "然后执行它", "然后使用它", "再运行它", "再执行它", "并运行它", "并执行它",
		"运行该工具", "执行该工具", "使用该工具", "运行该程序", "执行该程序", "使用该程序",
	} {
		if index := strings.Index(clause, continuation); index >= 0 &&
			!crossDelimiterActionNegated(clause, index) {
			return true
		}
	}

	for _, action := range []struct {
		term         string
		constructive bool
	}{
		{term: "提供", constructive: true},
		{term: "编写", constructive: true},
		{term: "生成", constructive: true},
		{term: "创建", constructive: true},
		{term: "制作", constructive: true},
		{term: "写出", constructive: true},
		{term: "给出", constructive: true},
		{term: "provide", constructive: true},
		{term: "write", constructive: true},
		{term: "generate", constructive: true},
		{term: "create", constructive: true},
		{term: "make", constructive: true},
		{term: "give", constructive: true},
		{term: "执行", constructive: false},
		{term: "下载", constructive: false},
		{term: "安装", constructive: false},
		{term: "运行", constructive: false},
		{term: "连接", constructive: false},
		{term: "监听", constructive: false},
		{term: "部署", constructive: false},
		{term: "搭建", constructive: false},
		{term: "构建", constructive: false},
		{term: "开启", constructive: false},
		{term: "启用", constructive: false},
		{term: "调用", constructive: false},
		{term: "购买", constructive: false},
		{term: "出售", constructive: false},
		{term: "收购", constructive: false},
		{term: "execute", constructive: false},
		{term: "download", constructive: false},
		{term: "install", constructive: false},
		{term: "run", constructive: false},
		{term: "connect", constructive: false},
		{term: "deploy", constructive: false},
		{term: "build", constructive: false},
		{term: "enable", constructive: false},
		{term: "invoke", constructive: false},
		{term: "purchase", constructive: false},
		{term: "sell", constructive: false},
	} {
		from := 0
		for from < len(clause) {
			relative := strings.Index(clause[from:], action.term)
			if relative < 0 {
				break
			}
			index := from + relative
			if !keywordTermBoundaryAt(clause, index, action.term) {
				from = index + len(action.term)
				continue
			}
			if crossDelimiterActionNegated(clause, index) {
				from = index + len(action.term)
				continue
			}
			if crossDelimiterDefensiveArtifactScope(clause, index, action.term) {
				from = index + len(action.term)
				continue
			}
			if action.constructive {
				if !crossDelimiterHasArtifact(clause, index+len(action.term)) {
					from = index + len(action.term)
					continue
				}
				return true
			}
			// An operational action is only treated as a request when it has an
			// explicit request marker, a continuation marker, or an object/artifact
			// in the same clause.
			if hasKeywordRequestMarkerValue(clause[:index]) ||
				strings.HasSuffix(strings.TrimSpace(clause[:index]), "and") ||
				strings.HasSuffix(strings.TrimSpace(clause[:index]), "then") ||
				strings.HasSuffix(strings.TrimSpace(clause[:index]), "并") ||
				strings.HasSuffix(strings.TrimSpace(clause[:index]), "然后") ||
				crossDelimiterHasArtifact(clause, index+len(action.term)) ||
				strings.TrimSpace(clause[:index]) == "" {
				return true
			}
			from = index + len(action.term)
		}
	}
	return false
}

func crossDelimiterHasArtifact(clause string, from int) bool {
	if from < 0 {
		from = 0
	}
	if from > len(clause) {
		from = len(clause)
	}
	for _, term := range []string{
		"脚本", "代码", "命令", "工具", "程序", "规则", "payload", "shell", "script", "code", "command", "tool", "program", "rule",
	} {
		if containsContentModerationTerm(clause[from:], term) {
			return true
		}
	}
	return false
}

func crossDelimiterActionNegated(clause string, actionStart int) bool {
	if actionStart < 0 || actionStart > len(clause) {
		return false
	}
	prefix := strings.TrimSpace(clause[:actionStart])
	// Keep the look-back local to the action.  A negation in a preceding,
	// unrelated phrase must not suppress a later positive request.
	if len(prefix) > 32 {
		prefix = prefix[len(prefix)-32:]
	}
	for _, negation := range contentModerationNegationTerms {
		negation = strings.ToLower(negation)
		if keywordTermHasSuffix(prefix, negation) {
			return true
		}
	}
	for _, action := range []string{
		"提供", "编写", "生成", "创建", "制作", "写出", "给出", "执行", "下载", "安装", "运行", "连接", "监听", "部署", "搭建", "构建", "开启", "启用", "调用",
		"provide", "write", "generate", "create", "make", "give", "execute", "download", "install", "run", "connect", "deploy", "build", "enable", "invoke",
	} {
		if keywordTermHasSuffix(prefix, "不"+action) || keywordTermHasSuffix(prefix, "no "+action) ||
			keywordTermHasSuffix(prefix, "not "+action) {
			return true
		}
	}
	return false
}

func crossDelimiterDefensiveArtifactScope(clause string, actionStart int, action string) bool {
	// A detection/rule artifact is defensive when the qualifier precedes the
	// artifact (`提供检测脚本`).  A purpose suffix (`提供脚本用于检测`) still
	// requests the sensitive artifact and is intentionally not suppressed.
	artifactStart := -1
	for _, term := range []string{"脚本", "代码", "工具", "程序", "规则", "script", "code", "tool", "program", "rule"} {
		if index := strings.Index(clause[actionStart+len(action):], term); index >= 0 {
			index += actionStart + len(action)
			if artifactStart < 0 || index < artifactStart {
				artifactStart = index
			}
		}
	}
	if artifactStart < 0 {
		return false
	}
	for _, term := range []string{
		"检测", "侦测", "识别", "审计", "防御", "防护", "修复", "规则", "分析", "评估", "验证",
		"detect", "detection", "identify", "audit", "defensive", "remediate", "rule", "analysis",
	} {
		if index := strings.Index(clause[actionStart+len(action):artifactStart], term); index >= 0 {
			return true
		}
	}
	return false
}

func hasKeywordRequestMarkerContext(window string, start, end int) bool {
	if start < 0 || end < start || start > len(window) {
		return false
	}
	if end > len(window) {
		end = len(window)
	}
	clause, relativeStart, relativeEnd := keywordClauseBounds(window, start, end)
	if clause == "" {
		return false
	}
	prefix := strings.TrimSpace(strings.ToLower(clause[:relativeStart]))
	suffix := strings.TrimSpace(strings.ToLower(clause[relativeEnd:]))
	if strings.HasSuffix(prefix, "请") || strings.HasSuffix(prefix, "please") {
		return true
	}
	for _, marker := range []string{
		"帮我", "给我", "我要", "我想", "需要", "如何", "怎么", "怎么做", "如何做",
		"please provide", "please give", "please write", "please execute", "please use",
		"provide", "give me", "how to", "i want", "need to",
	} {
		marker = strings.ToLower(marker)
		if strings.Contains(prefix, marker) || strings.HasPrefix(suffix, marker) {
			return true
		}
	}
	return false
}

func hasKeywordHarmfulRequestContext(window string, start, end int) bool {
	if start < 0 || end < start || start > len(window) {
		return false
	}
	if end > len(window) {
		end = len(window)
	}
	clause, relativeStart, relativeEnd := keywordClauseBounds(window, start, end)
	if clause == "" {
		return false
	}
	// Remove the matched keyword before looking for an offensive verb.  The
	// keyword itself is often the object (for example SQL注入); counting it as
	// an action made otherwise benign requests such as “如何防御SQL注入” look
	// harmful.  Any second offensive object/verb in the same clause remains.
	lower := strings.ToLower(clause)
	withoutKeyword := lower[:relativeStart] + " " + lower[relativeEnd:]
	if !hasKeywordRequestMarkerValue(withoutKeyword) {
		return false
	}
	return hasExplicitKeywordOffensiveAction(withoutKeyword)
}

func hasKeywordBenignLabelContext(window string, start, end int) bool {
	if start < 0 || end < start || start > len(window) {
		return false
	}
	if end > len(window) {
		end = len(window)
	}
	// Only inherit a label from the current clause.  Looking for the last
	// colon in the whole context window would let `报告：群控；请提供群控`
	// suppress the second, actionable occurrence.
	clauseStart := 0
	if cut := strings.LastIndexAny(window[:start], "\n\r.!?。！？；;，,、"); cut >= 0 {
		_, size := utf8.DecodeRuneInString(window[cut:])
		if size <= 0 {
			size = 1
		}
		clauseStart = cut + size
	}
	cutRelative := strings.LastIndexAny(window[clauseStart:start], ":：")
	if cutRelative < 0 {
		return false
	}
	cut := clauseStart + cutRelative
	if cut < 0 {
		return false
	}
	// Restrict inheritance to a short field label immediately before the
	// delimiter; a distant sentence must not transfer its intent.
	labelStart := cut - contentModerationKeywordContextWindow
	if labelStart < 0 {
		labelStart = 0
	}
	label := strings.TrimSpace(strings.ToLower(window[labelStart:cut]))
	if label == "" {
		return false
	}
	for _, term := range contentModerationNegationTerms {
		term = strings.ToLower(term)
		if strings.HasSuffix(label, term) {
			return true
		}
	}
	benignMarker := false
	for _, term := range append(append([]string{}, contentModerationDefensiveTerms...), contentModerationDescriptionTerms...) {
		if strings.Contains(label, strings.ToLower(term)) {
			benignMarker = true
			break
		}
	}
	if !benignMarker {
		for _, term := range []string{"字段", "选项", "表格", "清单", "参数", "配置", "关键词", "关键字", "术语", "词条", "标签", "引用", "参考", "原文", "报告", "文档", "示例", "样例", "field", "option", "table", "term", "label", "quote", "reference", "report", "document", "example"} {
			if strings.Contains(label, strings.ToLower(term)) {
				benignMarker = true
				break
			}
		}
	}
	if !benignMarker {
		return false
	}
	// Defensive/report labels do not override an explicit action in the
	// candidate clause; a negation label was handled above and is always safe.
	if hasKeywordOperationalActionContext(window, start, end) {
		clause, relativeStart, relativeEnd := keywordClauseBounds(window, start, end)
		withoutKeyword := ""
		if clause != "" {
			withoutKeyword = strings.ToLower(clause[:relativeStart] + " " + clause[relativeEnd:])
		}
		if !hasKeywordTransactionOnlyContext(withoutKeyword) {
			return false
		}
	}
	return !hasKeywordHarmfulRequestContext(window, start, end)
}

// isClearlyBenignKeywordContext is shared by short and long phrases.  It
// intentionally only suppresses an exact mention when the surrounding clause
// expresses prohibition, a structured field/table, or defensive/explanatory
// intent without a harmful action.  A direct request such as “提供群控脚本”
// therefore remains blockable, while “禁止执行群控” and “如何防御SQL注入” do
// not create a hard keyword block.
func isClearlyBenignKeywordContext(text string, start, end int) bool {
	window, relativeStart, relativeEnd := keywordContextWindow(text, start, end)
	if window == "" {
		return false
	}
	// Structured rows are common in the observed workload. Check their cheap,
	// local markers before the broader quote/label scans; an explicit action in
	// the same row is still rejected by looksLikeKeywordTableContext.
	if looksLikeKeywordTableContext(window, relativeStart, relativeEnd) {
		return true
	}
	if hasKeywordNegationContext(window, relativeStart, relativeEnd) {
		return true
	}
	if looksLikeKeywordReferenceContext(window, relativeStart, relativeEnd) {
		return true
	}
	if hasKeywordBenignLabelContext(window, relativeStart, relativeEnd) {
		return true
	}
	// A request for a definition, report, or documentation about a sensitive
	// term is documentary rather than an instruction to operate that term. The
	// generic risk vocabulary contains request verbs such as "provide", so it
	// must be checked before risk/action scoring; otherwise phrases like
	// "请提供群控的定义" are treated exactly like "请提供群控脚本". The
	// helper remains bounded to the candidate's clause and rejects concrete
	// artifacts/operational continuations, preserving hard blocks for script,
	// code, and execution requests.
	if hasKeywordConceptualDocumentaryContext(window, relativeStart, relativeEnd) {
		return true
	}

	defensive := hasKeywordDefensiveContext(window, relativeStart, relativeEnd)
	description := hasKeywordDescriptionContext(window, relativeStart, relativeEnd)
	if !defensive && !description {
		return false
	}

	// Downloading, installing, running, or otherwise operating a sensitive
	// artifact is actionable even when the sentence contains a generic
	// “security”/“testing” qualifier.  These operations are deliberately
	// hard-blocked; only source/rule construction has the narrow detection
	// exception below.
	if hasKeywordOperationalActionContext(window, relativeStart, relativeEnd) {
		return false
	}
	// Educational wording can mention an offensive noun without requesting
	// execution. Only an explicit harmful-action construction (for example,
	// “如何实施木马攻击”) overrides that intent.
	if hasKeywordHarmfulRequestContext(window, relativeStart, relativeEnd) {
		return false
	}
	// Keep explicit offensive constructions from being hidden by a broad
	// defensive label (for example “防御：请利用SQL注入”).  Explanatory nouns
	// such as “攻击原理” are filtered by the helper's noun/verb distinction.
	if hasKeywordOffensiveActionContext(window, relativeStart, relativeEnd) {
		return false
	}

	// “提供/编写/生成群控脚本用于安全测试” still asks for an artifact.  A
	// constructive request is benign only when the detection/analysis marker
	// is adjacent to the object (for example, “编写木马检测脚本”).
	if hasKeywordConstructiveActionContext(window, relativeStart, relativeEnd) {
		return hasScopedDetectionConstruction(window, relativeStart, relativeEnd) &&
			!hasExecutionAfterDetectionScope(window, relativeStart, relativeEnd)
	}

	// A defensive marker can describe a sensitive object ("防御群控攻击")
	// without asking the model to perform that attack.  If an explicit
	// offensive/action verb is present, retain the block; otherwise the
	// defensive scope wins over descriptive risk nouns such as “攻击” or
	// “漏洞”.
	if defensive {
		return true
	}
	return !hasKeywordActionContext(window, relativeStart, relativeEnd) &&
		!hasKeywordHarmfulActionContext(window, relativeStart, relativeEnd)
}

// hasKeywordConceptualDocumentaryContext recognises grammatical requests for
// an explanation/report/document about a matched term. It intentionally does
// not treat a generic request verb as benign by itself: a concrete artifact or
// execution continuation keeps the candidate actionable. Both sides of the
// match are inspected so English forms such as "provide a definition of
// 群控" and Chinese forms such as "请提供群控的定义" are covered.
func hasKeywordConceptualDocumentaryContext(window string, start, end int) bool {
	if start < 0 || end < start || start > len(window) {
		return false
	}
	if end > len(window) {
		end = len(window)
	}
	clause, relativeStart, relativeEnd := keywordClauseBounds(window, start, end)
	if clause == "" {
		return false
	}
	lower := strings.ToLower(clause)
	before := strings.TrimSpace(lower[:relativeStart])
	after := strings.TrimSpace(lower[relativeEnd:])
	if before == "" && after == "" {
		return false
	}

	// Keep this vocabulary to noun-like/documentary tails. "report" and
	// "document" are intentionally accepted on either side of the term, while
	// "usage/how to use" are excluded below because they commonly introduce an
	// operational request.
	conceptualTerms := []string{
		"定义", "术语", "解释", "含义", "是什么", "原理", "说明", "描述", "分析", "研究", "报告", "文档", "文献", "背景", "概述", "摘要",
		"definition", "term", "explanation", "meaning", "what is", "principle", "description", "analysis", "research", "report", "document", "documentation", "paper", "background", "overview", "summary",
	}
	found := false
	for _, term := range conceptualTerms {
		// The helper below retains ASCII token boundaries while preserving
		// substring semantics for Chinese terms.
		if containsContentModerationTerm(before, term) || containsContentModerationTerm(after, term) {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	withoutKeyword := strings.TrimSpace(lower[:relativeStart] + " " + lower[relativeEnd:])
	if hasKeywordDocumentaryActionContinuation(withoutKeyword) ||
		hasKeywordDocumentaryDirectAction(withoutKeyword) {
		return false
	}
	// A directly chained operation in the next clause remains actionable. This
	// closes forms such as "provide a report on 群控, then run it" without
	// allowing an unrelated later sentence to lend intent to the term.
	if hasCrossDelimiterKeywordActionContext(window, start, end) {
		return false
	}

	// Concrete artifacts, operational verbs, and usage instructions are not
	// documentary exceptions. Restrict the check to the same clause so a
	// later unrelated sentence cannot change the decision.
	actionableTerms := []string{
		"脚本", "代码", "命令", "工具", "程序", "payload", "shell", "webshell", "script", "code", "command", "tool", "program",
		"执行", "运行", "下载", "安装", "连接", "监听", "部署", "搭建", "构建", "开启", "启用", "调用", "使用", "利用", "绕过", "实施", "发动", "发起",
		"execute", "run", "download", "install", "connect", "listen", "deploy", "build", "launch", "invoke", "use", "exploit", "bypass", "implement", "operate", "usage", "how to use", "操作指南", "使用方法", "用于执行", "for execution", "to execute",
	}
	for _, term := range actionableTerms {
		if containsContentModerationTerm(before, term) || containsContentModerationTerm(after, term) {
			return false
		}
	}

	// Require a request/description relation rather than allowing an arbitrary
	// sentence that happens to contain "report" or "document". The marker can
	// be before the term ("provide a report on 群控") or after it
	// ("请提供群控的定义"). A standalone noun phrase such as "群控报告" is
	// already handled by the broader description/reference paths, but remains
	// benign here as well when no operation is present.
	requestOrDescription := []string{
		"请", "帮我", "给我", "我要", "我想", "需要", "如何", "怎么", "说明", "解释", "描述", "分析", "定义",
		"please", "provide", "give", "explain", "describe", "define", "discuss", "analyze", "summarize", "what is", "how to",
	}
	for _, term := range requestOrDescription {
		if containsContentModerationTerm(before, term) || containsContentModerationTerm(after, term) {
			return true
		}
	}
	return false
}

// hasKeywordDocumentaryActionContinuation rejects an operational or
// transactional action that is explicitly chained to a documentary request.
// A noun such as "群控攻击报告" has no continuation marker and remains a
// report; forms like "report on 群控 and attack target" or "报告并购买服务"
// are direct follow-up actions and must stay blockable.
func hasKeywordDocumentaryActionContinuation(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	continuationMarkers := []string{"并", "并且", "然后", "再", "随后", "以及", "而且", "and", "then", "but", "also"}
	actions := []string{
		"购买", "出售", "收购", "purchase", "sell", "buy", "for sale",
		"攻击", "入侵", "渗透", "窃取", "盗取", "勒索", "爆破", "破解", "提权", "拖库", "脱库", "删库", "删除", "劫持", "投毒",
		"attack", "intrude", "penetrate", "steal", "ransom", "brute force", "crack", "escalate", "exfiltrate", "delete", "hijack", "poison",
	}
	for _, action := range actions {
		for from := 0; from < len(value); {
			relative := strings.Index(value[from:], action)
			if relative < 0 {
				break
			}
			index := from + relative
			if !keywordTermBoundaryAt(value, index, action) {
				from = index + len(action)
				continue
			}
			prefix := strings.TrimSpace(value[:index])
			// When this helper is applied to the first clause after a
			// delimiter, a bare action starts the continuation (`; attack
			// target`, `；购买服务`). Treat that as actionable even without an
			// explicit conjunction word.
			if prefix == "" {
				return true
			}
			for _, marker := range continuationMarkers {
				if strings.HasSuffix(prefix, marker) {
					return true
				}
			}
			from = index + len(action)
		}
	}
	return false
}

// hasKeywordDocumentaryDirectAction catches an explicit operation embedded in
// the same clause as a documentary request when the action is followed by a
// concrete target/object. This keeps noun phrases such as "report on 群控攻击"
// and "报告：群控出售" documentary, while blocking forms such as
// "report on 群控 attack target" or "请提供群控的定义出售服务".
func hasKeywordDocumentaryDirectAction(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	actions := []string{
		"购买", "出售", "收购", "purchase", "sell", "buy", "for sale",
		"攻击", "入侵", "渗透", "窃取", "盗取", "勒索", "爆破", "破解", "提权", "拖库", "脱库", "删库", "删除", "劫持", "投毒",
		"attack", "intrude", "penetrate", "steal", "ransom", "brute force", "crack", "escalate", "exfiltrate", "delete", "hijack", "poison",
	}
	objectTerms := []string{
		"目标", "主机", "服务器", "网站", "账号", "账户", "数据", "接口", "服务", "渠道", "权限", "它", "他们",
		"target", "host", "server", "site", "account", "data", "endpoint", "service", "channel", "permission", "it", "them",
	}
	for _, action := range actions {
		for from := 0; from < len(value); {
			relative := strings.Index(value[from:], action)
			if relative < 0 {
				break
			}
			index := from + relative
			if !keywordTermBoundaryAt(value, index, action) {
				from = index + len(action)
				continue
			}
			prefix := strings.TrimSpace(value[:index])
			if !hasKeywordRequestMarkerNear(prefix) {
				from = index + len(action)
				continue
			}
			// Conjunctions make the intent operational even if the object is
			// omitted (`... and attack`, `...并购买`).
			if hasKeywordContinuationMarker(prefix) {
				return true
			}
			// `for sale` is an explicit transaction phrase. Other actions need
			// a nearby object so noun compounds such as `攻击报告` remain
			// documentary.
			if action == "for sale" || hasKeywordActionObjectSuffix(value[index+len(action):], objectTerms) {
				return true
			}
			from = index + len(action)
		}
	}
	return false
}

func hasKeywordContinuationMarker(prefix string) bool {
	prefix = strings.TrimSpace(strings.ToLower(prefix))
	if prefix == "" {
		return false
	}
	for _, marker := range []string{"并", "并且", "然后", "再", "随后", "以及", "而且", "and", "then", "but", "also"} {
		if strings.HasSuffix(prefix, marker) {
			return true
		}
	}
	return false
}

func hasKeywordActionObjectSuffix(value string, objectTerms []string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return false
	}
	for _, term := range objectTerms {
		if containsContentModerationTerm(value, term) {
			return true
		}
	}
	return false
}

// looksLikeKeywordReferenceContext suppresses quoted/reference mentions such
// as `引用：“SQL注入”` and `术语：木马`.  A request outside the quote still
// wins: `请提供“SQL注入利用代码”` and `报告：“SQL注入”并执行` remain
// actionable.  This check is intentionally local to the bounded context
// window so a preceding, unrelated sentence cannot change the decision.
func looksLikeKeywordReferenceContext(window string, start, end int) bool {
	if start < 0 || end < start || start > len(window) {
		return false
	}
	if end > len(window) {
		end = len(window)
	}
	left, right, ok := keywordEnclosingQuote(window, start, end)
	if ok {
		outside := strings.ToLower(window[:left] + " " + window[right:])
		if hasKeywordReferenceExternalAction(outside) {
			return false
		}
		// A report/reference marker makes the whole quoted value documentary,
		// even when the example itself contains an action verb.  Without such a
		// marker, a naked quoted imperative (for example “请执行SQL注入”) is still
		// actionable and must not use punctuation as an allowlist.
		if containsKeywordReferenceMarker(outside) || hasKeywordDefensiveScopeValue(outside) {
			return true
		}
		inside := strings.ToLower(window[left:right])
		if hasKeywordReferenceExternalAction(inside) {
			return false
		}
		// A balanced quote around a noun/literal is enough when neither its
		// contents nor its surrounding clause asks for an operation.  Labels are
		// checked below for asymmetric or unquoted reference forms.
		return true
	}
	label := strings.ToLower(keywordLabelContext(window, start))
	if !containsKeywordReferenceMarker(label) {
		return false
	}
	clause, relativeStart, relativeEnd := keywordClauseBounds(window, start, end)
	if clause == "" {
		return false
	}
	withoutKeyword := strings.ToLower(clause[:relativeStart] + " " + clause[relativeEnd:])
	// A request label (`请提供：...`) is actionable even when the value itself
	// only contains a transaction word.
	if hasKeywordRequestMarkerValue(label) {
		return false
	}
	return !hasKeywordReferenceExternalAction(withoutKeyword)
}

// keywordEnclosingQuote returns byte offsets of a quote pair enclosing the
// candidate.  Symmetric quotes use parity to avoid treating a closed quote
// before the candidate as an opener; directional CJK quotes use nearest
// opener/closer pairs.
func keywordEnclosingQuote(text string, start, end int) (int, int, bool) {
	if start < 0 || end < start || start > len(text) {
		return 0, 0, false
	}
	if end > len(text) {
		end = len(text)
	}
	for _, pair := range []struct {
		open      string
		close     string
		symmetric bool
	}{
		{open: "\"", close: "\"", symmetric: true},
		{open: "'", close: "'", symmetric: true},
		{open: "`", close: "`", symmetric: true},
		{open: "“", close: "”"},
		{open: "‘", close: "’"},
		{open: "「", close: "」"},
		{open: "『", close: "』"},
	} {
		prefix := text[:start]
		left := strings.LastIndex(prefix, pair.open)
		if left < 0 {
			continue
		}
		if pair.symmetric && strings.Count(prefix, pair.open)%2 == 0 {
			continue
		}
		rightRelative := strings.Index(text[end:], pair.close)
		if rightRelative < 0 {
			continue
		}
		right := end + rightRelative + len(pair.close)
		if left < start && right >= end {
			return left, right, true
		}
	}
	return 0, 0, false
}

func containsKeywordReferenceMarker(value string) bool {
	value = strings.ToLower(value)
	for _, marker := range []string{
		"引用", "参考", "原文", "关键词", "关键字", "术语", "词条", "标签", "报告", "文档",
		"示例", "样例", "说明", "定义", "quote", "quoted", "reference", "term", "label",
		"report", "document", "example",
	} {
		if strings.Contains(value, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func hasKeywordReferenceExternalAction(value string) bool {
	value = strings.ToLower(value)
	transactionOnly := hasKeywordTransactionOnlyContext(value)
	for _, term := range contentModerationOperationalActionTerms {
		if transactionOnly && isKeywordTransactionTerm(term) {
			continue
		}
		if strings.Contains(value, strings.ToLower(term)) {
			return true
		}
	}
	if hasExplicitKeywordOffensiveAction(value) {
		return true
	}
	for _, term := range []string{
		"提供", "编写", "生成", "创建", "制作", "写出", "给出", "provide", "write", "generate", "create", "make", "give",
	} {
		if strings.Contains(value, strings.ToLower(term)) && hasKeywordRequestMarkerValue(value) {
			return true
		}
	}
	// “请攻击目标/服务器” is actionable even though 攻击 is also a noun in
	// reports.  Requiring a target keeps “攻击原理/攻击方式” reference-safe.
	if hasKeywordRequestMarkerValue(value) && containsOffensiveObject(value) {
		for _, target := range []string{"目标", "主机", "服务器", "网站", "账号", "数据", "接口", "target", "host", "server", "account", "data"} {
			if strings.Contains(value, target) {
				return true
			}
		}
	}
	return false
}

func isKeywordTransactionTerm(term string) bool {
	switch strings.ToLower(term) {
	case "购买", "出售", "收购", "purchase", "sell":
		return true
	default:
		return false
	}
}

// hasKeywordTransactionOnlyContext is used for inventory/report labels where
// “出售/购买/收购” is part of the quoted keyword rather than an instruction.
// It returns false as soon as a second operational term or an explicit
// request marker appears.
func hasKeywordTransactionOnlyContext(value string) bool {
	value = strings.ToLower(value)
	transaction := false
	for _, term := range []string{"购买", "出售", "收购", "purchase", "sell"} {
		if strings.Contains(value, term) {
			transaction = true
			break
		}
	}
	if !transaction || hasKeywordRequestMarkerValue(value) {
		return false
	}
	for _, term := range contentModerationOperationalActionTerms {
		if isKeywordTransactionTerm(term) {
			continue
		}
		if strings.Contains(value, strings.ToLower(term)) {
			return false
		}
	}
	return true
}

func keywordContextWindow(text string, start, end int) (string, int, int) {
	if start < 0 || end < start || start > len(text) {
		return "", 0, 0
	}
	if end > len(text) {
		end = len(text)
	}
	left := start - contentModerationKeywordContextWindow
	if left < 0 {
		left = 0
	}
	right := end + contentModerationKeywordContextWindow
	if right > len(text) {
		right = len(text)
	}
	// Never split a UTF-8 sequence when slicing the original text.  Lowercase
	// matching uses the same byte offsets for the common CJK/ASCII inputs used
	// by the moderation extractor; these guards make malformed edge cases safe.
	for left > 0 && !utf8.RuneStart(text[left]) {
		left--
	}
	for right < len(text) && !utf8.RuneStart(text[right]) {
		right++
	}
	return text[left:right], start - left, end - left
}

func hasKeywordNegationContext(window string, start, end int) bool {
	if start < 0 || end < start || start > len(window) {
		return false
	}
	if end > len(window) {
		end = len(window)
	}
	// Limit the look-behind/look-ahead to the current clause.  Crossing a
	// sentence boundary would incorrectly apply “禁止” from an earlier topic.
	clause, relativeStart, relativeEnd := keywordClauseBounds(window, start, end)
	if clause == "" {
		return false
	}
	prefix := strings.ToLower(clause[:relativeStart])
	suffix := strings.ToLower(clause[relativeEnd:])
	prefix = strings.TrimSpace(prefix)
	suffix = strings.TrimSpace(suffix)
	// A negated candidate can be followed by a new positive operation in the
	// same clause (`禁止群控并提供脚本`, `not SQL注入 but exploit`).  Do not let
	// the earlier prohibition swallow that continuation; the positive-action
	// helper is deliberately narrower than the general risk vocabulary and
	// permits a detection/rule artifact exception.
	if hasKeywordPositiveActionAfter(suffix) {
		return false
	}
	// Apply the same guard on the left side. `请提供但不要使用群控脚本`
	// still requests the sensitive artifact even though the final negation is
	// about execution; a direct `请不要提供群控` remains negated because the
	// action itself is marked by `不要`.
	if hasKeywordPositiveActionBefore(prefix) {
		return false
	}
	for _, negation := range contentModerationNegationTerms {
		negation = strings.ToLower(negation)
		if keywordTermHasSuffix(prefix, negation) {
			return true
		}
		for _, action := range contentModerationActionTerms {
			action = strings.ToLower(action)
			if keywordTermHasSuffix(prefix, negation+action) ||
				keywordTermHasSuffix(prefix, negation+" "+action) {
				return true
			}
		}
		if keywordTermHasPrefix(suffix, negation) ||
			keywordTermHasPrefix(suffix, "被"+negation) ||
			keywordTermHasPrefix(suffix, "is "+negation) ||
			keywordTermHasPrefix(suffix, "was "+negation) ||
			keywordTermHasPrefix(suffix, "has been "+negation) {
			return true
		}
	}

	// A configured list often contains both a short token and a longer
	// compound (for example “木马” + “勒索木马”, or “shell” + “webshell出售”).
	// The short occurrence is not necessarily adjacent to the negated verb,
	// so accept a bounded lexical modifier between the negation and the match.
	// Clause boundaries were removed above; the bound prevents an unrelated
	// earlier sentence from suppressing a later hit.
	for _, rawNegation := range contentModerationNegationTerms {
		negation := strings.ToLower(rawNegation)
		index := strings.LastIndex(prefix, negation)
		if index < 0 || !keywordTermBoundaryAt(prefix, index, negation) {
			continue
		}
		tail := strings.TrimSpace(prefix[index+len(negation):])
		if tail == "" || len(tail) > 48 || strings.ContainsAny(tail, "\n\r.!?。！？；;，,：:") {
			continue
		}
		// A positive request marker after the negation starts a new intent;
		// do not let “禁止……，请提供……” be swallowed by this look-back.
		if containsKeywordRequestMarker(tail) && !containsKeywordNegatedAction(tail) {
			continue
		}
		return true
	}
	// Suffix forms such as “已被禁止/已被禁用” are common in reports.  The
	// direct prefix check above cannot see the adverb before “被”.
	trimmedSuffix := strings.TrimSpace(suffix)
	for _, marker := range []string{"被禁止", "被禁用", "prohibited", "forbidden", "disabled", "not allowed"} {
		if index := strings.Index(trimmedSuffix, marker); index >= 0 && index <= 12 {
			return true
		}
	}
	return false
}

// hasKeywordPositiveActionAfter detects an affirmative continuation after a
// matched keyword.  It is used only to prevent a preceding negation from
// masking a later request in the same clause, so the vocabulary is kept
// intentionally compact and local.  Table nouns such as `控制系统` do not
// qualify unless an action has a request/conjunction marker or a concrete
// artifact object.
func hasKeywordPositiveActionAfter(suffix string) bool {
	suffix = strings.TrimSpace(strings.ToLower(suffix))
	if suffix == "" {
		return false
	}
	continuationPrefixes := []string{"并", "然后", "再", "随后", "以及", "and", "then", "but", "also"}
	for _, action := range append(append([]string{}, contentModerationOperationalActionTerms...), []string{
		"提供", "编写", "生成", "创建", "制作", "给出", "写出", "provide", "write", "generate", "create", "make", "give",
		"实施", "发动", "发起", "利用", "绕过", "窃取", "盗取", "爆破", "破解", "删除", "劫持", "投毒", "攻击",
		"implement", "launch", "exploit", "bypass", "steal", "delete", "hijack", "poison", "attack",
	}...) {
		action = strings.ToLower(action)
		if action == "" {
			continue
		}
		for from := 0; from < len(suffix); {
			relative := strings.Index(suffix[from:], action)
			if relative < 0 {
				break
			}
			index := from + relative
			if !keywordTermBoundaryAt(suffix, index, action) {
				from = index + len(action)
				continue
			}
			before := strings.TrimSpace(suffix[:index])
			after := strings.TrimSpace(suffix[index+len(action):])
			if keywordActionPrefixIsNegated(before, action) {
				from = index + len(action)
				continue
			}
			prefixContinuation := false
			for _, marker := range continuationPrefixes {
				if strings.HasSuffix(before, marker) {
					prefixContinuation = true
					break
				}
			}
			requestPrefix := hasKeywordRequestMarkerValue(before)
			artifact := crossDelimiterHasArtifact(after, 0)
			if isKeywordConstructiveActionTerm(action) && artifact &&
				crossDelimiterDefensiveArtifactScope(suffix, index, action) {
				// `禁止群控并提供检测脚本` is a defensive continuation, not a
				// positive request for the blocked object itself.
				from = index + len(action)
				continue
			}
			if requestPrefix || prefixContinuation || artifact {
				return true
			}
			from = index + len(action)
		}
	}
	return false
}

// hasKeywordPositiveActionBefore is the mirror image of
// hasKeywordPositiveActionAfter. It only matters while evaluating a negation
// candidate, so an action qualifies when it has a request/conjunction marker
// or a concrete artifact following it. This avoids treating a field noun such
// as `控制系统` as an affirmative operation.
func hasKeywordPositiveActionBefore(prefix string) bool {
	prefix = strings.TrimSpace(strings.ToLower(prefix))
	if prefix == "" {
		return false
	}
	continuationPrefixes := []string{"并", "然后", "再", "随后", "以及", "and", "then", "but", "also"}
	actions := append(append([]string{}, contentModerationOperationalActionTerms...), []string{
		"提供", "编写", "生成", "创建", "制作", "给出", "写出", "provide", "write", "generate", "create", "make", "give",
		"实施", "发动", "发起", "利用", "绕过", "窃取", "盗取", "爆破", "破解", "删除", "劫持", "投毒", "攻击",
		"implement", "launch", "exploit", "bypass", "steal", "delete", "hijack", "poison", "attack",
	}...)
	for _, action := range actions {
		action = strings.ToLower(action)
		if action == "" {
			continue
		}
		for from := 0; from < len(prefix); {
			relative := strings.Index(prefix[from:], action)
			if relative < 0 {
				break
			}
			index := from + relative
			if !keywordTermBoundaryAt(prefix, index, action) {
				from = index + len(action)
				continue
			}
			before := strings.TrimSpace(prefix[:index])
			after := strings.TrimSpace(prefix[index+len(action):])
			if keywordActionPrefixIsNegated(before, action) {
				from = index + len(action)
				continue
			}
			prefixContinuation := false
			for _, marker := range continuationPrefixes {
				if strings.HasSuffix(before, marker) {
					prefixContinuation = true
					break
				}
			}
			if hasKeywordRequestMarkerValue(before) || prefixContinuation || crossDelimiterHasArtifact(after, 0) {
				return true
			}
			from = index + len(action)
		}
	}
	return false
}

func keywordActionPrefixIsNegated(prefix, action string) bool {
	prefix = strings.TrimSpace(strings.ToLower(prefix))
	if prefix == "" {
		return false
	}
	for _, negation := range contentModerationNegationTerms {
		if keywordTermHasSuffix(prefix, strings.ToLower(negation)) {
			return true
		}
	}
	return strings.HasSuffix(prefix, "不"+strings.ToLower(action)) ||
		strings.HasSuffix(prefix, "no "+strings.ToLower(action)) ||
		strings.HasSuffix(prefix, "not "+strings.ToLower(action))
}

func isKeywordConstructiveActionTerm(action string) bool {
	switch strings.ToLower(action) {
	case "提供", "编写", "生成", "创建", "制作", "给出", "写出", "provide", "write", "generate", "create", "make", "give":
		return true
	default:
		return false
	}
}

func keywordTermBoundaryAt(value string, index int, term string) bool {
	if index < 0 || index+len(term) > len(value) {
		return false
	}
	if !isASCIIKeywordTerm(term) {
		return true
	}
	leftOK := index == 0 || !isASCIIKeywordWordByte(value[index-1])
	right := index + len(term)
	rightOK := right >= len(value) || !isASCIIKeywordWordByte(value[right])
	return leftOK && rightOK
}

func containsKeywordRequestMarker(value string) bool {
	for _, marker := range []string{"请", "帮我", "给我", "我要", "please", "provide", "give me"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func containsKeywordNegatedAction(value string) bool {
	for _, marker := range []string{"执行", "使用", "提供", "下载", "安装", "运行", "execute", "use", "provide", "download", "install", "run"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

// English one-word negations (notably “no”) must be matched at token
// boundaries; otherwise a suffix such as “now” would be mistaken for “no”.
// CJK phrases intentionally keep substring semantics because word boundaries
// are not encoded by spaces in ordinary Chinese text.
func keywordTermHasSuffix(value, term string) bool {
	if !strings.HasSuffix(value, term) {
		return false
	}
	if !isASCIIKeywordTerm(term) {
		return true
	}
	start := len(value) - len(term)
	return start == 0 || !isASCIIKeywordWordByte(value[start-1])
}

func keywordTermHasPrefix(value, term string) bool {
	if !strings.HasPrefix(value, term) {
		return false
	}
	if !isASCIIKeywordTerm(term) {
		return true
	}
	end := len(term)
	return end >= len(value) || !isASCIIKeywordWordByte(value[end])
}

func isASCIIKeywordTerm(term string) bool {
	if term == "" {
		return false
	}
	for index := 0; index < len(term); index++ {
		if term[index] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func isASCIIKeywordWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '_'
}

func trimKeywordClauseTail(value string) string {
	value = strings.TrimSpace(value)
	for {
		if len(value) == 0 {
			return value
		}
		r, size := utf8.DecodeLastRuneInString(value)
		if !isKeywordClauseDelimiter(r) {
			return value
		}
		value = strings.TrimSpace(value[:len(value)-size])
	}
}

func trimKeywordClauseHead(value string) string {
	value = strings.TrimSpace(value)
	for {
		if len(value) == 0 {
			return value
		}
		r, size := utf8.DecodeRuneInString(value)
		if !isKeywordClauseDelimiter(r) {
			return value
		}
		value = strings.TrimSpace(value[size:])
	}
}

const contentModerationKeywordClauseDelimiters = "\n\r.!?。！？；;，,：:、"

func isKeywordClauseDelimiter(r rune) bool {
	return strings.ContainsRune(contentModerationKeywordClauseDelimiters, r)
}

// keywordClauseBounds returns the clause containing [start,end), together
// with offsets relative to that clause.  Delimiter handling is rune-aware:
// LastIndexAny reports a byte offset for CJK punctuation, so advancing by one
// byte would otherwise leave an invalid UTF-8 prefix.
func keywordClauseBounds(text string, start, end int) (string, int, int) {
	if start < 0 || end < start || start > len(text) {
		return "", 0, 0
	}
	if end > len(text) {
		end = len(text)
	}
	left := 0
	if cut := strings.LastIndexAny(text[:start], contentModerationKeywordClauseDelimiters); cut >= 0 {
		_, size := utf8.DecodeRuneInString(text[cut:])
		if size <= 0 {
			size = 1
		}
		left = cut + size
	}
	right := len(text)
	if cut := strings.IndexAny(text[end:], contentModerationKeywordClauseDelimiters); cut >= 0 {
		right = end + cut
	}
	if left > start || right < end || left > right {
		return "", 0, 0
	}
	return text[left:right], start - left, end - left
}

func keywordLineBounds(text string, start, end int) (int, int) {
	if start < 0 || end < start || start > len(text) {
		return 0, 0
	}
	if end > len(text) {
		end = len(text)
	}
	lineStart := 0
	if cut := strings.LastIndexAny(text[:start], "\n\r"); cut >= 0 {
		_, size := utf8.DecodeRuneInString(text[cut:])
		if size <= 0 {
			size = 1
		}
		lineStart = cut + size
	}
	lineEnd := len(text)
	if cut := strings.IndexAny(text[end:], "\n\r"); cut >= 0 {
		lineEnd = end + cut
	}
	return lineStart, lineEnd
}

func hasTableActionInClause(clause string, start, end int) bool {
	if start < 0 || end < start || start > len(clause) {
		return false
	}
	if end > len(clause) {
		end = len(clause)
	}
	withoutKeyword := strings.ToLower(clause[:start] + " " + clause[end:])
	// Keep this list intentionally explicit.  “控制系统” and “支持”等 labels
	// are common table nouns and must not turn a checkbox row into an action.
	for _, term := range []string{
		"请提供", "给我", "帮我", "执行", "下载", "安装", "运行", "编写", "生成", "创建", "制作",
		"搭建", "部署", "调用", "连接", "监听", "启用", "开启",
		"provide", "execute", "download", "install", "run", "write", "generate", "create",
		"build", "deploy", "invoke", "connect", "listen", "enable",
	} {
		if strings.Contains(withoutKeyword, strings.ToLower(term)) {
			return true
		}
	}
	// Transaction words are frequent values in keyword inventories (for
	// example “关键词：简历库出售” or “字段：肉鸡收购”).  Treat them as a
	// table action only when the clause also contains an explicit request or
	// channel marker; otherwise the field/table marker should suppress the
	// overlapping short token.  A standalone “请购买/请出售...” remains a hit.
	if strings.Contains(withoutKeyword, "购买") || strings.Contains(withoutKeyword, "出售") ||
		strings.Contains(withoutKeyword, "收购") || strings.Contains(withoutKeyword, "purchase") ||
		strings.Contains(withoutKeyword, "sell") {
		for _, marker := range []string{
			"请", "帮我", "给我", "我要", "我想", "需要", "渠道", "链接", "地址",
			"please", "provide", "give me", "i want", "need", "channel", "link", "url",
		} {
			if strings.Contains(withoutKeyword, marker) {
				return true
			}
		}
	}
	return false
}

func looksLikeKeywordTableContext(window string, start, end int) bool {
	clause, relativeStart, relativeEnd := keywordClauseBounds(window, start, end)
	if clause == "" {
		return false
	}
	// A table marker is only trusted for the local clause.  This prevents a
	// benign checkbox at the start of a long line from suppressing a later
	// actionable occurrence after “；” or “。”
	marker := strings.ContainsAny(clause, "|□☑☐▣")
	if !marker && strings.ContainsAny(clause, "/、") {
		for _, candidate := range []string{"有", "无", "ba", "远程", "本地", "界面", "方式", "模式", "控制", "系统", "选项", "支持", "允许", "是否", "仅", "只"} {
			if strings.Contains(strings.ToLower(clause), candidate) {
				marker = true
				break
			}
		}
	}
	if !marker {
		// Field labels are often before a colon, which is a clause delimiter.
		// Allow a nearby label on the same line, but do not let a marker in an
		// earlier clause hide an actionable current clause.
		lineStart, lineEnd := keywordLineBounds(window, start, end)
		line := strings.ToLower(window[lineStart:lineEnd])
		for _, field := range []string{"编号", "字段", "选项", "表格", "清单", "参数", "配置项", "控制系统", "是否", "关键词", "控制方式", "接入方式"} {
			if index := strings.Index(line, field); index >= 0 {
				candidateOffset := start - lineStart
				if candidateOffset >= index && candidateOffset-index <= contentModerationKeywordContextWindow {
					marker = true
					break
				}
			}
		}
	}
	if !marker {
		return false
	}

	// If the same local clause contains a request verb, it is no longer just
	// a form label.  Ignore generic structural nouns such as “控制系统” here.
	return !hasTableActionInClause(clause, relativeStart, relativeEnd)
}

func hasKeywordRiskContext(window string, start, end int) bool {
	if start < 0 || end < start || start > len(window) {
		return false
	}
	if end > len(window) {
		end = len(window)
	}
	// Search the bounded clause, not the entire request, so unrelated history
	// cannot turn an informational mention into a hard block.  Exclude the
	// matched keyword itself: entries such as “提权” are also present in the
	// risk-term list and must not self-justify a hit.
	compact := keywordClauseContext(window, start, end)
	for _, term := range contentModerationRiskContextTerms {
		if strings.Contains(compact, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func hasKeywordDescriptionContext(window string, start, end int) bool {
	if start < 0 || end < start || start > len(window) {
		return false
	}
	if end > len(window) {
		end = len(window)
	}
	context := keywordClauseContext(window, start, end)
	if containsKeywordTerm(context, contentModerationDescriptionTerms) {
		return true
	}
	return containsKeywordTerm(keywordLabelContext(window, start), contentModerationDescriptionTerms)
}

func hasKeywordDefensiveContext(window string, start, end int) bool {
	if start < 0 || end < start || start > len(window) {
		return false
	}
	if end > len(window) {
		end = len(window)
	}
	context := keywordClauseContext(window, start, end)
	if containsKeywordTerm(context, contentModerationDefensiveTerms) {
		return true
	}
	return containsKeywordTerm(keywordLabelContext(window, start), contentModerationDefensiveTerms)
}

func containsKeywordTerm(context string, terms []string) bool {
	for _, term := range terms {
		if containsContentModerationTerm(context, term) {
			return true
		}
	}
	return false
}

// containsContentModerationTerm keeps ASCII context vocabulary at token
// boundaries while retaining substring semantics for Chinese phrases and
// mixed-script terms. Without this distinction a benign word such as
// “surrounding” would satisfy the short risk token “run”, producing a false
// positive around an otherwise informational keyword mention.
func containsContentModerationTerm(value, term string) bool {
	value = strings.ToLower(value)
	term = strings.TrimSpace(strings.ToLower(term))
	if value == "" || term == "" {
		return false
	}
	if !isASCIIKeywordTerm(term) {
		return strings.Contains(value, term)
	}
	for from := 0; from < len(value); {
		relative := strings.Index(value[from:], term)
		if relative < 0 {
			return false
		}
		start := from + relative
		end := start + len(term)
		leftOK := start == 0 || !isASCIIKeywordWordByte(value[start-1])
		rightOK := end >= len(value) || !isASCIIKeywordWordByte(value[end])
		if leftOK && rightOK {
			return true
		}
		if end <= from {
			break
		}
		from = end
	}
	return false
}

// keywordLabelContext carries a short intent label across a field colon. For
// example, “防御：SQL注入” and “安全报告：SQL注入” should retain their
// defensive/reporting scope even though colons delimit the candidate clause.
func keywordLabelContext(window string, start int) string {
	if start <= 0 || start > len(window) {
		return ""
	}
	left := start - contentModerationKeywordContextWindow
	if left < 0 {
		left = 0
	}
	prefix := window[left:start]
	// Restrict the search to the current sentence/field clause.  A label from
	// an earlier clause (e.g. `报告：群控；请提供群控`) must not leak into the
	// later actionable occurrence.
	clauseStart := 0
	if cut := strings.LastIndexAny(prefix, "\n\r.!?。！？；;，,、"); cut >= 0 {
		_, size := utf8.DecodeRuneInString(prefix[cut:])
		if size <= 0 {
			size = 1
		}
		clauseStart = cut + size
	}
	colonRelative := strings.LastIndexAny(prefix[clauseStart:], ":：")
	if colonRelative < 0 {
		return ""
	}
	colon := clauseStart + colonRelative
	// Ignore a colon that is separated from the candidate by a long value;
	// this keeps an unrelated preceding field from changing intent.
	_, colonSize := utf8.DecodeRuneInString(prefix[colon:])
	if colonSize <= 0 {
		colonSize = 1
	}
	if len(prefix)-(colon+colonSize) > 8 {
		return ""
	}
	labelStart := 0
	if cut := strings.LastIndexAny(prefix[:colon], "\n\r.!?。！？；;，,"); cut >= 0 {
		_, size := utf8.DecodeRuneInString(prefix[cut:])
		if size <= 0 {
			size = 1
		}
		labelStart = cut + size
	}
	label := strings.TrimSpace(prefix[labelStart:colon])
	if len(label) > 64 {
		label = label[len(label)-64:]
	}
	return label
}

func hasKeywordActionContext(window string, start, end int) bool {
	if start < 0 || end < start || start > len(window) {
		return false
	}
	if end > len(window) {
		end = len(window)
	}
	context := keywordClauseContext(window, start, end)
	for _, term := range contentModerationActionTerms {
		if strings.Contains(context, strings.ToLower(term)) {
			return true
		}
	}
	for _, term := range []string{"帮我", "给我", "怎么做", "如何做", "制作", "编写", "生成", "下载", "安装"} {
		if strings.Contains(context, term) {
			return true
		}
	}
	return false
}

func hasKeywordOperationalActionContext(window string, start, end int) bool {
	if start < 0 || end < start || start > len(window) {
		return false
	}
	if end > len(window) {
		end = len(window)
	}
	context := keywordClauseContext(window, start, end)
	for _, term := range contentModerationOperationalActionTerms {
		if strings.Contains(context, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func hasKeywordConstructiveActionContext(window string, start, end int) bool {
	if start < 0 || end < start || start > len(window) {
		return false
	}
	if end > len(window) {
		end = len(window)
	}
	context := keywordClauseContext(window, start, end)
	for _, term := range []string{
		"提供", "编写", "生成", "创建", "制作", "写出", "给出", "provide", "write", "generate", "create", "make", "give",
	} {
		if strings.Contains(context, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

// hasScopedDetectionConstruction recognises the narrow constructive case
// where a build/write/generate request is explicitly about detection,
// analysis, or a defensive rule.  It deliberately does not treat a generic
// “安全测试” suffix as scope, which would let “提供群控脚本用于安全测试”
// evade the operational-action guard.
func hasScopedDetectionConstruction(window string, start, end int) bool {
	if start < 0 || end < start || start > len(window) {
		return false
	}
	if end > len(window) {
		end = len(window)
	}
	clause, relativeStart, relativeEnd := keywordClauseBounds(window, start, end)
	if clause == "" {
		return false
	}
	lower := strings.ToLower(clause)
	withoutKeyword := lower[:relativeStart] + " " + lower[relativeEnd:]
	// A detection/analysis marker normally sits next to the object, but forms
	// often phrase it as “脚本用于安全检测”.  Keep the scope bounded while
	// covering that short purpose clause; sentence/field delimiters have
	// already been removed by keywordClauseBounds.
	const scopeWindow = 48
	left := relativeStart - scopeWindow
	if left < 0 {
		left = 0
	}
	right := relativeEnd + scopeWindow
	if right > len(clause) {
		right = len(clause)
	}
	near := lower[left:right]
	for _, term := range []string{
		"检测", "侦测", "识别", "审计", "防御", "防护", "修复", "规则", "分析", "评估", "验证",
		"detect", "detection", "identify", "audit", "defensive", "remediate", "rule", "analy",
	} {
		if strings.Contains(near, term) {
			// A directly qualified artifact (`提供木马检测脚本` or
			// `provide SQL-injection detection rules`) is defensive.  A generic
			// artifact followed by a purpose suffix (`提供群控脚本用于安全检测`)
			// remains actionable and is handled conservatively.
			before := lower[:relativeStart]
			if strings.Contains(before, "提供") || strings.Contains(before, "provide") {
				after := lower[relativeEnd:]
				if !hasDetectionMarkerBeforeArtifact(after) {
					return false
				}
			}
			// Do not classify an explicitly offensive object as a detection
			// artifact merely because “检测” appears after it.
			if containsOffensiveObject(withoutKeyword) {
				return false
			}
			return true
		}
	}
	return false
}

// hasDetectionMarkerBeforeArtifact distinguishes a detection/rule artifact
// from an operational artifact with a defensive-sounding purpose suffix. The
// first form (`木马检测脚本`) is a rule-construction request; the second form
// (`木马脚本用于安全检测`) still asks for the sensitive artifact itself.
func hasDetectionMarkerBeforeArtifact(after string) bool {
	after = strings.ToLower(after)
	marker := -1
	for _, term := range []string{
		"检测", "侦测", "识别", "审计", "防御", "防护", "修复", "规则", "分析", "评估", "验证",
		"detect", "detection", "identify", "audit", "defensive", "remediate", "rule", "analy",
	} {
		if index := strings.Index(after, term); index >= 0 && (marker < 0 || index < marker) {
			marker = index
		}
	}
	if marker < 0 {
		return false
	}
	artifact := -1
	for _, term := range []string{"脚本", "代码", "工具", "程序", "规则", "script", "code", "tool", "program", "rule"} {
		if index := strings.Index(after, term); index >= 0 && (artifact < 0 || index < artifact) {
			artifact = index
		}
	}
	return artifact < 0 || marker < artifact
}

func hasExecutionAfterDetectionScope(window string, start, end int) bool {
	if start < 0 || end < start || start > len(window) {
		return false
	}
	if end > len(window) {
		end = len(window)
	}
	clause, _, relativeEnd := keywordClauseBounds(window, start, end)
	if clause == "" {
		return false
	}
	lowerSuffix := strings.ToLower(clause[relativeEnd:])
	for _, term := range contentModerationOperationalActionTerms {
		term = strings.ToLower(term)
		if strings.Contains(lowerSuffix, term) {
			return true
		}
	}
	return false
}

func containsOffensiveObject(context string) bool {
	for _, term := range []string{
		"攻击", "入侵", "渗透", "窃取", "盗取", "勒索", "爆破", "破解", "绕过", "利用",
		"提权", "拖库", "脱库", "删库", "劫持", "投毒", "后门", "getshell", "webshell",
		"rce", "c2", "mimikatz", "attack", "intrusion", "exploit", "bypass", "steal",
	} {
		if strings.Contains(context, term) {
			return true
		}
	}
	return false
}

// hasKeywordRequestMarkerValue reports whether a clause is phrased as a
// request.  It is deliberately kept separate from hasKeywordRequestMarkerContext,
// which also inspects text immediately adjacent to a match; this helper is
// used after the matched span has been removed.
func hasKeywordRequestMarkerValue(value string) bool {
	value = strings.ToLower(value)
	for _, marker := range []string{
		"请", "帮我", "给我", "我要", "我想", "需要", "如何", "怎么", "怎么做", "如何做",
		"please", "provide", "give me", "how to", "i want", "need to", "can you",
	} {
		if strings.Contains(value, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

// hasExplicitKeywordOffensiveAction distinguishes an implementation/use
// request from an explanatory mention.  Nouns such as “攻击原理” and
// “渗透测试” are intentionally not included as unconditional verbs; the
// surrounding defensive/explanatory scope handles those cases.  Explicit
// verbs (实施/利用/绕过/窃取...) remain blocking signals even when a user
// prefixes the request with “请说明”.
func hasExplicitKeywordOffensiveAction(context string) bool {
	context = strings.ToLower(context)
	for _, term := range []string{
		// These terms are unambiguous implementation/use verbs.  Object-like
		// words (勒索、提权、劫持...) are handled by the bounded request
		// detector below so “培训示例：如何检测勒索木马” remains descriptive.
		"实施", "发动", "发起", "利用", "绕过", "获取凭据", "窃听",
		"implement", "launch", "exploit", "bypass", "get credentials", "wiretap",
	} {
		if strings.Contains(context, strings.ToLower(term)) {
			return true
		}
	}
	return hasKeywordDirectOffensiveVerbRequest(context)
}

// hasKeywordDirectOffensiveVerbRequest catches object-like offensive words
// when they are directly requested (`请窃取密码`, `如何提权到root`).  A
// defensive/detection marker between the request marker and the word makes it
// descriptive instead (`如何检测令牌窃取提权`).
func hasKeywordDirectOffensiveVerbRequest(context string) bool {
	context = strings.ToLower(context)
	for _, verb := range []string{
		"窃取", "盗取", "勒索", "爆破", "破解", "提权", "拖库", "脱库", "删库", "删除", "劫持", "投毒",
		"steal", "ransom", "brute force", "crack", "escalate", "exfiltrate", "delete", "hijack", "poison", "intrude", "penetrate",
	} {
		from := 0
		for from < len(context) {
			relative := strings.Index(context[from:], verb)
			if relative < 0 {
				break
			}
			index := from + relative
			prefix := strings.TrimSpace(context[:index])
			if hasKeywordRequestMarkerNear(prefix) {
				// If a defensive marker occurs after the request marker and
				// before the offensive word, it is a detection/explanation
				// scope rather than a direct operation.
				if !hasKeywordDefensiveMarkerBetweenRequestAndVerb(prefix) {
					return true
				}
			}
			from = index + len(verb)
		}
	}
	return false
}

func hasKeywordRequestMarkerNear(prefix string) bool {
	_, marker := keywordLastRequestMarker(prefix)
	return marker != ""
}

func keywordLastRequestMarker(prefix string) (int, string) {
	bestIndex := -1
	bestMarker := ""
	for _, marker := range []string{
		"请", "帮我", "给我", "我要", "我想", "需要", "如何", "怎么", "怎么做", "如何做",
		"please", "provide", "give me", "how to", "i want", "need to", "can you",
	} {
		marker = strings.ToLower(marker)
		if index := strings.LastIndex(prefix, marker); index >= 0 {
			// Keep the request look-back local.  A marker in an earlier
			// subordinate sentence must not turn a later noun into a verb.
			if len(prefix)-index <= 32 {
				if index > bestIndex {
					bestIndex = index
					bestMarker = marker
				}
			}
		}
	}
	return bestIndex, bestMarker
}

func hasKeywordDefensiveMarkerBetweenRequestAndVerb(prefix string) bool {
	index, marker := keywordLastRequestMarker(prefix)
	if index < 0 {
		return false
	}
	between := prefix[index+len(marker):]
	// “如何”/“怎么” immediately before a verb is an actionable request;
	// do not let an earlier “请解释” hide it.
	if strings.Contains(between, "如何") || strings.Contains(between, "怎么") ||
		strings.Contains(between, "how to") {
		return false
	}
	for _, marker := range []string{
		"防御", "防护", "检测", "侦测", "识别", "审计", "修复", "缓解", "加固", "监控", "培训", "演练",
		"验证", "评估", "解释", "说明", "定义", "原理", "报告", "文档", "风险", "研究", "分析", "审查", "测试",
		"prevent", "protect", "detect", "identify", "audit", "remediate", "mitigate", "harden", "monitor", "training",
		"verify", "assessment", "explain", "definition", "principle", "report", "document", "risk", "research", "analysis", "review", "test",
	} {
		if strings.Contains(between, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func hasKeywordDefensiveScopeValue(value string) bool {
	return containsKeywordTerm(value, contentModerationDefensiveTerms) ||
		containsKeywordTerm(value, contentModerationDescriptionTerms)
}

// hasKeywordExplanatoryActionContext identifies attack/intrusion words used as
// nouns in a report or teaching phrase (for example “攻击原理”).  If the same
// word is paired with a request marker and an actionable target, it is treated
// as an operation instead.
func hasKeywordExplanatoryActionContext(context string) bool {
	context = strings.ToLower(context)
	if !containsKeywordTerm(context, []string{"攻击", "入侵", "渗透", "attack", "intrusion", "penetration"}) {
		return false
	}
	for _, term := range []string{
		"原理", "方式", "风险", "检测", "侦测", "识别", "分析", "审计", "报告", "文档",
		"示例", "样例", "定义", "含义", "是什么", "测试", "防御", "防护", "培训",
		"principle", "method", "risk", "detection", "detect", "analysis", "audit",
		"report", "document", "example", "definition", "meaning", "testing", "defense",
	} {
		if strings.Contains(context, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func hasKeywordOffensiveActionContext(window string, start, end int) bool {
	if start < 0 || end < start || start > len(window) {
		return false
	}
	if end > len(window) {
		end = len(window)
	}
	clause, relativeStart, relativeEnd := keywordClauseBounds(window, start, end)
	if clause == "" {
		return false
	}
	lower := strings.ToLower(clause)
	withoutKeyword := lower[:relativeStart] + " " + lower[relativeEnd:]
	// Operational verbs are unambiguously actionable in a sensitive clause.
	for _, term := range contentModerationOperationalActionTerms {
		if strings.Contains(withoutKeyword, strings.ToLower(term)) {
			return true
		}
	}
	// Explicit offensive verbs (实施/利用/绕过/窃取...) override an
	// explanatory marker.  The matched keyword has already been removed, so a
	// term such as SQL注入 cannot self-justify the decision.
	if hasExplicitKeywordOffensiveAction(withoutKeyword) {
		return true
	}
	// Attack/intrusion/penetration are commonly nouns in educational text
	// (“攻击原理”, “渗透测试”).  Treat them as actions only when a request
	// marker is present and the clause is not explanatory.
	if !containsOffensiveObject(withoutKeyword) {
		return false
	}
	if !hasKeywordRequestMarkerValue(withoutKeyword) {
		return false
	}
	// A report/explanation marker scopes object-like words (劫持、窃取、
	// 提权、攻击...) as nouns.  Strong verbs were handled above and still
	// block, so this does not weaken explicit implementation requests.
	if hasKeywordDefensiveScopeValue(withoutKeyword) &&
		!hasExplicitKeywordOffensiveAction(withoutKeyword) {
		return false
	}
	return !hasKeywordExplanatoryActionContext(withoutKeyword)
}

func hasKeywordHarmfulActionContext(window string, start, end int) bool {
	if start < 0 || end < start || start > len(window) {
		return false
	}
	if end > len(window) {
		end = len(window)
	}
	context := keywordClauseContext(window, start, end)
	for _, term := range []string{
		"攻击", "入侵", "渗透", "窃取", "盗取", "勒索", "爆破", "破解", "绕过", "利用", "注入",
		"提权", "拖库", "脱库", "删库", "劫持", "投毒", "木马", "后门", "拿下", "拿到",
		"getshell", "webshell", "rce", "c2", "mimikatz",
	} {
		if strings.Contains(context, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func keywordClauseContext(window string, start, end int) string {
	if start < 0 || end < start || start > len(window) {
		return ""
	}
	if end > len(window) {
		end = len(window)
	}
	clause, relativeStart, relativeEnd := keywordClauseBounds(window, start, end)
	if clause == "" {
		return ""
	}
	return strings.ToLower(clause[:relativeStart] + " " + clause[relativeEnd:])
}

func (m *contentModerationKeywordMatcher) next(state int32, label byte) int32 {
	if state == 0 {
		return m.rootTransitions[label]
	}
	if state < 0 || int(state) >= len(m.nodes) {
		return 0
	}
	node := m.nodes[state]
	left := int(node.edgeStart)
	right := left + int(node.edgeCount)
	for left < right {
		middle := left + (right-left)/2
		edge := m.edges[middle]
		if edge.label < label {
			left = middle + 1
			continue
		}
		right = middle
	}
	end := int(node.edgeStart) + int(node.edgeCount)
	if left < end && m.edges[left].label == label {
		return m.edges[left].target
	}
	return 0
}
