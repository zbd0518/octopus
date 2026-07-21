package sitesync

import (
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/model"
)

func TestParseSiteModelPriceDetectionTokenMode(t *testing.T) {
	// model_ratio=1 → $2 / 1M input；completion_ratio=2 → $4 / 1M output
	detection := parseSiteModelPriceDetection(map[string]any{
		"model_ratio":      1.0,
		"completion_ratio": 2.0,
		"cache_ratio":      0.5,
		"group_ratio":      1.0,
	})
	if !detection.HasPrice {
		t.Fatal("expected has price")
	}
	if detection.BillingMode != sitePriceBillingToken {
		t.Fatalf("billing mode = %q, want token", detection.BillingMode)
	}
	if detection.Input != 2 {
		t.Fatalf("input = %v, want 2", detection.Input)
	}
	if detection.Output != 4 {
		t.Fatalf("output = %v, want 4", detection.Output)
	}
	if detection.CacheRead != 1 {
		t.Fatalf("cache_read = %v, want 1", detection.CacheRead)
	}
}

func TestParseSiteModelPriceDetectionPerCallMode(t *testing.T) {
	detection := parseSiteModelPriceDetection(map[string]any{
		"quota_type":  1,
		"model_price": 0.05,
	})
	if !detection.HasPrice {
		t.Fatal("expected has price")
	}
	if detection.BillingMode != sitePriceBillingPerCall {
		t.Fatalf("billing mode = %q, want per_call", detection.BillingMode)
	}
	if detection.Input != 0.05 || detection.Output != 0.05 {
		t.Fatalf("input/output = %v/%v, want 0.05/0.05", detection.Input, detection.Output)
	}
}

func TestParseSiteModelPriceDetectionGroupRatioMap(t *testing.T) {
	detection := parseSiteModelPriceDetection(map[string]any{
		"model_ratio": 1.0,
		"group_ratio": map[string]any{
			"vip": 2.0,
		},
	})
	if !detection.HasPrice {
		t.Fatal("expected has price")
	}
	now := time.Now()
	item := model.SiteModel{GroupKey: "vip", ModelName: "gpt-4o"}
	applySiteModelPriceDetection(&item, detection, now)
	if item.PriceInput != 4 {
		t.Fatalf("vip input = %v, want 4 (2 * group_ratio 2)", item.PriceInput)
	}
	if item.PriceBillingMode != sitePriceBillingToken {
		t.Fatalf("billing mode = %q", item.PriceBillingMode)
	}
}

func TestCollectPricingRouteDetectionsKeepsPriceOnlyRows(t *testing.T) {
	payload := map[string]any{
		"data": []any{
			map[string]any{
				"model_name":              "gpt-4o-mini",
				"model_ratio":             0.5,
				"completion_ratio":        2.0,
				"supported_endpoint_types": []any{}, // 无 endpoint 时仍应保留价格
			},
		},
	}
	detections := collectPricingRouteDetections(payload, "/api/pricing", map[string]struct{}{
		"gpt-4o-mini": {},
	})
	d, ok := detections["gpt-4o-mini"]
	if !ok {
		t.Fatal("expected gpt-4o-mini detection")
	}
	if !d.Price.HasPrice {
		t.Fatal("expected price detection even without route endpoints")
	}
	if d.Price.Input != 1 {
		t.Fatalf("input = %v, want 1", d.Price.Input)
	}
}
