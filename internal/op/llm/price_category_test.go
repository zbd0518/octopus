package llm

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
)

func initPriceCategoryTestDB(t *testing.T) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
}

func TestPriceCategoryMatch(t *testing.T) {
	initPriceCategoryTestDB(t)
	ctx := context.Background()

	// exact 优先于 prefix：低 sort_order 的 exact 先命中。
	rows := []model.ModelPriceCategory{
		{Name: "exact-special", RuleType: string(model.ModelPriceCategoryRuleExact), RuleValue: "gpt-special", LLMPrice: model.LLMPrice{Input: 99, Output: 199}, SortOrder: 1, Enabled: true},
		{Name: "prefix-gpt", RuleType: string(model.ModelPriceCategoryRulePrefix), RuleValue: "gpt", LLMPrice: model.LLMPrice{Input: 5, Output: 15}, SortOrder: 10, Enabled: true},
		{Name: "contains-embed", RuleType: string(model.ModelPriceCategoryRuleContains), RuleValue: "embedding", LLMPrice: model.LLMPrice{Input: 0.1, Output: 0.2}, SortOrder: 2, Enabled: true},
		{Name: "disabled", RuleType: string(model.ModelPriceCategoryRuleContains), RuleValue: "off", LLMPrice: model.LLMPrice{Input: 1, Output: 2}, SortOrder: 0, Enabled: false},
	}

	for _, r := range rows {
		if _, err := CreatePriceCategory(r, ctx); err != nil {
			t.Fatalf("CreatePriceCategory(%s): %v", r.Name, err)
		}
	}

	cases := []struct {
		model string
		want  *model.LLMPrice
	}{
		{"gpt-special", &model.LLMPrice{Input: 99, Output: 199}},       // exact wins over prefix
		{"gpt-4o", &model.LLMPrice{Input: 5, Output: 15}},              // prefix
		{"text-embedding-3", &model.LLMPrice{Input: 0.1, Output: 0.2}}, // contains
		{"off-model", nil}, // disabled category ignored
		{"no-match", nil},
	}

	for _, c := range cases {
		got := PriceCategoryMatch(c.model)
		if c.want == nil {
			if got != nil {
				t.Fatalf("PriceCategoryMatch(%s) = %+v, want nil", c.model, got)
			}
			continue
		}
		if got == nil || *got != *c.want {
			t.Fatalf("PriceCategoryMatch(%s) = %+v, want %+v", c.model, got, c.want)
		}
	}
}

func TestPriceCategoryCRUD(t *testing.T) {
	initPriceCategoryTestDB(t)
	ctx := context.Background()

	created, err := CreatePriceCategory(model.ModelPriceCategory{
		Name:      "My Chat",
		RuleType:  string(model.ModelPriceCategoryRulePrefix),
		RuleValue: "my",
		LLMPrice:  model.LLMPrice{Input: 1, Output: 2, CacheRead: 0.5, CacheWrite: 1},
		SortOrder: 3,
		Enabled:   true,
	}, ctx)
	if err != nil {
		t.Fatalf("CreatePriceCategory: %v", err)
	}
	// name 应转为小写
	if created.Name != "my chat" {
		t.Fatalf("name not lowercased: %q", created.Name)
	}

	rows, err := ListPriceCategories(ctx)
	if err != nil {
		t.Fatalf("ListPriceCategories: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListPriceCategories len = %d, want 1", len(rows))
	}

	// 兜底应命中
	if p := PriceCategoryMatch("my-model"); p == nil || p.Input != 1 {
		t.Fatalf("expected fallback price after create, got %+v", p)
	}

	// update 改价 + 禁用应生效
	created.Enabled = false
	created.Input = 7
	if _, err := UpdatePriceCategory(created, ctx); err != nil {
		t.Fatalf("UpdatePriceCategory: %v", err)
	}
	if p := PriceCategoryMatch("my-model"); p != nil {
		t.Fatalf("disabled category should not match, got %+v", p)
	}

	// delete
	if err := DeletePriceCategory(created.ID, ctx); err != nil {
		t.Fatalf("DeletePriceCategory: %v", err)
	}
	rows, _ = ListPriceCategories(ctx)
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows after delete, got %d", len(rows))
	}
}

func TestPriceCategoryValidation(t *testing.T) {
	initPriceCategoryTestDB(t)
	ctx := context.Background()

	invalidCases := []model.ModelPriceCategory{
		{RuleType: "bad", RuleValue: "x", LLMPrice: model.LLMPrice{Input: 1, Output: 1}},                                                // bad rule_type
		{RuleType: string(model.ModelPriceCategoryRuleExact), RuleValue: " ", LLMPrice: model.LLMPrice{Input: 1, Output: 1}},            // empty rule_value
		{RuleType: string(model.ModelPriceCategoryRuleExact), RuleValue: "x", LLMPrice: model.LLMPrice{Input: -1, Output: 1}},           // negative price
		{Name: " ", RuleType: string(model.ModelPriceCategoryRuleExact), RuleValue: "x", LLMPrice: model.LLMPrice{Input: 1, Output: 1}}, // empty name
	}
	for i, c := range invalidCases {
		if _, err := CreatePriceCategory(c, ctx); err == nil {
			t.Fatalf("case %d: expected error, got nil", i)
		}
	}
}
