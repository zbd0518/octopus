package sitesync

import (
	"math"
	"strings"
	"time"

	"github.com/lingyuins/octopus/internal/model"
)

// token 模式常用 model_ratio，相对基准 $0.002 / 1K tokens，
// 换算成 $/M tokens 为 model_ratio * group_ratio * 2。

const (
	sitePriceBillingToken  = "token"
	sitePriceBillingPerCall = "per_call"
)

// siteModelPriceDetection 是从 /api/pricing 解析出的原始价格信息。
// GroupRatios 为按分组倍率；若为空则使用 BaseGroupRatio（默认 1）。
type siteModelPriceDetection struct {
	BillingMode    string
	Input          float64
	Output         float64
	CacheRead      float64
	CacheWrite     float64
	BaseGroupRatio float64
	GroupRatios    map[string]float64
	HasPrice       bool
}

func parseSiteModelPriceDetection(item map[string]any) siteModelPriceDetection {
	if item == nil {
		return siteModelPriceDetection{}
	}

	modelRatio := firstPositiveFloat(
		jsonFloat(item["model_ratio"]),
		jsonFloat(item["ModelRatio"]),
	)
	completionRatio := firstPositiveFloat(
		jsonFloat(item["completion_ratio"]),
		jsonFloat(item["CompletionRatio"]),
		1,
	)
	cacheRatio := firstNonNegativeFloat(
		jsonFloat(item["cache_ratio"]),
		jsonFloat(item["CacheRatio"]),
	)
	modelPrice := firstPositiveFloat(
		jsonFloat(item["model_price"]),
		jsonFloat(item["ModelPrice"]),
	)
	// 部分旧接口把固定价塞进 quota；仅当没有 model_ratio 时作为按次价候选。
	if modelPrice <= 0 && modelRatio <= 0 {
		modelPrice = firstPositiveFloat(
			jsonFloat(item["quota"]),
			jsonFloat(item["Quota"]),
		)
	}
	quotaType := int(jsonFloat(item["quota_type"]))
	if raw, ok := item["quota_type"]; !ok || raw == nil {
		if modelPrice > 0 && modelRatio <= 0 {
			quotaType = 1
		}
	}

	baseGroupRatio, groupRatios := parseGroupRatioField(item["group_ratio"])
	if baseGroupRatio <= 0 {
		baseGroupRatio = 1
	}

	// 按次 / 固定价：quota_type=1，或仅有 model_price 没有 model_ratio。
	if quotaType == 1 || (modelPrice > 0 && modelRatio <= 0) {
		if modelPrice <= 0 {
			return siteModelPriceDetection{}
		}
		return siteModelPriceDetection{
			BillingMode:    sitePriceBillingPerCall,
			Input:          modelPrice,
			Output:         modelPrice,
			CacheRead:      0,
			CacheWrite:     0,
			BaseGroupRatio: baseGroupRatio,
			GroupRatios:    groupRatios,
			HasPrice:       true,
		}
	}

	// token 模式：优先 model_ratio；若只有 model_price 且看起来像 $/M 也可兜底。
	if modelRatio > 0 {
		input := modelRatio * 2
		output := modelRatio * completionRatio * 2
		cacheRead := 0.0
		if cacheRatio > 0 {
			cacheRead = modelRatio * cacheRatio * 2
		}
		return siteModelPriceDetection{
			BillingMode:    sitePriceBillingToken,
			Input:          input,
			Output:         output,
			CacheRead:      cacheRead,
			CacheWrite:     0,
			BaseGroupRatio: baseGroupRatio,
			GroupRatios:    groupRatios,
			HasPrice:       true,
		}
	}

	if modelPrice > 0 {
		// 某些部署把 model_price 直接当 input $/M，completion_ratio 作输出倍率。
		return siteModelPriceDetection{
			BillingMode:    sitePriceBillingToken,
			Input:          modelPrice,
			Output:         modelPrice * completionRatio,
			CacheRead:      modelPrice * cacheRatio,
			CacheWrite:     0,
			BaseGroupRatio: baseGroupRatio,
			GroupRatios:    groupRatios,
			HasPrice:       true,
		}
	}

	return siteModelPriceDetection{}
}

func parseGroupRatioField(raw any) (float64, map[string]float64) {
	switch v := raw.(type) {
	case nil:
		return 1, nil
	case float64:
		if v <= 0 {
			return 1, nil
		}
		return v, nil
	case float32:
		if v <= 0 {
			return 1, nil
		}
		return float64(v), nil
	case int:
		if v <= 0 {
			return 1, nil
		}
		return float64(v), nil
	case int64:
		if v <= 0 {
			return 1, nil
		}
		return float64(v), nil
	case string:
		f := jsonFloat(v)
		if f <= 0 {
			return 1, nil
		}
		return f, nil
	case map[string]any:
		ratios := make(map[string]float64, len(v))
		for key, value := range v {
			normalized := model.NormalizeSiteGroupKey(key)
			f := jsonFloat(value)
			if normalized == "" || f <= 0 {
				continue
			}
			ratios[normalized] = f
		}
		if len(ratios) == 0 {
			return 1, nil
		}
		// 无精确分组命中时回退 1，而不是随便挑一个分组倍率。
		return 1, ratios
	default:
		return 1, nil
	}
}

func resolveGroupRatio(base float64, ratios map[string]float64, groupKey string) float64 {
	if len(ratios) > 0 {
		key := model.NormalizeSiteGroupKey(groupKey)
		if key != "" {
			if r, ok := ratios[key]; ok && r > 0 {
				return r
			}
		}
		if r, ok := ratios[model.SiteDefaultGroupKey]; ok && r > 0 {
			return r
		}
	}
	if base > 0 {
		return base
	}
	return 1
}

func applySiteModelPriceDetection(item *model.SiteModel, detection siteModelPriceDetection, now time.Time) {
	if item == nil || !detection.HasPrice {
		return
	}
	ratio := resolveGroupRatio(detection.BaseGroupRatio, detection.GroupRatios, item.GroupKey)
	item.PriceBillingMode = detection.BillingMode
	item.PriceInput = roundPrice(detection.Input * ratio)
	item.PriceOutput = roundPrice(detection.Output * ratio)
	item.PriceCacheRead = roundPrice(detection.CacheRead * ratio)
	item.PriceCacheWrite = roundPrice(detection.CacheWrite * ratio)
	item.PriceUpdatedAt = &now
}

func clearSiteModelPrice(item *model.SiteModel) {
	if item == nil {
		return
	}
	item.PriceBillingMode = ""
	item.PriceInput = 0
	item.PriceOutput = 0
	item.PriceCacheRead = 0
	item.PriceCacheWrite = 0
	item.PriceUpdatedAt = nil
}

func siteModelHasUpstreamPrice(item model.SiteModel) bool {
	if strings.TrimSpace(item.PriceBillingMode) == "" {
		return false
	}
	return item.PriceInput > 0 || item.PriceOutput > 0 || item.PriceCacheRead > 0 || item.PriceCacheWrite > 0
}

func firstPositiveFloat(values ...float64) float64 {
	for _, v := range values {
		if v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
			return v
		}
	}
	return 0
}

func firstNonNegativeFloat(values ...float64) float64 {
	for _, v := range values {
		if v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
			// 允许 0 作为显式值，但跳过“完全未提供”的哨兵没有意义；
			// 这里把第一个有限非负值返回，调用方再判断是否 > 0。
			return v
		}
	}
	return 0
}

func roundPrice(v float64) float64 {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	// 保留最多 6 位小数，避免 ratio 连乘产生过长尾巴。
	return math.Round(v*1e6) / 1e6
}
