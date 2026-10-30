package op

import (
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/setting"
)

func TestBuildModelMarket_AggregatesChannelsKeysAndStats(t *testing.T) {
	items, summary := buildModelMarket(
		[]model.LLMInfo{
			{
				Name: "gpt-5.2",
				LLMPrice: model.LLMPrice{
					Input:      1,
					Output:     2,
					CacheRead:  0.1,
					CacheWrite: 0.2,
				},
			},
		},
		[]model.LLMChannel{
			{Name: "gpt-5.2", ChannelID: 1, ChannelName: "NMapi", Enabled: true},
			{Name: "gpt-5.2", ChannelID: 2, ChannelName: "Ygxz", Enabled: false},
		},
		map[int]model.Channel{
			1: {ID: 1, Enabled: true, Keys: []model.ChannelKey{{Enabled: true}, {Enabled: true}, {Enabled: false}}},
			2: {ID: 2, Enabled: false, Keys: []model.ChannelKey{{Enabled: true}}},
		},
		[]model.StatsModel{
			{ID: 1, Name: "gpt-5.2", ChannelID: 1, StatsMetrics: model.StatsMetrics{WaitTime: 3000, RequestSuccess: 9, RequestFailed: 1}},
			{ID: 2, Name: "gpt-5.2", ChannelID: 2, StatsMetrics: model.StatsMetrics{WaitTime: 1000, RequestSuccess: 1, RequestFailed: 1}},
		},
		time.Date(2026, 4, 29, 10, 0, 0, 0, time.FixedZone("CST", 8*3600)),
	)

	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].ChannelCount != 2 {
		t.Fatalf("ChannelCount = %d, want 2", items[0].ChannelCount)
	}
	if items[0].EnabledKeyCount != 3 {
		t.Fatalf("EnabledKeyCount = %d, want 3", items[0].EnabledKeyCount)
	}
	if items[0].AverageLatencyMS != 333 {
		t.Fatalf("AverageLatencyMS = %d, want 333", items[0].AverageLatencyMS)
	}
	if items[0].SuccessRate != 0.8333333333333334 {
		t.Fatalf("SuccessRate = %v, want 0.8333333333333334", items[0].SuccessRate)
	}
	if summary.UniqueChannelCount != 2 {
		t.Fatalf("UniqueChannelCount = %d, want 2", summary.UniqueChannelCount)
	}
}

func TestBuildModelMarket_NormalizesAndMergesModelVariants(t *testing.T) {
	withModelMarketDedupeDefault(t, "true")

	items, summary := buildModelMarket(
		[]model.LLMInfo{
			{Name: "@cf/moonshotai/kimi-k2.5", LLMPrice: model.LLMPrice{Input: 1, Output: 2}},
			{Name: "kimi-k2.5", LLMPrice: model.LLMPrice{Input: 3, Output: 4}},
			{Name: "dmxapi-kimi-k2.5"},
			{Name: "kimi-k2.5-cc"},
		},
		[]model.LLMChannel{
			{Name: "kimi-k2.5", ChannelID: 1, ChannelName: "A", Enabled: true},
			{Name: "@cf/moonshotai/kimi-k2.5", ChannelID: 2, ChannelName: "B", Enabled: true},
			{Name: "dmxapi-kimi-k2.5", ChannelID: 3, ChannelName: "C", Enabled: true},
			{Name: "kimi-k2.5-cc", ChannelID: 1, ChannelName: "A", Enabled: true},
		},
		map[int]model.Channel{
			1: {ID: 1, Keys: []model.ChannelKey{{Enabled: true}, {Enabled: false}}},
			2: {ID: 2, Keys: []model.ChannelKey{{Enabled: true}, {Enabled: true}}},
			3: {ID: 3, Keys: []model.ChannelKey{{Enabled: true}}},
		},
		[]model.StatsModel{
			{Name: "kimi-k2.5", StatsMetrics: model.StatsMetrics{WaitTime: 100, RequestSuccess: 1}},
			{Name: "@cf/moonshotai/kimi-k2.5", StatsMetrics: model.StatsMetrics{WaitTime: 200, RequestSuccess: 2, RequestFailed: 1}},
			{Name: "dmxapi-kimi-k2.5", StatsMetrics: model.StatsMetrics{WaitTime: 300, RequestFailed: 2}},
		},
		time.Time{},
	)

	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1: %+v", len(items), items)
	}
	item := items[0]
	if item.Name != "kimi-k2.5" {
		t.Fatalf("Name = %q, want kimi-k2.5", item.Name)
	}
	if item.Input != 3 || item.Output != 4 {
		t.Fatalf("price = (%v,%v), want canonical exact price (3,4)", item.Input, item.Output)
	}
	if item.ChannelCount != 3 {
		t.Fatalf("ChannelCount = %d, want 3", item.ChannelCount)
	}
	if item.EnabledKeyCount != 4 {
		t.Fatalf("EnabledKeyCount = %d, want 4", item.EnabledKeyCount)
	}
	if item.RequestSuccess != 3 || item.RequestFailed != 3 {
		t.Fatalf("requests = (%d,%d), want (3,3)", item.RequestSuccess, item.RequestFailed)
	}
	if item.AverageLatencyMS != 100 {
		t.Fatalf("AverageLatencyMS = %d, want 100", item.AverageLatencyMS)
	}
	if item.SuccessRate != 0.5 {
		t.Fatalf("SuccessRate = %v, want 0.5", item.SuccessRate)
	}
	if summary.ModelCount != 1 || summary.CoverageCount != 3 || summary.UniqueChannelCount != 3 {
		t.Fatalf("summary = %+v, want model=1 coverage=3 unique=3", summary)
	}
}

func TestBuildModelMarket_KeepsRawModelsWhenMarketDedupeDisabled(t *testing.T) {
	withModelMarketDedupeDefault(t, "false")

	items, _ := buildModelMarket(
		[]model.LLMInfo{
			{Name: "kimi-k2.5"},
			{Name: "dmxapi-kimi-k2.5"},
		},
		nil,
		nil,
		nil,
		time.Time{},
	)

	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
}

func TestBuildModelMarket_SortsItemsBySuccessRateThenSuccessCount(t *testing.T) {
	items, _ := buildModelMarket(
		[]model.LLMInfo{
			{Name: "z-model"},
			{Name: "a-model"},
			{Name: "b-model"},
			{Name: "c-model"},
		},
		nil,
		nil,
		[]model.StatsModel{
			{ID: 1, Name: "z-model", StatsMetrics: model.StatsMetrics{RequestSuccess: 8, RequestFailed: 2}},
			{ID: 2, Name: "a-model", StatsMetrics: model.StatsMetrics{RequestSuccess: 4, RequestFailed: 0}},
			{ID: 3, Name: "b-model", StatsMetrics: model.StatsMetrics{RequestSuccess: 6, RequestFailed: 0}},
		},
		time.Time{},
	)

	if len(items) != 4 {
		t.Fatalf("len(items) = %d, want 4", len(items))
	}
	if items[0].Name != "b-model" || items[1].Name != "a-model" || items[2].Name != "z-model" || items[3].Name != "c-model" {
		t.Fatalf("unexpected item order: %+v", items)
	}
}

func TestBuildModelMarket_UsesEmptyChannelsSliceWhenModelHasNoChannels(t *testing.T) {
	items, _ := buildModelMarket(
		[]model.LLMInfo{
			{Name: "standalone-model"},
		},
		nil,
		nil,
		nil,
		time.Time{},
	)

	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Channels == nil {
		t.Fatal("Channels = nil, want empty slice")
	}
	if len(items[0].Channels) != 0 {
		t.Fatalf("len(Channels) = %d, want 0", len(items[0].Channels))
	}
}

func TestBuildModelMarket_IncludesChannelOnlyModelsAndVariants(t *testing.T) {
	items, summary := buildModelMarketWithDedupe(
		[]model.LLMInfo{{Name: "priced-model", LLMPrice: model.LLMPrice{Input: 1}}},
		[]model.LLMChannel{
			{Name: "priced-model", ChannelID: 1, ChannelName: "A", Enabled: true},
			{Name: "dmxapi-channel-only", ChannelID: 1, ChannelName: "A", Enabled: true},
			{Name: "channel-only", ChannelID: 2, ChannelName: "B", Enabled: true},
		},
		map[int]model.Channel{
			1: {ID: 1, Keys: []model.ChannelKey{{Enabled: true}}},
			2: {ID: 2, Keys: []model.ChannelKey{{Enabled: true}, {Enabled: true}}},
		},
		nil,
		time.Time{},
		true,
	)

	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2: %+v", len(items), items)
	}
	if summary.CoverageCount != 3 {
		t.Fatalf("CoverageCount = %d, want 3", summary.CoverageCount)
	}

	var channelOnly model.ModelMarketItem
	for _, item := range items {
		if item.Name == "channel-only" {
			channelOnly = item
		}
	}
	if channelOnly.Name != "channel-only" || channelOnly.PrimaryName != "channel-only" {
		t.Fatalf("channel-only item = %+v", channelOnly)
	}
	if channelOnly.Input != 0 || channelOnly.ChannelCount != 2 || channelOnly.EnabledKeyCount != 3 {
		t.Fatalf("channel-only coverage = %+v", channelOnly)
	}

	var priced model.ModelMarketItem
	for _, item := range items {
		if item.Name == "priced-model" {
			priced = item
		}
	}
	if priced.VariantCount != 1 || len(priced.VariantNames) != 1 || priced.PrimaryName != "priced-model" {
		t.Fatalf("priced variants = %+v", priced)
	}
}

func TestBuildModelMarket_DedupeModeControlsCanonicalAndRawItems(t *testing.T) {
	inputs := []model.LLMInfo{
		{Name: "kimi-k2.5"},
		{Name: "dmxapi-kimi-k2.5"},
	}

	deduped, _ := buildModelMarketWithDedupe(inputs, nil, nil, nil, time.Time{}, true)
	if len(deduped) != 1 || deduped[0].Name != "kimi-k2.5" || deduped[0].VariantCount != 2 {
		t.Fatalf("deduped = %+v", deduped)
	}

	raw, _ := buildModelMarketWithDedupe(inputs, nil, nil, nil, time.Time{}, false)
	if len(raw) != 2 {
		t.Fatalf("raw len = %d, want 2: %+v", len(raw), raw)
	}
	for _, item := range raw {
		if item.VariantCount != 1 || item.CanonicalName != "kimi-k2.5" {
			t.Fatalf("raw item = %+v", item)
		}
	}
}

func TestBuildModelMarket_RawModePreservesEveryCaseSensitiveChannelName(t *testing.T) {
	items, _ := buildModelMarketWithDedupe(
		nil,
		[]model.LLMChannel{
			{Name: "GPT-4o", ChannelID: 1, ChannelName: "A"},
			{Name: "gpt-4o", ChannelID: 2, ChannelName: "B"},
			{Name: "@cf/openai/gpt-4o", ChannelID: 3, ChannelName: "C"},
		},
		nil,
		nil,
		time.Time{},
		false,
	)
	if len(items) != 3 {
		t.Fatalf("raw items = %+v, want one item for each exact name", items)
	}
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		seen[item.Name] = true
		if item.PrimaryName != item.Name {
			t.Fatalf("raw primary_name = %q, want %q", item.PrimaryName, item.Name)
		}
		if item.VariantCount != 1 {
			t.Fatalf("raw variant_count = %d, want 1", item.VariantCount)
		}
	}
	for _, name := range []string{"GPT-4o", "gpt-4o", "@cf/openai/gpt-4o"} {
		if !seen[name] {
			t.Fatalf("raw model %q missing from %+v", name, items)
		}
	}
}

func withModelMarketDedupeDefault(t *testing.T, value string) {
	t.Helper()

	settingCache := setting.GetCache()
	oldSettings := settingCache.GetAll()
	settingCache.Set(model.SettingKeyModelNormalizeMarketDedupeDefault, value)
	t.Cleanup(func() {
		settingCache.Clear()
		for key, oldValue := range oldSettings {
			settingCache.Set(key, oldValue)
		}
	})
}
