package migrate

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func openStatsMigrateDB(t *testing.T, name string) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), name)
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func listColumns(t *testing.T, db *gorm.DB, table string) map[string]bool {
	t.Helper()
	cols, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		t.Fatalf("column types of %s: %v", table, err)
	}
	set := make(map[string]bool, len(cols))
	for _, c := range cols {
		set[c.Name()] = true
	}
	return set
}

// TestMigrateStatsMetricsLatencyColumnsIdempotent 验证 038 在完整 schema 上幂等，
// 且直方图列使用正确的数字下划线命名。
func TestMigrateStatsMetricsLatencyColumnsIdempotent(t *testing.T) {
	db := openStatsMigrateDB(t, "stats-metrics-full.db")

	if err := db.AutoMigrate(
		&model.StatsTotal{},
		&model.StatsDaily{},
		&model.StatsHourly{},
		&model.StatsModel{},
		&model.StatsChannel{},
		&model.StatsAPIKey{},
		&model.StatsDailyChannel{},
		&model.StatsDailyModel{},
		&model.StatsDailyAPIKey{},
		&model.StatsDailyChannelModel{},
		&model.StatsSiteModelHourly{},
	); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	if err := migrateStatsMetricsLatencyColumns(db); err != nil {
		t.Fatalf("migrateStatsMetricsLatencyColumns: %v", err)
	}
	if err := migrateStatsMetricsLatencyColumns(db); err != nil {
		t.Fatalf("migrateStatsMetricsLatencyColumns second run: %v", err)
	}

	cols := listColumns(t, db, "stats_daily_channels")
	for _, col := range []string{
		"latency_p50", "latency_p95", "latency_p99",
		"ftut_avg", "ftut_p50", "ftut_p95", "ftut_p99",
		"histogram_lt_100", "histogram_100_500", "histogram_500_1k",
		"histogram_1k_5k", "histogram_gt_5k",
	} {
		if !cols[col] {
			t.Fatalf("expected column %q on stats_daily_channels, got %v", col, cols)
		}
	}
	// 不应残留 GORM 默认错误命名
	for _, bad := range []string{
		"histogram_lt100", "histogram100to500", "histogram500to1k",
		"histogram1kto5k", "histogram_gt5k",
	} {
		if cols[bad] {
			t.Fatalf("unexpected legacy column %q still present", bad)
		}
	}
}

// TestMigrateStatsMetricsLatencyColumnsOnLegacyTable 验证旧库（无 latency/histogram）能补列。
func TestMigrateStatsMetricsLatencyColumnsOnLegacyTable(t *testing.T) {
	db := openStatsMigrateDB(t, "stats-metrics-legacy.db")

	if err := db.Exec(`CREATE TABLE stats_daily_channels (
		date TEXT NOT NULL,
		channel_id INTEGER NOT NULL,
		channel_name TEXT,
		input_token INTEGER DEFAULT 0,
		output_token INTEGER DEFAULT 0,
		input_cost REAL DEFAULT 0,
		output_cost REAL DEFAULT 0,
		wait_time INTEGER DEFAULT 0,
		request_success INTEGER DEFAULT 0,
		request_failed INTEGER DEFAULT 0,
		PRIMARY KEY (date, channel_id)
	)`).Error; err != nil {
		t.Fatalf("create legacy stats_daily_channels: %v", err)
	}

	if err := migrateStatsMetricsLatencyColumns(db); err != nil {
		t.Fatalf("migrateStatsMetricsLatencyColumns on legacy: %v", err)
	}

	cols := listColumns(t, db, "stats_daily_channels")
	for _, col := range []string{
		"latency_p50", "histogram_lt_100", "histogram_100_500",
		"histogram_500_1k", "histogram_1k_5k", "histogram_gt_5k",
		"ftut_avg", "ftut_p50", "ftut_p95", "ftut_p99",
	} {
		if !cols[col] {
			t.Fatalf("expected column %q after migrate, got %v", col, cols)
		}
	}
}

// TestMigrateStatsMetricsLatencyColumnsRenamesWrongNames 验证把 GORM 默认错误列名
// 重命名为 upsert/Redis 使用的正确列名，并保留数据。
func TestMigrateStatsMetricsLatencyColumnsRenamesWrongNames(t *testing.T) {
	db := openStatsMigrateDB(t, "stats-metrics-wrong-names.db")

	if err := db.Exec(`CREATE TABLE stats_daily_channels (
		date TEXT NOT NULL,
		channel_id INTEGER NOT NULL,
		channel_name TEXT,
		input_token INTEGER DEFAULT 0,
		output_token INTEGER DEFAULT 0,
		input_cost REAL DEFAULT 0,
		output_cost REAL DEFAULT 0,
		wait_time INTEGER DEFAULT 0,
		request_success INTEGER DEFAULT 0,
		request_failed INTEGER DEFAULT 0,
		latency_p50 INTEGER DEFAULT 0,
		histogram_lt100 INTEGER DEFAULT 0,
		histogram100to500 INTEGER DEFAULT 0,
		histogram500to1k INTEGER DEFAULT 0,
		histogram1kto5k INTEGER DEFAULT 0,
		histogram_gt5k INTEGER DEFAULT 0,
		PRIMARY KEY (date, channel_id)
	)`).Error; err != nil {
		t.Fatalf("create wrong-name table: %v", err)
	}
	if err := db.Exec(`INSERT INTO stats_daily_channels
		(date, channel_id, channel_name, histogram_lt100, histogram100to500)
		VALUES ('20260722', 1, 'ch', 7, 3)`).Error; err != nil {
		t.Fatalf("insert seed: %v", err)
	}

	if err := migrateStatsMetricsLatencyColumns(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cols := listColumns(t, db, "stats_daily_channels")
	if !cols["histogram_lt_100"] || !cols["histogram_100_500"] {
		t.Fatalf("expected renamed columns, got %v", cols)
	}
	for _, bad := range []string{"histogram_lt100", "histogram100to500"} {
		if cols[bad] {
			t.Fatalf("legacy column %q should be renamed away", bad)
		}
	}

	var lt100, h100 int64
	if err := db.Raw(`SELECT histogram_lt_100, histogram_100_500 FROM stats_daily_channels WHERE channel_id=1`).
		Row().Scan(&lt100, &h100); err != nil {
		t.Fatalf("scan renamed values: %v", err)
	}
	if lt100 != 7 || h100 != 3 {
		t.Fatalf("expected preserved values 7/3, got %d/%d", lt100, h100)
	}
}
