package stats

import (
	"context"
	"os"
	"testing"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
)

// TestDailyDimensionPostgresIntegration requires a dedicated writable database.
// Set OCTOPUS_TEST_POSTGRES_DSN to enable it.
func TestDailyDimensionPostgresIntegration(t *testing.T) {
	dsn := os.Getenv("OCTOPUS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("OCTOPUS_TEST_POSTGRES_DSN not set")
	}

	if err := db.InitDB("postgres", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if err := db.GetDB().WithContext(ctx).Where("channel_id = ?", 9001).Delete(&model.StatsDailyChannel{}).Error; err != nil {
		t.Fatalf("clean daily channel: %v", err)
	}
	if err := db.GetDB().WithContext(ctx).Where("model_name = ?", "m9001").Delete(&model.StatsDailyModel{}).Error; err != nil {
		t.Fatalf("clean daily model: %v", err)
	}
	if err := db.GetDB().WithContext(ctx).Where("api_key_id = ?", 9001).Delete(&model.StatsDailyAPIKey{}).Error; err != nil {
		t.Fatalf("clean daily api key: %v", err)
	}
	if err := db.GetDB().WithContext(ctx).Where("channel_id = ?", 9001).Delete(&model.StatsDailyChannelModel{}).Error; err != nil {
		t.Fatalf("clean daily channel model: %v", err)
	}

	metrics := model.StatsMetrics{
		RequestSuccess: 1,
		InputToken:     10,
		OutputToken:    20,
		InputCost:      0.14,
		OutputCost:     0.28,
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := DailyDimensionAPIKeyUpdate(ctx, 9001, "k9001", metrics); err != nil {
			t.Fatalf("api key upsert #%d: %v", attempt+1, err)
		}
		if err := DailyDimensionChannelUpdate(ctx, 9001, "ch9001", metrics); err != nil {
			t.Fatalf("channel upsert #%d: %v", attempt+1, err)
		}
		if err := DailyDimensionModelUpdate(ctx, "m9001", metrics); err != nil {
			t.Fatalf("model upsert #%d: %v", attempt+1, err)
		}
		if err := DailyDimensionChannelModelUpdate(ctx, 9001, "ch9001", "m9001", metrics); err != nil {
			t.Fatalf("channel-model upsert #%d: %v", attempt+1, err)
		}
	}

	var row model.StatsDailyAPIKey
	if err := db.GetDB().WithContext(ctx).
		Where("api_key_id = ? AND date = ?", 9001, today()).
		First(&row).Error; err != nil {
		t.Fatalf("stats_daily_api_keys row missing: %v", err)
	}
	if row.RequestSuccess != 2 || row.InputToken != 20 {
		t.Fatalf("row = %+v, want request_success=2 input_token=20", row.StatsMetrics)
	}
}
