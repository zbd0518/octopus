package price

import (
	"path/filepath"
	"testing"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/llm"
)

// TestGetLLMPriceCategoryPriority 验证分类表兜底优先于内置整词子串兜底：
// llmPrice 里有 "gpt-4o"，而模型 "my-gpt-4o-extra" 会命中 "gpt-4o" 的子串兜底，
// 但分类表里有一条 prefix "my-gpt-4o" 的分类，分类应胜出。
func TestGetLLMPriceCategoryPriority(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "test.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// 预置分类：prefix "my-" 兜底价。
	if _, err := llm.CreatePriceCategory(model.ModelPriceCategory{
		Name:      "my-models",
		RuleType:  string(model.ModelPriceCategoryRulePrefix),
		RuleValue: "my-",
		LLMPrice:  model.LLMPrice{Input: 42, Output: 84},
		SortOrder: 1,
		Enabled:   true,
	}, t.Context()); err != nil {
		t.Fatal(err)
	}

	restore := setPricesForTest(map[string]model.LLMPrice{
		"gpt-4o": {Input: 5, Output: 15}, // matchFallbackPrice 会对 "my-gpt-4o" 命中
	})
	t.Cleanup(restore)

	// llm.Get 未命中（DB 无 my-gpt-4o），分类应优先于 matchFallbackPrice。
	got := GetLLMPrice("my-gpt-4o")
	if got == nil {
		t.Fatal("GetLLMPrice(my-gpt-4o) = nil, want category price")
	}
	if got.Input != 42 {
		t.Fatalf("GetLLMPrice(my-gpt-4o) Input = %v, want category 42 (whole-word fallback would give 5)", got.Input)
	}
}
