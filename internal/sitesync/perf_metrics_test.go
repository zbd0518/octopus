package sitesync

import "testing"

func TestParseSiteModelPerfDetection(t *testing.T) {
	detection := parseSiteModelPerfDetection(map[string]any{
		"avg_latency_ms": 139000,
		"avg_tps":        39.2,
		"success_rate":   0.95,
	})
	if !detection.HasMetrics {
		t.Fatal("expected metrics")
	}
	if detection.LatencyMs != 139000 {
		t.Fatalf("latency = %d", detection.LatencyMs)
	}
	if detection.AvgTps != 39.2 {
		t.Fatalf("tps = %v", detection.AvgTps)
	}
	if detection.SuccessRate != 0.95 {
		t.Fatalf("success = %v", detection.SuccessRate)
	}
}

func TestParseSiteModelPerfDetectionPercentSuccess(t *testing.T) {
	detection := parseSiteModelPerfDetection(map[string]any{
		"avg_latency_ms": 500,
		"success_rate":   95,
	})
	if detection.SuccessRate != 0.95 {
		t.Fatalf("success = %v, want 0.95", detection.SuccessRate)
	}
}

func TestCollectPerfMetricsDetections(t *testing.T) {
	payload := map[string]any{
		"data": map[string]any{
			"models": []any{
				map[string]any{
					"model_name":     "glm-5.2",
					"avg_latency_ms": 1200,
					"avg_tps":        12.5,
					"success_rate":   0.88,
				},
			},
		},
	}
	detections := collectPerfMetricsDetections(payload, "/api/perf-metrics/summary", map[string]struct{}{
		"glm-5.2": {},
	})
	d, ok := detections["glm-5.2"]
	if !ok || !d.Perf.HasMetrics {
		t.Fatal("expected glm-5.2 metrics")
	}
	if d.Perf.LatencyMs != 1200 {
		t.Fatalf("latency = %d", d.Perf.LatencyMs)
	}
}
