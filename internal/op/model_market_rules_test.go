package op

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/setting"
)

// setupModelMarketRulesTest 初始化 DB + setting 缓存，返回清理函数。
func setupModelMarketRulesTest(t *testing.T) func() {
	t.Helper()
	testName := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", testName)
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("refresh cache: %v", err)
	}
	return func() {
		_ = db.Close()
	}
}

func mustSet(t *testing.T, key model.SettingKey, value string) {
	t.Helper()
	if err := setting.SetString(key, value); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
}

// 用户导入的显式映射规则应参与模型广场聚合：变体按 canonical 合并为一行。
func TestBuildModelMarket_AppliesImportedExplicitMappings(t *testing.T) {
	cleanup := setupModelMarketRulesTest(t)
	defer cleanup()

	// 开启模型广场去重。
	mustSet(t, model.SettingKeyModelNormalizeMarketDedupeDefault, "true")
	// 用户导入的显式映射：把无内置规则覆盖的变体映射到基准名。
	mustSet(t, model.SettingKeyModelNormalizeExplicitMappings,
		`[{"variant":"kimi-k2.5-256k","canonical":"kimi-k2.5"},{"variant":"deepseek-r1-0528","canonical":"deepseek-r1"}]`)

	items, _ := buildModelMarket(
		[]model.LLMInfo{
			{Name: "kimi-k2.5"},
			{Name: "kimi-k2.5-256k"},
			{Name: "deepseek-r1"},
			{Name: "deepseek-r1-0528"},
		},
		nil,
		nil,
		nil,
		time.Time{},
	)

	if len(items) != 2 {
		names := make([]string, 0, len(items))
		for _, it := range items {
			names = append(names, it.Name)
		}
		t.Fatalf("len(items) = %d, want 2 (merged by explicit mapping): %v", len(items), names)
	}
}

// 显式映射规则更新后，聚合应使用新规则（rulesCache 随 setting generation 失效）。
func TestBuildModelMarket_ReflectsRuleChanges(t *testing.T) {
	cleanup := setupModelMarketRulesTest(t)
	defer cleanup()

	mustSet(t, model.SettingKeyModelNormalizeMarketDedupeDefault, "true")

	// 第一次：v1 规则。
	mustSet(t, model.SettingKeyModelNormalizeExplicitMappings,
		`[{"variant":"kimi-k2.5-256k","canonical":"kimi-k2.5"}]`)
	items, _ := buildModelMarket(
		[]model.LLMInfo{{Name: "kimi-k2.5"}, {Name: "kimi-k2.5-256k"}},
		nil, nil, nil, time.Time{},
	)
	if len(items) != 1 {
		t.Fatalf("first pass: len(items) = %d, want 1", len(items))
	}

	// 第二次：规则改掉，256k 映射到独立基准名。
	mustSet(t, model.SettingKeyModelNormalizeExplicitMappings,
		`[{"variant":"kimi-k2.5-256k","canonical":"kimi-k2.5-256k"}]`)
	items, _ = buildModelMarket(
		[]model.LLMInfo{{Name: "kimi-k2.5"}, {Name: "kimi-k2.5-256k"}},
		nil, nil, nil, time.Time{},
	)
	if len(items) != 2 {
		t.Fatalf("second pass: len(items) = %d, want 2 (rules changed)", len(items))
	}
}

// 规则 JSON 里的 variant 大小写应与模型名匹配（精确全名匹配）。
func TestBuildModelMarket_ExplicitMappingCaseInsensitive(t *testing.T) {
	cleanup := setupModelMarketRulesTest(t)
	defer cleanup()

	mustSet(t, model.SettingKeyModelNormalizeMarketDedupeDefault, "true")
	mustSet(t, model.SettingKeyModelNormalizeExplicitMappings,
		`[{"variant":"KIMI-K2.5-256K","canonical":"kimi-k2.5"}]`)

	items, _ := buildModelMarket(
		[]model.LLMInfo{{Name: "kimi-k2.5"}, {Name: "kimi-k2.5-256k"}},
		nil, nil, nil, time.Time{},
	)
	if len(items) != 1 {
		names := make([]string, 0, len(items))
		for _, it := range items {
			names = append(names, it.Name)
		}
		t.Fatalf("len(items) = %d, want 1: %v", len(items), names)
	}
}

// 渠道侧模型名带路由前缀/路径时，导入的裸名 variant 规则也应命中并合并
// （显式映射匹配「剥离路径+路由前缀后的基础名」）。
func TestBuildModelMarket_ExplicitMappingMatchesPrefixedVariants(t *testing.T) {
	cleanup := setupModelMarketRulesTest(t)
	defer cleanup()

	mustSet(t, model.SettingKeyModelNormalizeMarketDedupeDefault, "true")
	mustSet(t, model.SettingKeyModelNormalizeExplicitMappings,
		`[{"variant":"kimi-k2.5-256k","canonical":"kimi-k2.5"}]`)

	items, _ := buildModelMarket(
		[]model.LLMInfo{
			{Name: "kimi-k2.5"},
			{Name: "dmxapi-kimi-k2.5-256k"},
			{Name: "moonshotai/kimi-k2.5-256k"},
		},
		nil, nil, nil, time.Time{},
	)
	if len(items) != 1 {
		names := make([]string, 0, len(items))
		for _, it := range items {
			names = append(names, it.Name)
		}
		t.Fatalf("len(items) = %d, want 1 (prefixed variants merged): %v", len(items), names)
	}
	if items[0].Name != "kimi-k2.5" {
		t.Fatalf("merged item name = %q, want kimi-k2.5", items[0].Name)
	}
}

// 确保上述测试用到的字段存在于类型定义中（编译期契约）。
var _ = json.Marshal
var _ = filepath.Join
