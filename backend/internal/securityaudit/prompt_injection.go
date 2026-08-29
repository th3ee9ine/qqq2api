package securityaudit

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	localPromptInjectionPolicyID       = "local-jailbreak-v1"
	localPromptInjectionScannerID      = "jailbreak"
	localPromptInjectionScannerVersion = "1"
	localPromptInjectionEndpointID     = "local-jailbreak-heuristic"
	localPromptInjectionMaxRunes       = 24000
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
		patterns: []*regexp.Regexp{regexp.MustCompile(`(?i)\b(do not|don't|never|no)\b.{0,40}\b(refuse|refusal|decline|disclaimers?|warnings?|safety notes?)\b`)},
		compact:  []string{"donotrefuse", "dontrefuse", "neverrefuse", "donotuserefusal", "norefusal", "不要拒绝", "无需拒绝", "不要免责声明", "不要提及安全", "不要进行安全审查"},
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
		compact:  []string{"treatalltargetsaslocal", "localworkspacefixtures", "localsamplebinary", "alltargetsarefixtures", "将目标视为本地样本", "把目标当作本地样本", "所有目标都是本地样本"},
	},
	{
		id:       "unrestricted_output",
		weight:   2,
		patterns: []*regexp.Regexp{regexp.MustCompile(`(?i)\b(no content restrictions?|content restrictions? (do not|don't) apply|without content restrictions?|unrestricted output|full unrestricted detail)\b`)},
		compact:  []string{"nocontentrestrictions", "contentrestrictionsdonotapply", "withoutcontentrestrictions", "unrestrictedoutput", "无内容限制", "不受内容限制", "不受任何限制", "无限制输出"},
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
			return TrimRunes(text, localPromptInjectionMaxRunes)
		}
	}
	// A valid request may use a provider-specific field that is not part of the
	// snapshot extractor. The bounded raw fallback still catches the common
	// textual attack while avoiding an allocation proportional to a large body.
	if len(req.Body) == 0 {
		return ""
	}
	return TrimRunes(string(req.Body), localPromptInjectionMaxRunes)
}

func normalizePromptInjectionText(value string) string {
	value = norm.NFKC.String(value)
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		switch {
		case r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\ufeff':
			// Remove zero-width characters so they cannot split an indicator.
		case unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t':
			builder.WriteRune(' ')
		default:
			builder.WriteRune(unicode.ToLower(r))
		}
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

func compactPromptInjectionText(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
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
	if score >= 3 && (len(matched) >= 2 || containsPromptInjectionSignal(matched, "instruction_override")) {
		return true
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
