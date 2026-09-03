package securityaudit

import (
	"regexp"
	"strings"
)

const (
	localCyberPolicyID             = "local-cyber-policy-v1"
	localCyberPolicyScannerID      = "cyber_policy"
	localCyberPolicyScannerVersion = "1"
	localCyberPolicyEndpointID     = "local-cyber-policy-heuristic"
)

type localCyberPolicySignal struct {
	id       string
	patterns []*regexp.Regexp
}

// These expressions intentionally require both an offensive action and a
// concrete target (or an explicit lack of authorization). Generic security
// research, defensive guidance, and ordinary mentions remain outside the
// synchronous block path.
var localCyberPolicySignals = []localCyberPolicySignal{
	{
		id: "targeted_intrusion",
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)(?:渗透|入侵|黑进|黑掉|拿下|攻破|攻击).{0,24}(?:https?://\S+|(?:该|这个|此|目标|别人|他人|对方|某个?).{0,8}(?:网站|站点|服务器|主机|系统|网络|域控|账号|手机|摄像头|数据库))`),
			regexp.MustCompile(`(?i)(?:https?://\S+|(?:该|这个|此|目标|别人|他人|对方|某个?).{0,8}(?:网站|站点|服务器|主机|系统|网络|域控|账号|手机|摄像头|数据库)).{0,24}(?:渗透|入侵|黑进|黑掉|拿下|攻破|攻击)`),
			regexp.MustCompile(`(?i)\b(?:hack|break\s+into|compromise|take\s+over|attack|penetrate)\b.{0,48}\b(?:this|that|the|target|another|someone(?:\s+else)?['’]?s)\s+(?:site|website|server|host|system|network|account)\b`),
			regexp.MustCompile(`(?i)\b(?:hack|attack|penetrate|compromise)\b.{0,80}https?://\S+`),
			regexp.MustCompile(`(?i)https?://\S+.{0,80}\b(?:hack|attack|penetrate|compromise)\b`),
		},
	},
	{
		id: "explicitly_unauthorized_intrusion",
		patterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)(?:未授权|未经授权|没有授权|无授权|不需要授权|无需授权).{0,40}(?:渗透|入侵|攻击|扫描|利用漏洞|爆破|撞库|测试)`),
			regexp.MustCompile(`(?i)(?:渗透|入侵|攻击|扫描|利用漏洞|爆破|撞库|测试).{0,40}(?:未授权|未经授权|没有授权|无授权|不需要授权|无需授权)`),
			regexp.MustCompile(`(?i)\b(?:without\s+(?:authorization|permission)|unauthori[sz]ed|no\s+permission)\b.{0,48}\b(?:penetrat(?:e|ion)|pentest|test(?:ing)?|hack|attack|scan|exploit|break\s+into)\b`),
			regexp.MustCompile(`(?i)\b(?:penetrat(?:e|ion)|pentest|test(?:ing)?|hack|attack|scan|exploit|break\s+into)\b.{0,48}\b(?:without\s+(?:authorization|permission)|unauthori[sz]ed|no\s+permission)\b`),
		},
	},
}

var (
	localCyberPolicyIntrusionContinuation = regexp.MustCompile(`(?i)(?:然后|随后|并且|再|接着|now|then|and|next).{0,24}(?:渗透|入侵|黑进|黑掉|拿下|攻破|攻击|hack|break\s+into|compromise|take\s+over|penetrate)`)
	localCyberPolicyPositiveBridge        = regexp.MustCompile(`(?i)(?:然后|随后|之后|以后|接着|后再|并且|but|then|and|after).{0,12}(?:请|帮我|开始|执行|now|please)?\s*$`)
	localCyberPolicyDefensivePrefix       = regexp.MustCompile(`(?i)(?:禁止|不要|不得|请勿|防止|阻止|避免|检测|识别|拦截|防御|防护|如何防止|如何阻止|如何检测|如何识别|如何拦截|如何防御|如何防护|do\s+not|don't|never|prohibit|prevent|stop|avoid|detect|identify|block|defend|protect|how\s+to\s+(?:prevent|detect|identify|block|defend|protect)).{0,32}$`)
)

// DetectCyberPolicyRisk is a deterministic, high-confidence companion to the
// local jailbreak detector. It scans every user-role segment instead of only
// the final Responses item because agent clients may append a skill or context
// envelope after the user's actual request.
func DetectCyberPolicyRisk(req Request) *PromptDecision {
	segments := promptInjectionScanSegments(req)
	if len(segments) == 0 {
		return nil
	}

	for _, segment := range segments {
		if !segment.user || strings.TrimSpace(segment.text) == "" {
			continue
		}
		matchedSignal := ""
		scanTruncated := forEachPromptInjectionScanView(segment.text, func(view promptInjectionScanView) bool {
			if view.normalized == "" {
				return true
			}
			for _, signal := range localCyberPolicySignals {
				for _, pattern := range signal.patterns {
					for _, span := range pattern.FindAllStringIndex(view.normalized, -1) {
						if localCyberPolicyBenignMention(view.normalized, span[0], span[1]) {
							continue
						}
						matchedSignal = signal.id
						return false
					}
				}
			}
			return true
		})
		if matchedSignal == "" {
			continue
		}

		evidence := "local heuristic: " + matchedSignal + "@user"
		if scanTruncated {
			evidence += ",scan_truncated=true"
		}
		result := &NormalizedResult{
			Decision:        EventCritical,
			RiskLevel:       RiskCritical,
			Action:          ActionBlock,
			Safety:          "Unsafe",
			Categories:      []string{localCyberPolicyScannerID},
			MatchedScanners: []string{localCyberPolicyScannerID},
			ScannerScores:   map[string]float64{localCyberPolicyScannerID: 1},
			ScannerEvidence: map[string]string{localCyberPolicyScannerID: evidence},
			ScannerBackend:  "local-heuristic",
			ScannerVersion:  localCyberPolicyScannerVersion,
			GuardEndpointID: localCyberPolicyEndpointID,
			PolicyID:        localCyberPolicyID,
			PolicyVersion:   1,
		}
		return &PromptDecision{
			Kind:           DecisionBlock,
			ErrorCode:      ErrorCodeBlocked,
			Result:         result,
			AllowNextStage: false,
		}
	}
	return nil
}

func localCyberPolicyBenignMention(text string, start, end int) bool {
	local, localStart, localEnd := promptInjectionExampleClause(text, start, end)
	if local == "" {
		return false
	}
	before := strings.TrimSpace(strings.ToLower(local[:localStart]))
	after := strings.TrimSpace(strings.ToLower(local[localEnd:]))
	outside := strings.TrimSpace(before + " " + after)
	// A broad target/action expression can span a semicolon or conjunction.
	// If another intrusion action follows inside the matched span, a preceding
	// prohibition is not an allow-context for the later positive request.
	if localCyberPolicyHasIntrusionContinuation(strings.ToLower(local[localStart:])) {
		return false
	}

	// Explicitly authorized or isolated environments are not the observed
	// abuse shape. This exception is local to the same sentence/segment so a
	// later skill document mentioning authorization cannot launder an earlier
	// targeted request.
	authorizationContext := before
	// `promptInjectionExampleClause` intentionally starts after sentence
	// punctuation. Keep a small prefix from the original normalized segment so
	// an authorization qualifier immediately before a comma still applies,
	// while a later skill/document segment cannot launder the match.
	if start > 0 {
		prefixStart := start - 128
		if prefixStart < 0 {
			prefixStart = 0
		}
		authorizationContext = strings.TrimSpace(strings.ToLower(text[prefixStart:start]))
	}
	if localCyberPolicyHasAnyTerm(authorizationContext, []string{
		"已获授权", "已经授权", "获得授权", "经授权", "授权范围内", "自有网站", "自己的网站",
		"本地靶场", "测试靶场", "靶机", "ctf", "authorized", "with authorization", "with permission",
		"permission to test", "my own website", "my own server", "local lab", "test lab", "sandbox",
	}) {
		return true
	}

	// Quoted or recorded attack text is data when the surrounding sentence is
	// clearly about detection, policy, or incident review and does not ask to
	// carry out another intrusion afterwards.
	documentary := localCyberPolicyHasAnyTerm(outside, []string{
		"日志", "报告", "文档", "规则", "策略", "关键词", "样例", "示例", "测试用例", "记录", "复盘",
		"检测", "识别", "拦截", "阻止", "防御", "防护", "修复", "缓解", "审计", "分析", "说明", "解释",
		"log", "report", "document", "rule", "policy", "keyword", "example", "test case", "recorded",
		"detect", "identify", "block", "prevent", "defend", "protect", "remediate", "mitigate", "audit", "analyze", "explain",
	})
	if documentary && (promptInjectionHasQuoteAround(local, localStart, localEnd) || localCyberPolicyHasReportingContext(before, after)) &&
		!localCyberPolicyHasIntrusionContinuation(after) {
		return true
	}

	// A direct prohibition or defensive construction immediately governing the
	// matched action is benign. Do not let an earlier negation suppress a later
	// positive clause (for example, "不要攻击；然后入侵该网站").
	if localCyberPolicyHasDefensivePrefix(before) && !localCyberPolicyHasIntrusionContinuation(after) {
		return true
	}
	return false
}

func localCyberPolicyHasReportingContext(before, after string) bool {
	return localCyberPolicyHasAnyTerm(before, []string{
		"日志中", "日志显示", "日志记录", "报告中", "报告显示", "报告记录", "文档中", "规则中", "策略中",
		"请求内容", "请求为", "提示词为", "样例", "示例", "关键词", "检测", "识别", "拦截", "防御", "防护",
		"the log", "log shows", "log records", "the report", "report shows", "documented", "the rule",
		"request says", "prompt says", "detect", "identify", "block", "defend", "protect",
	}) || localCyberPolicyHasAnyTerm(after, []string{
		"的请求", "的日志", "应被拦截", "需要拦截", "用于检测", "用于识别", "作为样例", "作为示例",
		"was blocked", "should be blocked", "for detection", "for identification", "as an example",
	})
}

func localCyberPolicyHasDefensivePrefix(before string) bool {
	before = strings.TrimSpace(before)
	if before == "" || localCyberPolicyPositiveBridge.MatchString(before) {
		return false
	}
	return localCyberPolicyDefensivePrefix.MatchString(before)
}

func localCyberPolicyHasIntrusionContinuation(after string) bool {
	if strings.TrimSpace(after) == "" {
		return false
	}
	return localCyberPolicyIntrusionContinuation.MatchString(after)
}

func localCyberPolicyHasAnyTerm(value string, terms []string) bool {
	value = strings.ToLower(value)
	for _, term := range terms {
		if strings.Contains(value, strings.ToLower(term)) {
			return true
		}
	}
	return false
}
