package ops

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/relaylog"
	"github.com/lingyuins/octopus/internal/op/setting"
	"github.com/lingyuins/octopus/internal/utils/telemetry"
)

func loadOpsSource(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	src, err := os.ReadFile(filepath.Clean(filepath.Join(filepath.Dir(file), "ops.go")))
	if err != nil {
		t.Fatalf("read ops.go: %v", err)
	}
	return string(src)
}

func setupOpsLogDB(t *testing.T) {
	t.Helper()
	mainPath := filepath.Join(t.TempDir(), "main.db")
	if err := db.InitDB("sqlite", mainPath, false); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	logPath := filepath.Join(t.TempDir(), "logs.db")
	if err := db.InitLogDB("sqlite", logPath, false); err != nil {
		t.Fatalf("InitLogDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("RefreshCache: %v", err)
	}
	if err := setting.SetString(model.SettingKeyRelayLogKeepEnabled, "true"); err != nil {
		t.Fatalf("enable keep: %v", err)
	}
	t.Cleanup(relaylog.SetCacheForTest(nil))
	// 清掉可能残留的 telemetry logs 缓存。
	telemetryLogsCacheMu.Lock()
	telemetryLogsCache = opsTelemetryLogSample{}
	telemetryLogsCacheKey = 0
	telemetryLogsCacheExp = time.Time{}
	telemetryLogsCacheMu.Unlock()
}

// TestLoadOpsTelemetryLogs_DoesNotMaterializeFullRows 验证 telemetry 读路径
// 不再把 1h 内全部 relay_logs 行（含大字段）装入 []RelayLog。
func TestLoadOpsTelemetryLogs_DoesNotMaterializeFullRows(t *testing.T) {
	setupOpsLogDB(t)

	now := time.Now()
	logs := make([]model.RelayLog, 0, 50)
	for i := 0; i < 50; i++ {
		errText := ""
		if i%5 == 0 {
			errText = "boom"
		}
		logs = append(logs, model.RelayLog{
			ID:              int64(10000 + i),
			Time:            now.Add(-time.Duration(i) * time.Second).Unix(),
			UseTime:         100 + i*10,
			Error:           errText,
			RequestContent:  strings.Repeat("q", 1024),
			ResponseContent: strings.Repeat("a", 2048),
			ChannelId:       1,
			ActualModelName: "gpt-4o",
		})
	}
	if err := db.GetLogDB().Create(&logs).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 源码不变量：loadOpsTelemetryLogs 不得 Select response_content / request_content，
	// 且不得 Find 到 []model.RelayLog 全字段。
	src := loadOpsSource(t)
	fn := "func loadOpsTelemetryLogs"
	idx := strings.Index(src, fn)
	if idx < 0 {
		t.Fatal("loadOpsTelemetryLogs not found")
	}
	end := strings.Index(src[idx+1:], "\nfunc ")
	body := src[idx:]
	if end > 0 {
		body = src[idx : idx+1+end]
	}
	if strings.Contains(body, "response_content") || strings.Contains(body, "request_content") {
		t.Fatal("loadOpsTelemetryLogs must not select content large fields")
	}
	if strings.Contains(body, "Find(&dbLogs)") && strings.Contains(body, "[]model.RelayLog") {
		// 允许聚合路径；禁止全量 RelayLog Find。
		if !strings.Contains(body, "Group(") && !strings.Contains(body, "telemetryLogAggregate") && !strings.Contains(body, "opsTelemetryAggregate") {
			t.Fatal("loadOpsTelemetryLogs still Finds full RelayLog rows without aggregation")
		}
	}

	// 行为：TelemetrySummaryGet 在大量日志下仍可返回（不 OOM、有 P95/吞吐）。
	summary, err := TelemetrySummaryGet(context.Background())
	if err != nil {
		t.Fatalf("TelemetrySummaryGet: %v", err)
	}
	if summary == nil {
		t.Fatal("summary is nil")
	}
	// snap 可能为空；至少结构可访问。
	_ = summary.RuntimeSignals.MemoryMB
	_ = telemetry.Global().Snapshot()
}

// TestLoadOpsProviderPromptCacheLogs_UsesPersistedCacheColumns 源码级：
// prompt cache 摘要不得再依赖 response_content 大字段全量加载。
func TestLoadOpsProviderPromptCacheLogs_UsesPersistedCacheColumns(t *testing.T) {
	src := loadOpsSource(t)
	fn := "func loadOpsProviderPromptCacheLogs"
	idx := strings.Index(src, fn)
	if idx < 0 {
		t.Fatal("loadOpsProviderPromptCacheLogs not found")
	}
	end := strings.Index(src[idx+1:], "\nfunc ")
	body := src[idx:]
	if end > 0 {
		body = src[idx : idx+1+end]
	}
	if strings.Contains(body, `"response_content"`) {
		t.Fatal("loadOpsProviderPromptCacheLogs must not select response_content large field")
	}
}
