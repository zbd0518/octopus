package helper

import (
	"context"
	"testing"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/llm"
)

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
