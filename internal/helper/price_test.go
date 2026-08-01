package helper

import (
	"context"
	"testing"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/llm"
)

// TestLLMPriceRefreshExistingModels_PreservesManualPrices 验证：手动设置过价格
// 的模型（price_manual=true，经 llm.Create/Update 创建/编辑）不参与同步刷新，
// 即使同步源未命中也不会被写 0 覆盖。
func TestLLMPriceRefreshExistingModels_PreservesManualPrices(t *testing.T) {
	setupHelperDB(t)

	llmCache := llm.GetCache()
	oldLLMs := llmCache.GetAll()
	llmCache.Clear()
	defer func() {
		llmCache.Clear()
		for k, v := range oldLLMs {
			llmCache.Set(k, v)
		}
	}()

	ctx := context.Background()
	// 手动创建：价格 123，不在任何同步源中。
	manual := model.LLMInfo{Name: "my-manual-model-xyz", LLMPrice: model.LLMPrice{Input: 123, Output: 456, CacheRead: 0.1, CacheWrite: 0.2}}
	if err := llm.Create(manual, ctx); err != nil {
		t.Fatalf("llm.Create(manual) error = %v", err)
	}
	// 同步模型：不在同步源，旧值非 0，刷新后应被写 0。
	if err := llm.BatchCreate([]model.LLMInfo{{Name: "sync-model-xyz", LLMPrice: model.LLMPrice{Input: 7, Output: 7, CacheRead: 7, CacheWrite: 7}}}, ctx); err != nil {
		t.Fatalf("llm.BatchCreate(sync) error = %v", err)
	}

	if err := LLMPriceRefreshExistingModels(ctx); err != nil {
		t.Fatalf("LLMPriceRefreshExistingModels() error = %v", err)
	}

	// 手动模型价格保留。
	got, err := llm.Get("my-manual-model-xyz")
	if err != nil {
		t.Fatalf("llm.Get(manual) error = %v", err)
	}
	if got != manual.LLMPrice {
		t.Fatalf("manual price = %+v, want %+v (preserved)", got, manual.LLMPrice)
	}
	// 同步模型被写 0。
	got, err = llm.Get("sync-model-xyz")
	if err != nil {
		t.Fatalf("llm.Get(sync) error = %v", err)
	}
	if got != (model.LLMPrice{}) {
		t.Fatalf("sync model price = %+v, want zero (overwritten)", got)
	}
}

// TestLLMPriceDeleteFromDBWithNoPrice_SkipsManualModels 验证：手动创建的模型
// （price_manual=true，即使价格为 0）不会被"删 0 价格模型"任务删除。
func TestLLMPriceDeleteFromDBWithNoPrice_SkipsManualModels(t *testing.T) {
	setupHelperDB(t)

	llmCache := llm.GetCache()
	oldLLMs := llmCache.GetAll()
	llmCache.Clear()
	defer func() {
		llmCache.Clear()
		for k, v := range oldLLMs {
			llmCache.Set(k, v)
		}
	}()

	ctx := context.Background()
	// 手动创建 0 价模型（用户明确创建，即使没填价格也不应被自动删除）。
	if err := llm.Create(model.LLMInfo{Name: "manual-zero-model-xyz"}, ctx); err != nil {
		t.Fatalf("llm.Create(manual-zero) error = %v", err)
	}
	// 同步 0 价模型：应被删除。
	if err := llm.BatchCreate([]model.LLMInfo{{Name: "sync-zero-model-xyz"}}, ctx); err != nil {
		t.Fatalf("llm.BatchCreate(sync-zero) error = %v", err)
	}

	if err := LLMPriceDeleteFromDBWithNoPrice([]string{"manual-zero-model-xyz", "sync-zero-model-xyz"}, ctx); err != nil {
		t.Fatalf("LLMPriceDeleteFromDBWithNoPrice() error = %v", err)
	}

	// 手动模型仍存在。
	if _, err := llm.Get("manual-zero-model-xyz"); err != nil {
		t.Fatalf("manual-zero model deleted, want preserved: %v", err)
	}
	// 同步模型已删除。
	if _, err := llm.Get("sync-zero-model-xyz"); err == nil {
		t.Fatal("sync-zero model still exists, want deleted")
	}
}

// TestLLMPriceRefreshExistingModels_FallbackToPresetThenZero 验证"同步价格"刷新
// 已有模型时的价格解析顺序：
//  1. 外部价格文件命中 → 用外部价格
//  2. 外部未命中 → 回落托底价格（presets_manual.go，deepseek-v4-flash 属托底条目）
//  3. 均未命中 → 写 0
//
// 依赖真实 presets_manual.go 内容（与 price 包 TestDeepSeekV4PresetPrices 一致）。
func TestLLMPriceRefreshExistingModels_FallbackToPresetThenZero(t *testing.T) {
	setupHelperDB(t)

	llmCache := llm.GetCache()
	oldLLMs := llmCache.GetAll()
	llmCache.Clear()
	defer func() {
		llmCache.Clear()
		for k, v := range oldLLMs {
			llmCache.Set(k, v)
		}
	}()

	// deepseek-v4-flash：托底价格存在（presets_manual.go），旧值故意设为 0
	// 以便断言刷新后写入托底价。
	llmCache.Set("deepseek-v4-flash", model.LLMPrice{})
	// 完全未知的模型：外部与托底均未命中，刷新后应写 0。
	llmCache.Set("totally-unknown-model-xyz", model.LLMPrice{Input: 9, Output: 9, CacheRead: 9, CacheWrite: 9})

	if err := LLMPriceRefreshExistingModels(context.Background()); err != nil {
		t.Fatalf("LLMPriceRefreshExistingModels() error = %v", err)
	}

	// 托底命中：deepseek-v4-flash 应被写入 presets_manual.go 中的价格。
	got, err := llm.Get("deepseek-v4-flash")
	if err != nil {
		t.Fatalf("llm.Get(deepseek-v4-flash) error = %v", err)
	}
	want := model.LLMPrice{Input: 0.14, Output: 0.28, CacheRead: 0.0028, CacheWrite: 0}
	if got != want {
		t.Fatalf("deepseek-v4-flash price = %+v, want preset %+v", got, want)
	}

	// 均未命中：应写 0。
	got, err = llm.Get("totally-unknown-model-xyz")
	if err != nil {
		t.Fatalf("llm.Get(totally-unknown-model-xyz) error = %v", err)
	}
	if got != (model.LLMPrice{}) {
		t.Fatalf("totally-unknown-model-xyz price = %+v, want zero", got)
	}
}
