package modelnormalize

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// loadUserTestData 加载 testdata 中的用户规则与真实渠道模型名。
func loadUserTestData(t *testing.T) (Rules, []string) {
	t.Helper()
	ruleRaw, err := os.ReadFile("testdata/user_rules.json")
	if err != nil {
		t.Fatalf("read user_rules.json: %v", err)
	}
	var rules struct {
		RouterPrefixes     []string          `json:"router_prefixes"`
		FunctionalSuffixes []string          `json:"functional_suffixes"`
		ExplicitMappings   []ExplicitMapping `json:"explicit_mappings"`
	}
	if err := json.Unmarshal(ruleRaw, &rules); err != nil {
		t.Fatalf("parse user_rules.json: %v", err)
	}
	nameRaw, err := os.ReadFile("testdata/channel_names.json")
	if err != nil {
		t.Fatalf("read channel_names.json: %v", err)
	}
	var names []string
	if err := json.Unmarshal(nameRaw, &names); err != nil {
		t.Fatalf("parse channel_names.json: %v", err)
	}
	// 预构建显式映射缓存，避免每个模型名都重建（生产路径由 CurrentRules 缓存）。
	rulesObj := Rules{
		RouterPrefixes:     rules.RouterPrefixes,
		FunctionalSuffixes: rules.FunctionalSuffixes,
		ExplicitMappings:   rules.ExplicitMappings,
	}
	rulesObj.explicitByKey = buildExplicitByKey(rulesObj.ExplicitMappings, rulesObj)
	return rulesObj, names
} // TestRealData_NormalizeAllNames 用真实渠道模型名跑归一化，输出归一名分布。
// 只打印诊断信息，不断言（数据质量问题由用户侧修复）。
func TestRealData_NormalizeAllNames(t *testing.T) {
	rules, names := loadUserTestData(t)

	canonToOriginals := make(map[string][]string)
	for _, name := range names {
		canon := NormalizeWithRules(name, rules)
		canonToOriginals[canon] = append(canonToOriginals[canon], name)
	}

	// 归一名数量 vs 原始名数量（去重生效指标）。
	fmt.Printf("原始模型名: %d, 归一名: %d\n", len(names), len(canonToOriginals))

	// 输出归并成功的簇（>1 个原始名）。
	merged := 0
	for _, originals := range canonToOriginals {
		if len(originals) > 1 {
			merged += len(originals) - 1
		}
	}
	fmt.Printf("成功归并的额外变体数: %d\n", merged)

	// 输出重点模型族：claude 系列的归一名分布（找去重失效点）。
	fmt.Println("\n=== claude 系列归一名分布 ===")
	var claudeCanons []string
	for canon := range canonToOriginals {
		if strings.Contains(canon, "claude") {
			claudeCanons = append(claudeCanons, canon)
		}
	}
	sort.Strings(claudeCanons)
	for _, canon := range claudeCanons {
		fmt.Printf("%-30s ← %v\n", canon, canonToOriginals[canon])
	}
}

// TestRealData_FreeVariants 分析 free 变体的归并情况。
func TestRealData_FreeVariants(t *testing.T) {
	rules, names := loadUserTestData(t)

	// 找出所有含 free 的原始名及其归一名。
	fmt.Println("=== free 变体 ===")
	for _, name := range names {
		lower := strings.ToLower(name)
		if strings.Contains(lower, "free") {
			canon := NormalizeWithRules(name, rules)
			fmt.Printf("%-50s → %s\n", name, canon)
		}
	}
}

// TestRealData_DateSuffixVariants 分析日期后缀变体的归并情况（-\d{8} 正则是否被字面匹配）。
func TestRealData_DateSuffixVariants(t *testing.T) {
	rules, names := loadUserTestData(t)

	fmt.Println("=== 日期变体（未走显式映射的）===")
	count := 0
	for _, name := range names {
		// 原始名含 6/8 位日期但不在显式映射里（靠正则后缀剥离才能合并）。
		canon := NormalizeWithRules(name, rules)
		hasDigits := strings.Contains(canon, "2025") || strings.Contains(canon, "2026") || strings.Contains(canon, "250") || strings.Contains(canon, "260") || strings.Contains(canon, "240")
		if hasDigits && count < 30 {
			fmt.Printf("%-50s → %s\n", name, canon)
			count++
		}
	}
	fmt.Printf("（共展示 %d 条含日期数字的归一名）\n", count)
}
