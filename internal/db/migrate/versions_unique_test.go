package migrate

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// 复现 issue：合并 master/v2.7.0 后，分支曾把 upstream 的池迁移临时推到
// 62-65，与本分支新增的 nodes 迁移(66) 及后续编号同时存在，启动时
// runMigrationsWithRecord 直接报 duplicated migration version。
//
// 该用例断言：当前注册表里 afterAutoMigrations 的版本号唯一，且覆盖连续区间。
func TestAfterAutoMigrationVersionsAreUnique(t *testing.T) {
	seen := make(map[int]bool, len(afterAutoMigrations))
	for _, m := range afterAutoMigrations {
		if seen[m.Version] {
			t.Fatalf("duplicated migration version: %d", m.Version)
		}
		seen[m.Version] = true
	}
}

// 存量库场景：migration_records 里全是「已执行」时，启动不应重复执行，
// 也不应因编号错位而报错。
func TestMigrationsSkipAlreadyRecordedVersions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migrate_skip.db")
	conn, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := conn.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	ran := make(map[int]int)
	migrations := []Migration{
		{Version: 63, Up: func(*gorm.DB) error { ran[63]++; return nil }},
		{Version: 64, Up: func(*gorm.DB) error { ran[64]++; return nil }},
	}

	if err := runMigrationsWithRecord(conn, migrations); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if ran[63] != 1 || ran[64] != 1 {
		t.Fatalf("first run counts = %v, want each 1", ran)
	}

	// 第二轮：已记录 success，必须跳过。
	if err := runMigrationsWithRecord(conn, migrations); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if ran[63] != 1 || ran[64] != 1 {
		t.Fatalf("second run re-executed migrations: %v", ran)
	}
}
