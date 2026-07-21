package analytics

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/relaylog"
	"github.com/lingyuins/octopus/internal/op/setting"
)

// TestIssue148_ChannelModelMergesRelayLogsForMissingKeys 验证 stats_daily 为主、
// relay_logs 补缺的合并策略：stats_daily_channel_model 有渠道A 的数据，
// relay_logs 有渠道B 的数据（迁移前写入、未进 stats_daily），两者都应出现。
// 修复前 loadAnalyticsChannelModelRows 在 stats_daily 有任意行后彻底不读
// relay_logs，导致迁移/更新重启后旧模型消失（issue #148）。
func TestIssue148_ChannelModelMergesRelayLogsForMissingKeys(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "issue148.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("RefreshCache failed: %v", err)
	}
	if err := setting.SetString(model.SettingKeyRelayLogKeepEnabled, "true"); err != nil {
		t.Fatalf("enable relay log keep failed: %v", err)
	}
	t.Cleanup(relaylog.SetCacheForTest(nil))

	// stats_daily_channel_model 只有渠道A 的数据（迁移后新请求写入）。
	date := time.Now().Format("20060102")
	if err := db.GetDB().Create(&model.StatsDailyChannelModel{
		Date:        date,
		ChannelID:   1,
		ChannelName: "channelA",
		ModelName:   "gpt-4o",
		StatsMetrics: model.StatsMetrics{
			RequestSuccess: 5,
			InputToken:     500,
			OutputToken:    200,
			InputCost:      0.5,
			OutputCost:     0.2,
		},
	}).Error; err != nil {
		t.Fatalf("create stats_daily_channel_model: %v", err)
	}

	// relay_logs 有渠道B 的数据（迁移前写入，未进 stats_daily）。
	logs := []model.RelayLog{
		{
			ID: 100, Time: time.Now().Unix(),
			RequestModelName: "claude-3", ActualModelName: "claude-3",
			ChannelId: 2, ChannelName: "channelB", Error: "",
			InputTokens: 300, OutputTokens: 100, Cost: 0.3,
		},
	}
	if err := db.GetDB().Create(&logs).Error; err != nil {
		t.Fatalf("create relay_logs: %v", err)
	}

	items, err := AnalyticsChannelModelBreakdownGet(context.Background(), model.AnalyticsRange1D, nil)
	if err != nil {
		t.Fatalf("AnalyticsChannelModelBreakdownGet error: %v", err)
	}

	foundA, foundB := false, false
	for _, it := range items {
		switch it.ChannelID {
		case 1:
			foundA = true
			if it.ModelName != "gpt-4o" || it.RequestCount != 5 {
				t.Errorf("channelA: model=%q count=%d, want gpt-4o/5", it.ModelName, it.RequestCount)
			}
		case 2:
			foundB = true
			if it.ModelName != "claude-3" || it.RequestCount != 1 {
				t.Errorf("channelB: model=%q count=%d, want claude-3/1", it.ModelName, it.RequestCount)
			}
		}
	}
	if !foundA {
		t.Error("channelA (from stats_daily) missing in breakdown")
	}
	if !foundB {
		t.Error("channelB (from relay_logs, not in stats_daily) missing in breakdown — issue #148 regression")
	}
}

// TestIssue148_ModelBreakdownMergesRelayLogsForMissingKeys 验证按模型维度同样
// 执行补缺合并：stats_daily_model 有 gpt-4o，relay_logs 有 claude-3，两者都出现。
func TestIssue148_ModelBreakdownMergesRelayLogsForMissingKeys(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "issue148-model.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("RefreshCache failed: %v", err)
	}
	if err := setting.SetString(model.SettingKeyRelayLogKeepEnabled, "true"); err != nil {
		t.Fatalf("enable relay log keep failed: %v", err)
	}
	t.Cleanup(relaylog.SetCacheForTest(nil))

	date := time.Now().Format("20060102")
	if err := db.GetDB().Create(&model.StatsDailyModel{
		Date:      date,
		ModelName: "gpt-4o",
		StatsMetrics: model.StatsMetrics{
			RequestSuccess: 3,
			InputToken:     300,
			OutputToken:    100,
		},
	}).Error; err != nil {
		t.Fatalf("create stats_daily_model: %v", err)
	}

	logs := []model.RelayLog{
		{
			ID: 200, Time: time.Now().Unix(),
			RequestModelName: "claude-3", ActualModelName: "claude-3",
			ChannelId: 1, Error: "",
			InputTokens: 100, OutputTokens: 50, Cost: 0.1,
		},
	}
	if err := db.GetDB().Create(&logs).Error; err != nil {
		t.Fatalf("create relay_logs: %v", err)
	}

	items, err := AnalyticsModelBreakdownGet(context.Background(), model.AnalyticsRange1D)
	if err != nil {
		t.Fatalf("AnalyticsModelBreakdownGet error: %v", err)
	}

	foundGPT, foundClaude := false, false
	for _, it := range items {
		switch it.ModelName {
		case "gpt-4o":
			foundGPT = true
		case "claude-3":
			foundClaude = true
		}
	}
	if !foundGPT {
		t.Error("gpt-4o (from stats_daily) missing in model breakdown")
	}
	if !foundClaude {
		t.Error("claude-3 (from relay_logs, not in stats_daily) missing in model breakdown — issue #148 regression")
	}
}
