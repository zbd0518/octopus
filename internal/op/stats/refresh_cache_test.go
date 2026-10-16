package stats

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/setting"
)

func TestHourlyGetRolling24hSpansYesterdayAndToday(t *testing.T) {
	cleanupNow := SetTimeNowForTest(func() time.Time {
		return time.Date(2026, 5, 20, 10, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	})
	defer cleanupNow()

	setting.GetCache().Clear()
	setting.GetCache().Set(model.SettingKeyStatsTimezoneOffset, "8")

	testName := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", testName)
	if err := db.InitDB("sqlite", dsn, true); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer func() { _ = db.Close() }()

	dbConn := db.GetDB().WithContext(context.Background())
	// 昨天小时 20/23 有数据；小时 21/22 无行（补 0 占位）；前天的行不应混入。
	if err := dbConn.Create(&[]model.StatsHourly{
		{Hour: 20, Date: "20260519", StatsMetrics: model.StatsMetrics{RequestSuccess: 7, InputToken: 70}},
		{Hour: 23, Date: "20260519", StatsMetrics: model.StatsMetrics{RequestSuccess: 9, InputToken: 90}},
		{Hour: 20, Date: "20260518", StatsMetrics: model.StatsMetrics{RequestSuccess: 111, InputToken: 999}},
		{Hour: 9, Date: "20260520", StatsMetrics: model.StatsMetrics{RequestSuccess: 2, InputToken: 20}},
	}).Error; err != nil {
		t.Fatalf("create hourly rows: %v", err)
	}

	if err := RefreshCache(context.Background()); err != nil {
		t.Fatalf("refresh cache: %v", err)
	}

	hourly := HourlyGetRolling24h()
	if len(hourly) != 24 {
		t.Fatalf("expected 24 rolling entries, got %d", len(hourly))
	}
	// 升序：昨天 11..23（13 桶）在前，今天 0..10（11 桶）在后。
	wantOrder := []int{11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	for i, want := range wantOrder {
		if hourly[i].Hour != want {
			t.Fatalf("entry %d: hour = %d, want %d", i, hourly[i].Hour, want)
		}
	}
	// 昨天的桶使用昨天日期，无行的补 0 占位。
	if hourly[0].Date != "20260519" {
		t.Fatalf("entry 0 date = %q, want 20260519", hourly[0].Date)
	}
	if hourly[9].RequestSuccess != 7 || hourly[9].InputToken != 70 {
		t.Fatalf("yesterday hour 20 should carry DB row data, got %+v", hourly[9])
	}
	if hourly[10].RequestSuccess != 0 || hourly[10].InputToken != 0 {
		t.Fatalf("yesterday hour 21 should be zero placeholder, got %+v", hourly[10])
	}
	if hourly[9].RequestSuccess == 111 {
		t.Fatal("row from 20260518 leaked into rolling window")
	}
	// 今天的桶走内存缓存：hour 9 从 DB 行加载。
	if hourly[22].Date != "20260520" || hourly[22].RequestSuccess != 2 {
		t.Fatalf("today hour 9 should carry cache data, got %+v", hourly[22])
	}
}

func TestRefreshCacheSkipsNonTodayHourlyStats(t *testing.T) {
	cleanupNow := SetTimeNowForTest(func() time.Time {
		return time.Date(2026, 5, 20, 10, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	})
	defer cleanupNow()

	setting.GetCache().Clear()
	setting.GetCache().Set(model.SettingKeyStatsTimezoneOffset, "8")

	testName := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", testName)
	if err := db.InitDB("sqlite", dsn, true); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer func() { _ = db.Close() }()

	dbConn := db.GetDB().WithContext(context.Background())
	if err := dbConn.Create(&model.StatsHourly{
		Hour:         9,
		Date:         "20260519",
		StatsMetrics: model.StatsMetrics{RequestSuccess: 99, InputToken: 999},
	}).Error; err != nil {
		t.Fatalf("create yesterday hourly: %v", err)
	}

	if err := RefreshCache(context.Background()); err != nil {
		t.Fatalf("refresh cache: %v", err)
	}

	hourly := HourlyGet()
	if len(hourly) != 11 {
		t.Fatalf("expected 11 hourly entries up to current hour, got %d", len(hourly))
	}
	if hourly[9].Date != "20260520" {
		t.Fatalf("expected hour 9 placeholder to use today's date, got %q", hourly[9].Date)
	}
	if hourly[9].RequestSuccess != 0 || hourly[9].InputToken != 0 {
		t.Fatalf("expected hour 9 placeholder to be empty, got %+v", hourly[9])
	}
}

func TestStatsHourlyCompositePrimaryKeyAllowsMultipleDatesPerHour(t *testing.T) {
	cleanupNow := SetTimeNowForTest(func() time.Time {
		return time.Date(2026, 5, 20, 10, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	})
	defer cleanupNow()

	setting.GetCache().Clear()
	setting.GetCache().Set(model.SettingKeyStatsTimezoneOffset, "8")

	testName := strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", testName)
	if err := db.InitDB("sqlite", dsn, true); err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer func() { _ = db.Close() }()

	dbConn := db.GetDB().WithContext(context.Background())
	entries := []model.StatsHourly{
		{Hour: 9, Date: "20260519", StatsMetrics: model.StatsMetrics{RequestSuccess: 1}},
		{Hour: 9, Date: "20260520", StatsMetrics: model.StatsMetrics{RequestSuccess: 2}},
	}
	for _, entry := range entries {
		if err := dbConn.Create(&entry).Error; err != nil {
			t.Fatalf("create hourly entry %+v: %v", entry, err)
		}
	}

	var count int64
	if err := dbConn.Model(&model.StatsHourly{}).Where("hour = ?", 9).Count(&count).Error; err != nil {
		t.Fatalf("count hourly entries: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 hourly entries for hour 9 across dates, got %d", count)
	}
}
