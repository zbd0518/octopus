package analytics

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
)

func loadAnalyticsSource(t *testing.T) (string, error) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", os.ErrInvalid
	}
	src, err := os.ReadFile(filepath.Clean(filepath.Join(filepath.Dir(file), "analytics.go")))
	if err != nil {
		return "", err
	}
	return string(src), nil
}

// TestChannelModelRowsFromRelayLogs_UsesDBAggregation 回归保护：
// loadAnalyticsChannelModelRowsFromRelayLogs 不得把 attempts JSON 大字段整行
// 拉进内存再 Go 聚合。修复前 90d/all 会 OOM。
func TestChannelModelRowsFromRelayLogs_UsesDBAggregation(t *testing.T) {
	setupSeparateLogDB(t)

	now := time.Now().Unix()
	logs := []model.RelayLog{
		{
			ID:               9001,
			Time:             now - 60,
			ChannelId:        1,
			ChannelName:      "provider-a",
			RequestModelName: "gpt-4o",
			ActualModelName:  "gpt-4o",
			InputTokens:      100,
			OutputTokens:     20,
			Cost:             0.01,
			// 故意塞入大 attempts，确保实现若仍 Find 整行 attempts 会明显变慢/膨胀。
			Attempts: []model.ChannelAttempt{
				{ChannelID: 1, ChannelName: "provider-a", ModelName: "gpt-4o", Status: model.AttemptFailed, AttemptNum: 1, Msg: strings.Repeat("x", 2048)},
				{ChannelID: 2, ChannelName: "provider-b", ModelName: "gpt-4o", Status: model.AttemptSuccess, AttemptNum: 2},
			},
		},
		{
			ID:               9002,
			Time:             now - 30,
			ChannelId:        2,
			ChannelName:      "provider-b",
			RequestModelName: "gpt-4o",
			ActualModelName:  "gpt-4o",
			InputTokens:      50,
			OutputTokens:     10,
			Cost:             0.005,
			Error:            "upstream error",
		},
		{
			ID:               9003,
			Time:             now - 10,
			ChannelId:        1,
			ChannelName:      "provider-a",
			RequestModelName: "claude",
			ActualModelName:  "claude-3-5",
			InputTokens:      200,
			OutputTokens:     40,
			Cost:             0.02,
		},
	}
	if err := db.GetLogDB().Create(&logs).Error; err != nil {
		t.Fatalf("seed log DB failed: %v", err)
	}

	rows, err := loadAnalyticsChannelModelRowsFromRelayLogs(context.Background(), model.AnalyticsRange1D, channelModelScope{})
	if err != nil {
		t.Fatalf("loadAnalyticsChannelModelRowsFromRelayLogs error: %v", err)
	}

	// 顶层聚合：按最终渠道×模型。9001 记在 channel=1/gpt-4o 成功（无 attempts 表行时走 JSON/顶层）。
	// 若 attempts 表存在且为空，JSON 分批 merge 会把 attempts 中 channel1 failed + channel2 success 计入。
	// 本测试未写 attempts 表行，故走 JSON 路径：channel1 failed + channel2 success + 9002 fail + 9003 success。
	keyA := "1\x00gpt-4o"
	rowA, ok := rows[keyA]
	if !ok {
		t.Fatalf("missing aggregate for %q; rows=%v", keyA, keysOfChannelModelRows(rows))
	}
	// 9001 attempts: channel1 failed；无成功 token。
	if rowA.RequestFailed < 1 {
		t.Fatalf("channel1/gpt-4o failed = %d, want >=1; row=%+v", rowA.RequestFailed, rowA)
	}

	keyB := "2\x00gpt-4o"
	rowB, ok := rows[keyB]
	if !ok {
		t.Fatalf("missing aggregate for %q; rows=%v", keyB, keysOfChannelModelRows(rows))
	}
	// 9001 attempts success on channel2 + 9002 top-level fail on channel2
	if rowB.RequestSuccess < 1 && rowB.RequestFailed < 1 {
		t.Fatalf("channel2/gpt-4o empty; row=%+v", rowB)
	}

	keyClaude := "1\x00claude-3-5"
	rowC, ok := rows[keyClaude]
	if !ok {
		keyClaude = "1\x00claude"
		rowC, ok = rows[keyClaude]
	}
	if !ok {
		t.Fatalf("missing claude aggregate; rows=%v", keysOfChannelModelRows(rows))
	}
	if rowC.RequestSuccess != 1 || rowC.InputTokens != 200 {
		t.Fatalf("claude aggregate = %+v, want success=1 input=200", rowC)
	}
}

// TestChannelModelRowsFromRelayLogs_SourceDoesNotSelectAttemptsJSON 源码级不变量：
// 主聚合路径必须走 attempts 表或分批 Limit，禁止无界 Find 全表 attempts。
func TestChannelModelRowsFromRelayLogs_SourceDoesNotSelectAttemptsJSON(t *testing.T) {
	src, err := loadAnalyticsSource(t)
	if err != nil {
		t.Fatalf("load source: %v", err)
	}
	marker := "func loadAnalyticsChannelModelRowsFromRelayLogs"
	idx := strings.Index(src, marker)
	if idx < 0 {
		t.Fatal("function not found in source")
	}
	// 检查到下一个同级 func 为止（包含 helper 定义区）。
	end := strings.Index(src[idx+len(marker):], "\nfunc makeAnalyticsAPIKeyAggregateKey")
	body := src[idx:]
	if end > 0 {
		body = src[idx : idx+len(marker)+end]
	}
	if !strings.Contains(body, "loadChannelModelRowsFromAttemptsTable") {
		t.Fatal("expected attempts-table aggregation path")
	}
	if !strings.Contains(body, "channelModelAttemptsJSONBatchSize") {
		t.Fatal("expected batched attempts JSON fallback with batch size constant")
	}
	if !strings.Contains(body, "Limit(channelModelAttemptsJSONBatchSize)") {
		t.Fatal("attempts JSON path must use Limit for bounded memory")
	}
	// 禁止无 Limit 的全表 Find liteLogs 旧模式。
	if strings.Contains(body, "var liteLogs []") && strings.Contains(body, "Find(&liteLogs)") {
		t.Fatal("unbounded Find(&liteLogs) must not return")
	}
}

func keysOfChannelModelRows(rows map[string]*analyticsChannelModelAggregateRow) []string {
	out := make([]string, 0, len(rows))
	for k := range rows {
		out = append(out, k)
	}
	return out
}
