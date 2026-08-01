package modelnormalize

import (
	"fmt"
	"testing"
)

// 用用户实际导入的规则（从设置页导出的代表性子集）构造回归用例。
func userRules() Rules {
	return Rules{
		RouterPrefixes: []string{
			"DMXAPI-", "DMXAPl-", "gateway-", "agent-", "cc-", "mm-", "coding-",
			"[官B]-", "[官C]-", "[特价C]-", "[特价D]-", "[特价F]-", "[限时福利]-",
			"@cf/-", "Pro/-", "[.*]-",
		},
		FunctionalSuffixes: []string{
			"-cc", "-ssvip", "-vip", "-thinking", "-think", "-zj", "-fast",
			"-get", "-all", "-sk", "-or", "-pt", "-uc",
			"-\\d+k", "-\\d{8}", "-\\d{6}", "-\\d{4}", "-@\\w+", "-(free)", "-:free",
		},
		ExplicitMappings: []ExplicitMapping{
			// 用户按前缀枚举的映射：variant 带渠道前缀。
			{Variant: "[官B]claude-opus-4-6-thinking", Canonical: "claude-opus-4.6"},
			{Variant: "[特价C]claude-opus-4-6", Canonical: "claude-opus-4.6"},
			{Variant: "DMXAPI-g-p-t-5.2", Canonical: "gpt-5.2"},
			// 裸名映射：无前缀变体。
			{Variant: "claude-opus-4-6", Canonical: "claude-opus-4.6"},
			// doubao 日期变体。
			{Variant: "doubao-seed-1-6-flash-250615", Canonical: "doubao-seed-1.6-flash"},
		},
	}
}

// 回归：映射 variant 带渠道前缀时，任意前缀/无前缀的渠道模型名都应命中
// （「规范化匹配」档：variant 与输入都剥路径+前缀+后缀后比较）。
// 用户按前缀枚举映射（[官B]/[特价C]/DMXAPI...），但渠道侧是任意前缀/无前缀，
// 若只按精确/base 匹配会漏，同模型归一到不同名导致去重失效。
func TestNormalizeWithRules_CrossPrefixMappingMatch(t *testing.T) {
	rules := userRules()

	inputs := map[string]string{
		"[官B]claude-opus-4-6-thinking":        "claude-opus-4.6", // 精确命中
		"dmxapi-claude-opus-4-6-thinking":     "claude-opus-4.6", // 其他前缀，规范化命中
		"claude-opus-4-6-thinking":            "claude-opus-4.6", // 无前缀
		"mm-claude-opus-4-6-thinking":         "claude-opus-4.6", // 另一前缀
		"[特价C]claude-opus-4-6":                "claude-opus-4.6", // 精确命中
		"gateway-claude-opus-4-6":             "claude-opus-4.6", // 规范化命中
		"DMXAPI-g-p-t-5.2":                    "gpt-5.2",         // 精确命中
		"dmxapi-g-p-t-5.2":                    "gpt-5.2",         // 大小写不敏感
		"doubao-seed-1-6-flash-250615":        "doubao-seed-1.6-flash",
		"dmxapi-doubao-seed-1-6-flash-250615": "doubao-seed-1.6-flash", // 前缀 + 日期变体
	}
	for input, want := range inputs {
		if got := NormalizeWithRules(input, rules); got != want {
			t.Errorf("NormalizeWithRules(%q) = %q, want %q", input, got, want)
		}
	}
}

// 双向冲突映射自动消解：claude-opus-4-6 → 4.6 与 claude-opus-4.6 → 4-6 互斥，
// 同 dotDashKey 取第一条，同一模型两种命名归一到同一 canonical，
// 带路径/前缀的渠道名也命中（存量数据无需用户手动改即可生效）。
func TestNormalizeWithRules_ResolvesBidirectionalConflicts(t *testing.T) {
	rules := Rules{
		RouterPrefixes: []string{"dmxapi-", "[官B]-"},
		ExplicitMappings: []ExplicitMapping{
			{Variant: "claude-opus-4-6", Canonical: "claude-opus-4.6"},
			{Variant: "claude-opus-4.6", Canonical: "claude-opus-4-6"}, // 反向冲突
		},
	}
	inputs := map[string]string{
		"claude-opus-4-6":           "claude-opus-4.6",
		"claude-opus-4.6":           "claude-opus-4.6",
		"[官B]claude-opus-4-6":       "claude-opus-4.6",
		"anthropic/claude-opus-4.6": "claude-opus-4.6",
	}
	for input, want := range inputs {
		if got := NormalizeWithRules(input, rules); got != want {
			t.Errorf("NormalizeWithRules(%q) = %q, want %q", input, got, want)
		}
	}
}

// 冒号/括号/@ 后缀变体：-:free → :free、-(free) → (free)、-@\w+ → @\w+。
func TestNormalizeWithRules_StripsColonParenAtSuffixes(t *testing.T) {
	rules := Rules{
		FunctionalSuffixes: []string{"-cc", "-:free", "-(free)", "-@\\w+"},
	}
	inputs := map[string]string{
		"deepseek/deepseek-r1:free":  "deepseek-r1",
		"deepseek/deepseek-r1(free)": "deepseek-r1",
		"claude-3-haiku@20240307":    "claude-3-haiku",
	}
	for input, want := range inputs {
		if got := NormalizeWithRules(input, rules); got != want {
			t.Errorf("NormalizeWithRules(%q) = %q, want %q", input, got, want)
		}
	}
}

// 正则日期后缀：-\d{8} 匹配 -20250514。
func TestNormalizeWithRules_StripsRegexDateSuffix(t *testing.T) {
	rules := Rules{
		FunctionalSuffixes: []string{"-\\d{8}", "-cc"},
	}
	inputs := map[string]string{
		"claude-opus-4-20250514":     "claude-opus-4",
		"claude-3-7-sonnet-20250219": "claude-3-7-sonnet",
	}
	for input, want := range inputs {
		if got := NormalizeWithRules(input, rules); got != want {
			t.Errorf("NormalizeWithRules(%q) = %q, want %q", input, got, want)
		}
	}
}

// [官B]- 前缀配置（带尾 -）应匹配 [官B]claude-...（无连字符）渠道名。
func TestNormalizeWithRules_StripsBracketPrefixWithTrailingDash(t *testing.T) {
	rules := Rules{
		RouterPrefixes: []string{"dmxapi-", "[官B]-"},
	}
	inputs := map[string]string{
		"[官B]claude-opus-4-6": "claude-opus-4-6",
		"dmxapi-kimi-k2.5":    "kimi-k2.5",
	}
	for input, want := range inputs {
		if got := NormalizeWithRules(input, rules); got != want {
			t.Errorf("NormalizeWithRules(%q) = %q, want %q", input, got, want)
		}
	}
}

// 4.6→4-6），同一模型两种命名归一到不同名，去重失效。这是规则数据冲突，
// 代码无法自动消解，设置页保存时已新增冲突检测拦截。
func TestDiagnose_BidirectionalConflictsAreDataProblem(t *testing.T) {
	rules := Rules{
		RouterPrefixes: []string{"dmxapi-"},
		ExplicitMappings: []ExplicitMapping{
			{Variant: "claude-opus-4-6", Canonical: "claude-opus-4.6"},
			{Variant: "claude-opus-4.6", Canonical: "claude-opus-4-6"}, // 反向冲突
		},
	}
	a := NormalizeWithRules("claude-opus-4-6", rules)
	b := NormalizeWithRules("claude-opus-4.6", rules)
	fmt.Printf("冲突对: claude-opus-4-6 → %q, claude-opus-4.6 → %q (去重失效，需用户删除反向映射)\n", a, b)
	if a == b {
		t.Logf("意外合并成功: %q", a)
	}
}
