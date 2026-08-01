package stats

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/setting"
)

// TestDailyDimensionChannelUpdatePersistsUpsert 回归：stats_daily_channel 的
// ON CONFLICT upsert 曾引用不存在的列（histogram_lt_100 等，GORM 实际列名为
// histogram_lt100），导致每次写入报 SQL 错误、今日统计永远为空（累计表
// stats_channel 因不引用 histogram 列而正常，表现为「累计调用增长、今日调用 0」）。
func TestDailyDimensionChannelUpdatePersistsUpsert(t *testing.T) {
	cleanupNow := SetTimeNowForTest(func() time.Time {
		return time.Date(2026, 5, 20, 8, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	})
	defer cleanupNow()

	setting.GetCache().Clear()
	setting.GetCache().Set(model.SettingKeyStatsTimezoneOffset, "8")
	ClearAllCachesForTest()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	if err := db.InitDB("sqlite", dsn, true); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	// 两次写入同一 (date, channel_id)，验证 ON CONFLICT 累加而非报错。
	if err := DailyDimensionChannelUpdate(ctx, 42, "deepseek-ch", model.StatsMetrics{
		RequestSuccess: 1,
		RequestFailed:  0,
		InputCost:      0.14,
		OutputCost:     0.28,
	}); err != nil {
		t.Fatalf("first DailyDimensionChannelUpdate: %v", err)
	}
	if err := DailyDimensionChannelUpdate(ctx, 42, "deepseek-ch", model.StatsMetrics{
		RequestSuccess: 1,
		RequestFailed:  0,
		InputCost:      0.14,
		OutputCost:     0.28,
	}); err != nil {
		t.Fatalf("second DailyDimensionChannelUpdate: %v", err)
	}

	var row model.StatsDailyChannel
	if err := db.GetDB().WithContext(ctx).
		Where("date = ? AND channel_id = ?", today(), 42).
		First(&row).Error; err != nil {
		t.Fatalf("stats_daily_channel 今日行不存在（upsert 失败）: %v", err)
	}
	if row.RequestSuccess != 2 {
		t.Errorf("RequestSuccess = %d, want 2（两次累加）", row.RequestSuccess)
	}
	if row.InputCost != 0.28 || row.OutputCost != 0.56 {
		t.Errorf("cost = (%v, %v), want (0.28, 0.56)", row.InputCost, row.OutputCost)
	}
}
