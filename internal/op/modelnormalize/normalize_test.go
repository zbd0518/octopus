package modelnormalize

import "testing"

func TestNormalizeWithRules_UsesBuiltinRules(t *testing.T) {
	rules := Rules{}
	tests := map[string]string{
		"kimi-k2.5":                "kimi-k2.5",
		"@cf/moonshotai/kimi-k2.5": "kimi-k2.5",
		"dmxapi-kimi-k2.5":         "kimi-k2.5",
		"moonshotai/kimi-k2.5":     "kimi-k2.5",
		"agent/kimi-k2.5":          "kimi-k2.5",
		"kimi-k2.5-cc":             "kimi-k2.5",
		"Kimi-K2.5-CC":             "kimi-k2.5",
		"kimi-k2.5-preview-fast":   "kimi-k2.5",
	}

	for input, want := range tests {
		if got := NormalizeWithRules(input, rules); got != want {
			t.Fatalf("NormalizeWithRules(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeWithRules_ExplicitMappingTakesPrecedence(t *testing.T) {
	rules := Rules{
		ExplicitMappings: []ExplicitMapping{{Variant: "dmxapi-kimi-k2.5-cc", Canonical: "kimi-k2.5-routing"}},
	}

	if got := NormalizeWithRules("DMXAPI-KIMI-K2.5-CC", rules); got != "kimi-k2.5-routing" {
		t.Fatalf("NormalizeWithRules explicit = %q, want kimi-k2.5-routing", got)
	}
}

// 显式映射应匹配「剥离路径 + 路由前缀后的基础名」：用户导入裸名 variant，
// 渠道侧模型名带前缀/路径时也能命中（dmxapi-kimi-k2.5-256k → kimi-k2.5-256k）。
func TestNormalizeWithRules_ExplicitMappingMatchesStrippedBase(t *testing.T) {
	rules := Rules{
		ExplicitMappings: []ExplicitMapping{{Variant: "kimi-k2.5-256k", Canonical: "kimi-k2.5"}},
	}

	tests := map[string]string{
		"kimi-k2.5-256k":            "kimi-k2.5", // 裸名精确匹配
		"dmxapi-kimi-k2.5-256k":     "kimi-k2.5", // 路由前缀剥离后命中
		"moonshotai/kimi-k2.5-256k": "kimi-k2.5", // 路径剥离后命中
		"@cf/org/kimi-k2.5-256k":    "kimi-k2.5", // 多级路径
	}
	for input, want := range tests {
		if got := NormalizeWithRules(input, rules); got != want {
			t.Fatalf("NormalizeWithRules(%q) = %q, want %q", input, got, want)
		}
	}
}

// 显式映射命中后不误伤：带前缀但基础名不同的变体不应被错误合并。
func TestNormalizeWithRules_ExplicitMappingDoesNotOverMatch(t *testing.T) {
	rules := Rules{
		ExplicitMappings: []ExplicitMapping{{Variant: "kimi-k2.5-256k", Canonical: "kimi-k2.5"}},
	}
	// 基础名是 kimi-k2.5（非 256k 变体），不应命中显式映射；走内置规则应保持独立。
	if got := NormalizeWithRules("dmxapi-kimi-k2.5", rules); got != "kimi-k2.5" {
		t.Fatalf("NormalizeWithRules(dmxapi-kimi-k2.5) = %q, want kimi-k2.5", got)
	}
	// 另一个模型的 256k 变体不应被映射到 kimi。
	if got := NormalizeWithRules("dmxapi-gpt-4o-256k", rules); got == "kimi-k2.5" {
		t.Fatal("gpt-4o-256k 被错误映射到 kimi-k2.5")
	}
}

func TestNormalizeWithRules_RuntimeRulesOverrideBuiltinPrefixesAndSuffixes(t *testing.T) {
	rules := Rules{
		RouterPrefixes:     []string{"custom-"},
		FunctionalSuffixes: []string{"-route"},
	}

	if got := NormalizeWithRules("dmxapi-kimi-k2.5-cc", rules); got != "dmxapi-kimi-k2.5-cc" {
		t.Fatalf("NormalizeWithRules with overridden rules = %q, want original lower-case", got)
	}
	if got := NormalizeWithRules("custom-kimi-k2.5-route", rules); got != "kimi-k2.5" {
		t.Fatalf("NormalizeWithRules custom = %q, want kimi-k2.5", got)
	}
}
