package sitesync

import (
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/model"
)

// 回归：已有定价 detection 时仍应能合并 perf 并写到 SiteModel。
func TestApplyKnownDetectionsMergesPerfOntoPricedRoute(t *testing.T) {
	pricing := collectPricingRouteDetections(map[string]any{
		"data": []any{
			map[string]any{
				"model_name":               "glm-5.2",
				"model_ratio":              0.4,
				"completion_ratio":         3.0,
				"cache_ratio":              0.2,
				"enable_groups":            []any{"default"},
				"supported_endpoint_types": []any{"/v1/chat/completions"},
			},
		},
	}, "/api/pricing", map[string]struct{}{"glm-5.2": {}})

	perf := collectPerfMetricsDetections(map[string]any{
		"data": map[string]any{
			"models": []any{
				map[string]any{
					"model_name":     "glm-5.2",
					"avg_latency_ms": 21772,
					"avg_tps":        61.0,
					"success_rate":   100.0,
				},
			},
		},
	}, "/api/perf-metrics/summary", map[string]struct{}{"glm-5.2": {}})

	// 模拟 short-circuit：先只有 pricing，再合并 perf（修复后的路径）
	merged := mergeSiteModelRouteDetections(pricing, perf)
	items := []model.SiteModel{{ModelName: "glm-5.2", GroupKey: "default"}}
	out := applyKnownRouteDetectionsToSiteModels(items, merged)
	if out[0].PriceBillingMode == "" || out[0].PriceInput <= 0 {
		t.Fatalf("price not applied: %#v", out[0])
	}
	if out[0].PerfLatencyMs != 21772 {
		t.Fatalf("latency = %d, want 21772", out[0].PerfLatencyMs)
	}
	if out[0].PerfAvgTps != 61 {
		t.Fatalf("tps = %v, want 61", out[0].PerfAvgTps)
	}
	if out[0].PerfSuccessRate != 1 {
		t.Fatalf("success = %v, want 1", out[0].PerfSuccessRate)
	}
	if out[0].PerfUpdatedAt == nil || time.Since(*out[0].PerfUpdatedAt) > time.Minute {
		t.Fatalf("perf_updated_at missing/stale: %#v", out[0].PerfUpdatedAt)
	}
}
