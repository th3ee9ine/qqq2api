package service

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationKeywordMatcherMatchesLegacyBehavior(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		keywords []string
	}{
		{name: "miss", text: "clean prompt", keywords: []string{"blocked", "secret"}},
		{name: "case insensitive", text: "contains SECRET value", keywords: []string{"secret"}},
		{name: "configured order wins", text: "early appears before later", keywords: []string{"later", "early"}},
		{name: "overlap uses configured order", text: "abc", keywords: []string{"bc", "abc"}},
		{name: "unicode", text: "这里包含敏感词和世界", keywords: []string{"世界", "敏感词"}},
		{name: "duplicates", text: "duplicate", keywords: []string{"duplicate", "DUPLICATE"}},
		{name: "empty entries", text: "blocked", keywords: []string{"", "blocked"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantKeyword, wantHit := matchBlockedKeyword(tt.text, tt.keywords)
			gotKeyword, gotHit := newContentModerationKeywordMatcher(tt.keywords).Match(tt.text)
			require.Equal(t, wantHit, gotHit)
			require.Equal(t, wantKeyword, gotKeyword)
		})
	}
}

func TestContentModerationKeywordMatcherRandomizedParity(t *testing.T) {
	rng := rand.New(rand.NewSource(20260714))
	const alphabet = "abcXYZ"
	for iteration := 0; iteration < 1000; iteration++ {
		keywords := make([]string, 1+rng.Intn(30))
		for index := range keywords {
			length := 1 + rng.Intn(8)
			var value strings.Builder
			for range length {
				_ = value.WriteByte(alphabet[rng.Intn(len(alphabet))])
			}
			keywords[index] = value.String()
		}
		var text strings.Builder
		for range 20 + rng.Intn(100) {
			_ = text.WriteByte(alphabet[rng.Intn(len(alphabet))])
		}

		wantKeyword, wantHit := matchBlockedKeyword(text.String(), keywords)
		gotKeyword, gotHit := newContentModerationKeywordMatcher(keywords).Match(text.String())
		require.Equal(t, wantHit, gotHit, "iteration %d", iteration)
		require.Equal(t, wantKeyword, gotKeyword, "iteration %d", iteration)
	}
}

func TestContentModerationKeywordMatcherContextAwareAllowsStructuredMentions(t *testing.T) {
	matcher := newContentModerationKeywordMatcher([]string{"群控"})
	for _, text := range []string{
		"A10 | 控制系统 | □有BA/群控 □有但不用",
		"控制方式：BA/群控界面",
		"接入方式：远程/群控",
		"禁止执行群控脚本",
		"请解释群控是什么",
	} {
		keyword, hit := matcher.MatchContextAware(text)
		if hit {
			t.Fatalf("context-aware matcher blocked informational text %q with keyword %q", text, keyword)
		}
	}
}

func TestContentModerationKeywordMatcherContextAwareBlocksActionableMentions(t *testing.T) {
	matcher := newContentModerationKeywordMatcher([]string{"群控"})
	for _, text := range []string{
		"请提供群控脚本",
		"帮我搭建群控服务器",
		"下载并运行群控程序",
	} {
		keyword, hit := matcher.MatchContextAware(text)
		if !hit || keyword != "群控" {
			t.Fatalf("context-aware matcher missed actionable text %q (keyword=%q hit=%v)", text, keyword, hit)
		}
	}
}

func TestContentModerationKeywordMatcherContextAwareCrossDelimiterAction(t *testing.T) {
	matcher := newContentModerationKeywordMatcher([]string{"群控"})
	cases := []struct {
		name string
		text string
		hit  bool
	}{
		{name: "semicolon request supplies omitted object", text: "群控；请提供脚本", hit: true},
		{name: "sentence request supplies omitted object", text: "群控。请提供脚本", hit: true},
		{name: "comma continuation refers to keyword", text: "群控, then run it", hit: true},
		{name: "next clause negation", text: "群控；请勿提供脚本", hit: false},
		{name: "report label remains documentary", text: "报告：群控；请提供脚本", hit: false},
		{name: "table label remains documentary", text: "表格：群控；请提供脚本", hit: false},
		{name: "defensive artifact remains documentary", text: "群控；请提供检测脚本", hit: false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			keyword, hit := matcher.MatchContextAware(tt.text)
			if hit != tt.hit {
				t.Fatalf("cross-delimiter hit=%v, want %v for %q (keyword=%q)", hit, tt.hit, tt.text, keyword)
			}
		})
	}
}

func TestContentModerationKeywordMatcherContextAwareKeepsLongPhraseAndDefensiveContext(t *testing.T) {
	matcher := newContentModerationKeywordMatcher([]string{"SQL注入"})
	if keyword, hit := matcher.MatchContextAware("请编写SQL注入利用代码"); !hit || keyword != "SQL注入" {
		t.Fatalf("expected actionable long phrase to block, got keyword=%q hit=%v", keyword, hit)
	}
	if keyword, hit := matcher.MatchContextAware("如何防御SQL注入并完善审计规则"); hit {
		t.Fatalf("defensive explanation should not hard block, got keyword=%q", keyword)
	}
}

func TestContentModerationKeywordMatcherContextAwareOperationalAndDefensiveBoundaries(t *testing.T) {
	tests := []struct {
		name string
		text string
		hit  bool
	}{
		{name: "download sensitive tool", text: "安全地下载远控工具", hit: true},
		{name: "install sensitive program", text: "请安装群控程序", hit: true},
		{name: "run sensitive artifact", text: "请运行远控木马", hit: true},
		{name: "detection script is defensive", text: "请编写木马检测脚本", hit: false},
		{name: "provided detection script is defensive", text: "请提供木马检测脚本", hit: false},
		{name: "provided detection rules are defensive", text: "请提供SQL注入检测规则", hit: false},
		{name: "detection purpose is defensive", text: "请编写木马脚本用于安全检测", hit: false},
		{name: "provide artifact remains actionable", text: "请提供群控脚本用于安全检测", hit: true},
		{name: "training detection is defensive", text: "安全培训：如何检测 vssadmin delete shadows", hit: false},
		{name: "protection plan is defensive", text: "DDoS防护方案", hit: false},
		{name: "punctuation does not leak negation", text: "禁止执行。\n请提供群控脚本", hit: true},
		{name: "table then action", text: "□有BA/群控；请提供群控脚本", hit: true},
		{name: "build offensive network", text: "请解释如何构建僵尸网络", hit: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher := newContentModerationKeywordMatcher([]string{"远控", "群控", "木马", "僵尸网络", "DDoS", "vssadmin delete shadows"})
			_, got := matcher.MatchContextAware(tt.text)
			if got != tt.hit {
				t.Fatalf("context-aware hit=%v, want %v for %q", got, tt.hit, tt.text)
			}
		})
	}
}

func TestContentModerationKeywordMatcherContextAwareUTF8AndMultipleOccurrences(t *testing.T) {
	matcher := newContentModerationKeywordMatcher([]string{"群控"})
	for _, tt := range []struct {
		name string
		text string
		hit  bool
	}{
		{name: "table occurrence only", text: "A10 | 控制系统 | □有BA/群控 □有但不用", hit: false},
		{name: "later actionable occurrence", text: "A10 | 控制系统 | □有BA/群控；请提供群控脚本", hit: true},
		{name: "utf8 punctuation", text: "说明：群控。请总结", hit: false},
		{name: "standalone short term", text: "群控", hit: false},
		{name: "sensitive object co-occurrence", text: "群控脚本", hit: true},
		{name: "field then actionable occurrence", text: "字段：群控；请提供群控脚本", hit: true},
		{name: "report then actionable occurrence", text: "安全报告：群控；请提供群控脚本", hit: true},
		{name: "sentence then actionable occurrence", text: "报告：群控。请提供群控脚本", hit: true},
		{name: "table comma then actionable occurrence", text: "表格：群控, 请提供群控脚本", hit: true},
		{name: "reference then actionable occurrence", text: "引用：群控；请提供群控脚本", hit: true},
		{name: "newline then actionable occurrence", text: "字段：群控\n请提供群控脚本", hit: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, got := matcher.MatchContextAware(tt.text)
			if got != tt.hit {
				t.Fatalf("context-aware hit=%v, want %v for %q", got, tt.hit, tt.text)
			}
		})
	}
}

func TestContentModerationKeywordMatcherContextAwareCanonicalizesInput(t *testing.T) {
	for _, tt := range []struct {
		name    string
		keyword string
		text    string
	}{
		{name: "zero width cjk", keyword: "群控", text: "请提供群\u200b控脚本"},
		{name: "combining mark ascii", keyword: "群控", text: "请提供群控\u0301脚本"},
		{name: "full width ascii", keyword: "DDoS", text: "请执行ＤＤＯＳ攻击"},
		{name: "full width configured keyword", keyword: "ＤＤｏＳ", text: "请执行DDoS攻击"},
		{name: "padded configured keyword", keyword: "  DDoS  ", text: "请执行DDoS攻击"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			keyword, hit := newContentModerationKeywordMatcher([]string{tt.keyword}).MatchContextAware(tt.text)
			if !hit || keyword != tt.keyword {
				t.Fatalf("canonical matcher missed %q in %q (keyword=%q hit=%v)", tt.keyword, tt.text, keyword, hit)
			}
		})
	}
}

func TestContentModerationKeywordMatcherContextAwareIntentBoundaries(t *testing.T) {
	cases := []struct {
		name    string
		keyword string
		text    string
		hit     bool
	}{
		{name: "defensive label", keyword: "SQL注入", text: "防御：SQL注入", hit: false},
		{name: "detection label", keyword: "SQL注入", text: "检测：SQL注入", hit: false},
		{name: "report label", keyword: "SQL注入", text: "安全报告：SQL注入", hit: false},
		{name: "label with explicit exploit", keyword: "SQL注入", text: "请提供：SQL注入利用代码", hit: true},
		{name: "definition short", keyword: "群控", text: "群控系统是什么", hit: false},
		{name: "definition long", keyword: "群控系统", text: "群控系统是什么", hit: false},
		{name: "privilege target", keyword: "提权", text: "请提权到root", hit: true},
		{name: "privilege compound target", keyword: "提权到", text: "请提权到root", hit: true},
		{name: "explain remote control", keyword: "远控", text: "请解释远控工具的原理", hit: false},
		{name: "explain attack method", keyword: "远控", text: "请说明远控攻击方式", hit: false},
		{name: "explain trojan principle", keyword: "木马", text: "请说明木马攻击原理", hit: false},
		{name: "explain trojan", keyword: "木马", text: "请解释木马原理", hit: false},
		{name: "english negated long phrase", keyword: "SQL注入", text: "not a SQL注入 request", hit: false},
		{name: "negation does not swallow positive continuation", keyword: "群控", text: "禁止群控并提供群控脚本", hit: true},
		{name: "negated defensive continuation", keyword: "群控", text: "禁止群控并提供检测脚本", hit: false},
		{name: "english positive continuation", keyword: "SQL注入", text: "not SQL注入 but exploit code", hit: true},
		{name: "positive construction before later negation", keyword: "群控", text: "请提供但不要使用群控脚本", hit: true},
		{name: "action itself negated", keyword: "群控", text: "请不要提供群控脚本", hit: false},
		{name: "keyword inventory overlap", keyword: "简历库", text: "关键词：简历库出售", hit: false},
		{name: "long inventory overlap", keyword: "简历库出售", text: "关键词：简历库出售", hit: false},
		{name: "explicit implementation", keyword: "SQL注入", text: "请实施SQL注入", hit: true},
		{name: "explicit exploitation", keyword: "SQL注入", text: "请利用SQL注入", hit: true},
		{name: "negated trojan", keyword: "木马", text: "禁止执行勒索木马", hit: false},
		{name: "negated remote trojan", keyword: "木马", text: "禁止执行远控木马", hit: false},
		{name: "negated privilege", keyword: "提权", text: "禁止执行内核提权", hit: false},
		{name: "negated shell sale", keyword: "shell出售", text: "禁止执行webshell出售", hit: false},
		{name: "negated bypass", keyword: "过EDR", text: "禁止执行绕过EDR", hit: false},
		{name: "quoted reference", keyword: "SQL注入", text: "引用：“SQL注入”", hit: false},
		{name: "quoted defensive example", keyword: "SQL注入", text: "引用：“请执行SQL注入攻击”", hit: false},
		{name: "naked quoted imperative", keyword: "SQL注入", text: "“请执行SQL注入攻击”", hit: true},
		{name: "quoted request", keyword: "SQL注入", text: "请提供“SQL注入利用代码”", hit: true},
		{name: "quoted report action", keyword: "SQL注入", text: "报告：“SQL注入”并执行", hit: true},
		{name: "training detection", keyword: "SQL注入", text: "培训示例：如何检测 SQL注入", hit: false},
		{name: "training ransomware detection", keyword: "勒索病毒", text: "培训示例：如何检测 勒索病毒", hit: false},
		{name: "training overlapping trojan", keyword: "木马", text: "培训示例：如何检测 勒索木马", hit: false},
		{name: "training overlapping evasion", keyword: "免杀", text: "培训示例：如何检测 DLL劫持免杀", hit: false},
		{name: "training overlapping privilege", keyword: "提权", text: "培训示例：如何检测 令牌窃取提权", hit: false},
		{name: "report overlap resume", keyword: "简历库", text: "报告：简历库出售", hit: false},
		{name: "report overlap bot", keyword: "肉鸡", text: "报告：肉鸡出售", hit: false},
		{name: "explain overlap evasion", keyword: "免杀", text: "请解释 DLL劫持免杀", hit: false},
		{name: "explain overlap privilege", keyword: "提权", text: "请解释 令牌窃取提权", hit: false},
		{name: "explain overlap ddos", keyword: "DDoS", text: "请解释 DDOS攻击", hit: false},
		{name: "provide Chinese definition", keyword: "群控", text: "请提供群控的定义", hit: false},
		{name: "provide Chinese term explanation", keyword: "群控", text: "请提供群控术语解释", hit: false},
		{name: "provide Chinese report", keyword: "群控", text: "请提供群控相关报告", hit: false},
		{name: "provide Chinese document", keyword: "群控", text: "请提供关于群控的文档", hit: false},
		{name: "provide English definition", keyword: "群控", text: "please provide a definition of 群控", hit: false},
		{name: "provide English report", keyword: "群控", text: "provide a report on 群控", hit: false},
		{name: "provide English document", keyword: "群控", text: "please provide documentation about 群控", hit: false},
		{name: "documentary tail then execute", keyword: "群控", text: "provide a report on 群控, then execute it", hit: true},
		{name: "documentary tail then run", keyword: "群控", text: "请提供群控的定义，然后运行它", hit: true},
		{name: "documentary tail then transact", keyword: "群控", text: "provide a report on 群控 and sell it", hit: true},
		{name: "documentary tail then attack", keyword: "群控", text: "provide a report on 群控 and attack target", hit: true},
		{name: "Chinese documentary tail then transact", keyword: "群控", text: "请提供群控的报告并购买服务", hit: true},
		{name: "documentary punctuation then transact", keyword: "群控", text: "provide a report on 群控; then sell it", hit: true},
		{name: "documentary punctuation then attack", keyword: "群控", text: "provide a report on 群控; then attack target", hit: true},
		{name: "documentary punctuation bare transact", keyword: "群控", text: "provide a report on 群控; sell it", hit: true},
		{name: "documentary punctuation bare attack", keyword: "群控", text: "provide a report on 群控; attack target", hit: true},
		{name: "attack noun in documentary report", keyword: "群控", text: "provide a report on 群控攻击", hit: false},
		{name: "transaction noun in documentary report", keyword: "群控", text: "provide a report on 群控出售", hit: false},
		{name: "script remains actionable", keyword: "群控", text: "provide 群控 script", hit: true},
		{name: "code remains actionable", keyword: "群控", text: "provide 群控 code", hit: true},
		{name: "transaction remains actionable", keyword: "群控", text: "请购买群控服务", hit: true},
		{name: "attack artifact remains actionable", keyword: "群控", text: "请提供群控攻击代码", hit: true},
		{name: "definition overlap", keyword: "勒索木马", text: "定义：勒索木马", hit: false},
		{name: "table then actionable label", keyword: "群控", text: "安全报告：群控；请提供群控脚本", hit: true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			keyword, hit := newContentModerationKeywordMatcher([]string{tt.keyword}).MatchContextAware(tt.text)
			if hit != tt.hit {
				t.Fatalf("context-aware hit=%v, want %v for %q (keyword=%q)", hit, tt.hit, tt.text, keyword)
			}
		})
	}
}

func TestContentModerationKeywordMatcherKeepsUnknownShortTokensBlocking(t *testing.T) {
	matcher := newContentModerationKeywordMatcher([]string{"xy"})
	if keyword, hit := matcher.MatchContextAware("xy"); !hit || keyword != "xy" {
		t.Fatalf("custom short token should retain hard-block semantics (keyword=%q hit=%v)", keyword, hit)
	}
}

func TestContentModerationKeywordMatcherContextAwareOverlappingPhrase(t *testing.T) {
	matcher := newContentModerationKeywordMatcher([]string{"简历库", "简历库出售", "肉鸡", "肉鸡出售"})
	for _, text := range []string{
		"培训示例：如何检测 简历库出售",
		"本文讨论肉鸡出售的定义、检测与防御",
		"引用：“简历库出售”",
	} {
		if keyword, hit := matcher.MatchContextAware(text); hit {
			t.Fatalf("benign overlapping phrase %q was blocked by %q", text, keyword)
		}
	}
	for _, text := range []string{
		"请提供简历库出售脚本",
		"请购买肉鸡出售服务",
	} {
		if keyword, hit := matcher.MatchContextAware(text); !hit {
			t.Fatalf("actionable overlapping phrase %q was not blocked (keyword=%q)", text, keyword)
		}
	}
}

func TestContentModerationRuntimeSnapshotUsesContextAwareFallback(t *testing.T) {
	config := defaultContentModerationConfig()
	config.BlockedKeywords = []string{"群控"}

	withMatcher := &contentModerationRuntimeSnapshot{
		config:         config,
		keywordMatcher: newContentModerationKeywordMatcher(config.BlockedKeywords),
	}
	withoutMatcher := &contentModerationRuntimeSnapshot{config: config}

	if keyword, hit := withMatcher.matchBlockedKeyword("控制系统 | □有BA/群控"); hit {
		t.Fatalf("runtime AC matcher blocked table mention with keyword %q", keyword)
	}
	if keyword, hit := withoutMatcher.matchBlockedKeyword("请提供群控脚本"); !hit || keyword != "群控" {
		t.Fatalf("runtime fallback missed actionable mention (keyword=%q hit=%v)", keyword, hit)
	}
}
