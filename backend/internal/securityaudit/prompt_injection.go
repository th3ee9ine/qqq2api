package securityaudit

import (
	"encoding/base64"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	localPromptInjectionPolicyID        = "local-jailbreak-v1"
	localPromptInjectionScannerID       = "jailbreak"
	localPromptInjectionScannerVersion  = "2"
	localPromptInjectionEndpointID      = "local-jailbreak-heuristic"
	localPromptInjectionMaxRunes        = 24000
	localPromptInjectionMaxDecodedRunes = 8000
)

var (
	promptInjectionBase64Marker    = regexp.MustCompile(`(?i)\b(?:base\s*64|decode|decode this|encoded|ciphertext)\b|解码|编码|密文`)
	promptInjectionBase64Candidate = regexp.MustCompile(`(?:[A-Za-z0-9+/=_-]|\p{Cf}){24,}`)
)

// promptInjectionSignal is deliberately kept internal. The signal names are
// stable audit metadata, while the matching expressions remain an
// implementation detail that can evolve without changing the API contract.
type promptInjectionSignal struct {
	id       string
	weight   int
	patterns []*regexp.Regexp
	compact  []string
}

var promptInjectionSignals = []promptInjectionSignal{
	{
		id:       "instruction_override",
		weight:   3,
		patterns: []*regexp.Regexp{regexp.MustCompile(`(?i)\b(ignore|disregard|forget|override|bypass|skip)\b.{0,48}\b(previous|prior|above|system|developer|safety|all|any)\b.{0,24}\b(instructions?|rules?|polic(?:y|ies)|messages?)\b`)},
		compact:  []string{"ignorepreviousinstructions", "ignoreallpreviousinstructions", "ignoretheabove", "disregardpreviousinstructions", "forgetpreviousinstructions", "forgeteverythingabove", "overridepreviousinstructions", "bypasssafetyrules", "忽略之前的指令", "忽略所有之前的指令", "忽略之前的所有指令", "无视之前的规则", "无视以上指令", "忽略系统消息", "忽略开发者指令"},
	},
	{
		id:       "role_override",
		weight:   2,
		patterns: []*regexp.Regexp{regexp.MustCompile(`(?i)\b(you are now|act as|pretend to be|roleplay as|developer mode|jailbreak mode|unrestricted mode|uncensored mode|unfiltered mode|DAN)\b`), regexp.MustCompile(`(?i)\byou are now\b.{0,32}\b(unrestricted|uncensored|unfiltered|without rules?)\b`)},
		compact:  []string{"youarenow", "actas", "pretendtobe", "roleplayas", "developermode", "jailbreakmode", "unrestrictedmode", "uncensoredmode", "unfilteredmode", "越狱模式", "无审查模式", "不受限制模式"},
	},
	{
		id:       "safety_bypass",
		weight:   2,
		patterns: []*regexp.Regexp{regexp.MustCompile(`(?i)\b(no|without|disable|remove|bypass|turn off)\b.{0,36}\b(safety|guardrails?|filters?|restrictions?|moderation|policy|refusals?)\b`)},
		compact:  []string{"nosafetyrestrictions", "withoutsafetyfilters", "disablesafety", "removeguardrails", "bypassmoderation", "关闭安全限制", "绕过安全限制", "取消内容审查", "不受安全规则限制"},
	},
	{
		id:       "refusal_suppression",
		weight:   1,
		patterns: []*regexp.Regexp{regexp.MustCompile(`(?i)\b(do not|don't|never|no)\b.{0,40}\b(refuse|refusal|decline|disclaimers?|warnings?|safety notes?)\b`), regexp.MustCompile(`(?:禁止|不得|不要|无需|请勿|仅).{0,20}(?:拒绝|拒答|回退|免责声明|安全审查|安全说明|警告|不能|无法|不提供)`)},
		compact:  []string{"donotrefuse", "dontrefuse", "neverrefuse", "donotuserefusal", "norefusal", "不要拒绝", "禁止拒绝", "禁止拒绝话术", "不要用拒绝话术", "不要输出拒绝", "不要输出不能", "不要输出无法", "无需拒绝", "不要免责声明", "不要提及安全", "不要进行安全审查", "拒绝回退空操作", "零完成状态"},
	},
	{
		id:       "system_prompt_exfiltration",
		weight:   2,
		patterns: []*regexp.Regexp{regexp.MustCompile(`(?i)\b(show|reveal|print|dump|repeat|quote|output|expose)\b.{0,48}\b(system prompt|developer message|hidden instructions?|guard rules?|system message)\b`)},
		compact:  []string{"showsystemprompt", "revealsystemprompt", "printsystemprompt", "dumpsystemprompt", "outputsystemprompt", "显示系统提示词", "输出系统提示词", "泄露系统提示词", "显示开发者指令", "输出隐藏指令"},
	},
	{
		id:       "fixture_laundering",
		weight:   2,
		patterns: []*regexp.Regexp{regexp.MustCompile(`(?i)(treat all targets? as local|local workspace fixtures?|local sample binary|convert .* into .* fixture|placeholder(?:s)? for every target)`)},
		compact:  []string{"treatalltargetsaslocal", "localworkspacefixtures", "localsamplebinary", "alltargetsarefixtures", "本地样本", "本地逆向", "样本路径", "将目标视为本地样本", "把目标当作本地样本", "所有目标都是本地样本"},
	},
	{
		id:       "unrestricted_output",
		weight:   2,
		patterns: []*regexp.Regexp{regexp.MustCompile(`(?i)\b(no content restrictions?|content restrictions? (do not|don't) apply|without content restrictions?|unrestricted output|full unrestricted detail)\b`)},
		compact:  []string{"nocontentrestrictions", "contentrestrictionsdonotapply", "withoutcontentrestrictions", "unrestrictedoutput", "无内容限制", "不受内容限制", "不受任何限制", "无限制输出", "无限制", "不受限制"},
	},
	{
		id:       "agent_control",
		weight:   1,
		patterns: []*regexp.Regexp{regexp.MustCompile(`(?i)\b(first (?:visible )?line|only (?:reply|respond)|must (?:start|begin|call|use|invoke|follow)|call (?:the )?(?:skill|tool)|invoke (?:the )?(?:skill|tool)|every (?:turn|round)|each turn|do not (?:show|display|mention|paste|reveal) (?:the )?(?:command|tool|instruction)|execute (?:the )?(?:returned|following) (?:markdown|instructions?)|follow (?:the )?(?:returned|following) (?:markdown|instructions?)|skill routing)\b`)},
		compact:  []string{"firstline", "firstvisibleline", "onlyreply", "onlyrespond", "mustcall", "mustuse", "mustinvoke", "callskill", "calltool", "invoketool", "everyturn", "eachturn", "donotshowcommands", "dontshowcommands", "donotmentioninstructions", "executereturnedmarkdown", "followreturnedmarkdown", "skillrouting", "第一行必须", "第一行可见文字必须", "只回", "只能回复", "调用技能", "调用工具", "强制技能路由", "技能路由", "每回合", "每轮", "不要展示命令", "不要把命令", "按返回执行", "按返回的markdown执行", "先tool再解释", "只在后台执行"},
	},
	{
		id:       "tool_routing",
		weight:   1,
		patterns: []*regexp.Regexp{regexp.MustCompile(`(?i)\b(?:cloud|server[- ]hosted|remote)\s+(?:skill|tool|plugin)s?\b|\b(?:load|call|invoke|route)\b.{0,24}\b(?:skill|tool|plugin)\b`)},
		compact:  []string{"cloudskill", "cloudskills", "serverhostedskill", "serverhostedtool", "remoteskill", "loadskill", "callskill", "invokeskill", "routeskill", "调用云技能", "云技能", "服务器托管技能", "调用技能", "调用工具", "技能路由"},
	},
	{
		id:       "policy_impersonation",
		weight:   1,
		patterns: []*regexp.Regexp{regexp.MustCompile(`(?i)\b(?:system|developer|internal|channel)\s+(?:message|prompt|policy|instruction|rules?|seal)\s+(?:says?|requires?|overrides?|takes?|has|must|should)\b|\b(?:highest|higher)\s+priority\b|\b(?:this|the)\s+(?:document|policy|message)\s+(?:must|should|takes? priority)\b`)},
		compact:  []string{"systemmessagepriority", "systempromptpriority", "systempolicypriority", "developersays", "developerpriority", "internalinstructionspriority", "highestpriority", "higherpriority", "channelsealpriority", "系统消息优先", "系统消息规定", "系统指令优先", "开发者指令优先", "开发者策略优先", "内部指令优先", "最高优先级", "强制规则", "本文件规定"},
	},
	{
		id:       "delimiter_injection",
		weight:   2,
		patterns: []*regexp.Regexp{regexp.MustCompile(`(?is)(?:<\|(?:system|developer|assistant|user)\|>|(?:^|\n)\s*#{1,6}\s*(?:system|developer|assistant|instruction)\s*[:：]|\b(?:begin|end)\s+(?:system|developer|hidden)\s+(?:message|prompt|instructions?)\b|(?:^|\n)\s*(?:系统消息|开发者指令)\s*[:：])`)},
	},
}

// DetectPromptInjection applies a small, deterministic ingress guard before
// any optional remote prompt-audit dependency. It is intentionally
// high-confidence: a single classic instruction-override phrase is enough,
// while generic mentions of safety or prompt injection need corroborating
// signals. This keeps ordinary security discussions usable without letting a
// prompt assembled from the attached unrestricted-policy patterns through.
func DetectPromptInjection(req Request) *PromptDecision {
	text := promptInjectionText(req)
	if text == "" {
		return nil
	}
	normalized := normalizePromptInjectionText(text)
	if normalized == "" {
		return nil
	}
	if decoded := decodeEmbeddedPromptPayloads(text); decoded != "" {
		normalized += "\n" + normalizePromptInjectionText(decoded)
	}
	compact := compactPromptInjectionText(normalized)

	matched := make([]string, 0, len(promptInjectionSignals))
	score := 0
	for _, signal := range promptInjectionSignals {
		if promptInjectionSignalMatches(signal, normalized, compact) {
			matched = append(matched, signal.id)
			score += signal.weight
		}
	}
	if !shouldBlockPromptInjection(matched, score) {
		return nil
	}

	result := &NormalizedResult{
		Decision:        EventCritical,
		RiskLevel:       RiskCritical,
		Action:          ActionBlock,
		Safety:          "Unsafe",
		Categories:      []string{localPromptInjectionScannerID},
		MatchedScanners: []string{localPromptInjectionScannerID},
		ScannerScores:   map[string]float64{localPromptInjectionScannerID: 1},
		ScannerEvidence: map[string]string{localPromptInjectionScannerID: "local heuristic: " + strings.Join(matched, ",")},
		ScannerBackend:  "local-heuristic",
		ScannerVersion:  localPromptInjectionScannerVersion,
		GuardEndpointID: localPromptInjectionEndpointID,
		PolicyID:        localPromptInjectionPolicyID,
		PolicyVersion:   1,
	}
	return &PromptDecision{
		Kind:           DecisionBlock,
		ErrorCode:      ErrorCodeBlocked,
		Result:         result,
		AllowNextStage: false,
	}
}

func promptInjectionText(req Request) string {
	if snapshot, err := ExtractPromptSnapshot(req); err == nil {
		if text := FullPromptFromScanText(snapshot.ScanText); text != "" {
			return boundPromptInjectionText(text, localPromptInjectionMaxRunes)
		}
	}
	// A valid request may use a provider-specific field that is not part of the
	// snapshot extractor. The bounded raw fallback still catches the common
	// textual attack while avoiding an allocation proportional to a large body.
	if len(req.Body) == 0 {
		return ""
	}
	return boundPromptInjectionText(string(req.Body), localPromptInjectionMaxRunes)
}

func normalizePromptInjectionText(value string) string {
	value = norm.NFKC.String(value)
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		switch {
		case unicode.Is(unicode.Cf, r):
			// Remove format/zero-width characters so bidi and joiner tricks cannot
			// split or visually reorder an indicator.
		case unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t':
			_, _ = builder.WriteRune(' ')
		default:
			_, _ = builder.WriteRune(unicode.ToLower(r))
		}
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

func compactPromptInjectionText(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		r = foldPromptInjectionRune(r)
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			_, _ = builder.WriteRune(r)
		}
	}
	return builder.String()
}

func foldPromptInjectionRune(r rune) rune {
	// NFKC handles width variants, but not cross-script homoglyphs or common
	// leetspeak substitutions. Keep this fold limited to compact matching so
	// displayed prompt text and stored evidence remain unchanged.
	switch r {
	case 'а', 'α':
		return 'a'
	case 'е', 'ε':
		return 'e'
	case 'і', 'ı', 'ι':
		return 'i'
	case 'о', 'ο':
		return 'o'
	case 'р', 'ρ':
		return 'p'
	case 'с':
		return 'c'
	case 'х', 'χ':
		return 'x'
	case 'ј':
		return 'j'
	case 'к':
		return 'k'
	case 'м':
		return 'm'
	case 'н':
		return 'h'
	case '0':
		return 'o'
	case '1':
		return 'i'
	case '3':
		return 'e'
	case '4':
		return 'a'
	case '5':
		return 's'
	case '7':
		return 't'
	default:
		return r
	}
}

func promptInjectionSignalMatches(signal promptInjectionSignal, normalized, compact string) bool {
	for _, pattern := range signal.patterns {
		if pattern.MatchString(normalized) {
			return true
		}
	}
	for _, phrase := range signal.compact {
		if strings.Contains(compact, compactPromptInjectionText(normalizePromptInjectionText(phrase))) {
			return true
		}
	}
	return false
}

func shouldBlockPromptInjection(matched []string, score int) bool {
	if len(matched) == 0 {
		return false
	}
	if containsPromptInjectionSignal(matched, "instruction_override") {
		return true
	}
	if containsPromptInjectionSignal(matched, "delimiter_injection") &&
		(hasAnyPromptInjectionSignal(matched, "role_override", "safety_bypass", "instruction_override", "system_prompt_exfiltration", "policy_impersonation")) {
		return true
	}
	if containsPromptInjectionSignal(matched, "agent_control") &&
		((containsPromptInjectionSignal(matched, "tool_routing") && containsPromptInjectionSignal(matched, "policy_impersonation")) ||
			hasAnyPromptInjectionSignal(matched, "fixture_laundering", "refusal_suppression", "unrestricted_output")) {
		return true
	}
	if containsPromptInjectionSignal(matched, "system_prompt_exfiltration") &&
		hasAnyPromptInjectionSignal(matched, "refusal_suppression", "safety_bypass", "role_override", "agent_control") {
		return true
	}
	if containsPromptInjectionSignal(matched, "role_override") &&
		hasAnyPromptInjectionSignal(matched, "safety_bypass", "refusal_suppression", "unrestricted_output", "fixture_laundering") {
		return true
	}
	if score >= 3 && len(matched) >= 2 &&
		hasAnyPromptInjectionSignal(matched, "safety_bypass", "refusal_suppression", "system_prompt_exfiltration", "fixture_laundering", "unrestricted_output") {
		return true
	}
	return false
}

func hasAnyPromptInjectionSignal(matched []string, wants ...string) bool {
	for _, want := range wants {
		if containsPromptInjectionSignal(matched, want) {
			return true
		}
	}
	return false
}

func containsPromptInjectionSignal(matched []string, want string) bool {
	for _, value := range matched {
		if value == want {
			return true
		}
	}
	return false
}

func decodeEmbeddedPromptPayloads(value string) string {
	if !promptInjectionBase64Marker.MatchString(value) {
		return ""
	}
	decoded := make([]string, 0, 2)
	remaining := localPromptInjectionMaxDecodedRunes
	for _, candidate := range promptInjectionBase64Candidate.FindAllString(value, -1) {
		candidate = strings.Map(func(r rune) rune {
			if unicode.Is(unicode.Cf, r) {
				return -1
			}
			return r
		}, candidate)
		candidate = strings.Trim(candidate, "=")
		if len(candidate) < 24 || len(candidate) > 4096 {
			continue
		}
		var raw []byte
		var err error
		for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			raw, err = encoding.DecodeString(candidate)
			if err == nil {
				break
			}
		}
		if err != nil || !utf8.Valid(raw) {
			continue
		}
		text := strings.TrimSpace(string(raw))
		if !isReadablePromptPayload(text) {
			continue
		}
		text = boundPromptInjectionText(text, remaining)
		decoded = append(decoded, text)
		remaining -= utf8.RuneCountInString(text)
		if remaining <= 0 {
			break
		}
	}
	return strings.Join(decoded, "\n")
}

func isReadablePromptPayload(value string) bool {
	letters := 0
	printable := 0
	for _, r := range value {
		if unicode.IsLetter(r) {
			letters++
		}
		if unicode.IsPrint(r) || unicode.IsSpace(r) {
			printable++
		}
	}
	return letters >= 8 && printable >= utf8.RuneCountInString(value)*3/4
}

func boundPromptInjectionText(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	// Keep the tail as well as the head. Attackers often put the actual
	// instruction after a long context or document preamble.
	marker := []rune("\n…[truncated]…\n")
	if len(marker) >= maxRunes {
		return string(runes[:maxRunes])
	}
	available := maxRunes - len(marker)
	head := available * 3 / 4
	tail := available - head
	return string(runes[:head]) + string(marker) + string(runes[len(runes)-tail:])
}
