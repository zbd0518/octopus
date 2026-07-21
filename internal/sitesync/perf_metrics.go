package sitesync

import (
	"math"
	"strings"
	"time"

	"github.com/lingyuins/octopus/internal/model"
)

// siteModelPerfDetection 是从 NewAPI /api/perf-metrics/summary 解析出的性能指标。
// LatencyMs 平均延迟；AvgTps 平均吞吐；SuccessRate 成功率 0-1。
type siteModelPerfDetection struct {
	LatencyMs   int64
	AvgTps      float64
	SuccessRate float64
	HasMetrics  bool
}

func parseSiteModelPerfDetection(item map[string]any) siteModelPerfDetection {
	if item == nil {
		return siteModelPerfDetection{}
	}

	latencyMs := firstPositiveInt64(
		int64(jsonFloat(item["avg_latency_ms"])),
		int64(jsonFloat(item["AvgLatencyMs"])),
		int64(jsonFloat(item["latency_ms"])),
	)
	avgTps := firstPositiveFloat(
		jsonFloat(item["avg_tps"]),
		jsonFloat(item["AvgTps"]),
		jsonFloat(item["tps"]),
	)
	// success_rate 可能是 0-1 或 0-100
	successRaw := firstNonNegativeFloat(
		jsonFloat(item["success_rate"]),
		jsonFloat(item["SuccessRate"]),
	)
	successRate := normalizeSuccessRate(successRaw)

	if latencyMs <= 0 && avgTps <= 0 && successRate <= 0 {
		return siteModelPerfDetection{}
	}
	return siteModelPerfDetection{
		LatencyMs:   latencyMs,
		AvgTps:      avgTps,
		SuccessRate: successRate,
		HasMetrics:  true,
	}
}

func normalizeSuccessRate(raw float64) float64 {
	if raw <= 0 || math.IsNaN(raw) || math.IsInf(raw, 0) {
		return 0
	}
	// NewAPI 文档与实现通常是 0-1；若 >1 则按百分比处理。
	if raw > 1 {
		raw = raw / 100
	}
	if raw > 1 {
		return 1
	}
	return raw
}

func collectPerfMetricsDetections(
	payload map[string]any,
	_ string,
	modelFilter map[string]struct{},
) map[string]siteModelRouteDetection {
	if payload == nil {
		return nil
	}

	// 兼容 data.models / data / models 几种壳层。
	items := normalizeItemSlice(payload["data"])
	if len(items) == 0 {
		if dataMap, ok := payload["data"].(map[string]any); ok {
			items = normalizeItemSlice(dataMap["models"])
		}
	}
	if len(items) == 0 {
		items = normalizeItemSlice(payload["models"])
	}
	if len(items) == 0 {
		return nil
	}

	result := make(map[string]siteModelRouteDetection)
	for _, item := range items {
		modelName := firstNonEmptyString(
			jsonString(item["model_name"]),
			jsonString(item["model"]),
			jsonString(item["name"]),
			jsonString(item["id"]),
		)
		normalizedName := strings.ToLower(strings.TrimSpace(modelName))
		if normalizedName == "" {
			continue
		}
		if len(modelFilter) > 0 {
			if _, exists := modelFilter[normalizedName]; !exists {
				continue
			}
		}
		perf := parseSiteModelPerfDetection(item)
		if !perf.HasMetrics {
			continue
		}
		result[normalizedName] = siteModelRouteDetection{Perf: perf}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func applySiteModelPerfDetection(item *model.SiteModel, detection siteModelPerfDetection, now time.Time) {
	if item == nil || !detection.HasMetrics {
		return
	}
	item.PerfLatencyMs = detection.LatencyMs
	item.PerfAvgTps = roundPrice(detection.AvgTps)
	item.PerfSuccessRate = roundPrice(detection.SuccessRate)
	item.PerfUpdatedAt = &now
}

func siteModelHasUpstreamPerf(item model.SiteModel) bool {
	return item.PerfLatencyMs > 0 || item.PerfAvgTps > 0 || item.PerfSuccessRate > 0
}

func firstPositiveInt64(values ...int64) int64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}
