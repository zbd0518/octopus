package migrate

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// newDedupeSettingsDB 建一张仅含 settings(key,value) 的最小库，模拟存量设置行。
func newDedupeSettingsDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "dedupe.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := db.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`).Error; err != nil {
		t.Fatalf("create settings table: %v", err)
	}
	return db
}

func dedupeSettingValue(t *testing.T, db *gorm.DB, key string) (string, error) {
	t.Helper()
	var value string
	err := db.Raw(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value).Error
	return value, err
}

// 存量默认值 "false" 应被翻转为 "true"。
func TestMigrateModelNormalizeMarketDedupeDefault_FalseToTrue(t *testing.T) {
	db := newDedupeSettingsDB(t)
	if err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, "model_normalize_market_dedupe_default", "false").Error; err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := migrateModelNormalizeMarketDedupeDefault(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	got, err := dedupeSettingValue(t, db, "model_normalize_market_dedupe_default")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != "true" {
		t.Fatalf("value = %q, want true", got)
	}
}

// 用户已主动设为 "true"（或迁移已跑过）的行保持不变，幂等。
func TestMigrateModelNormalizeMarketDedupeDefault_AlreadyTrue(t *testing.T) {
	db := newDedupeSettingsDB(t)
	if err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, "model_normalize_market_dedupe_default", "true").Error; err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := migrateModelNormalizeMarketDedupeDefault(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	got, err := dedupeSettingValue(t, db, "model_normalize_market_dedupe_default")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != "true" {
		t.Fatalf("value = %q, want true", got)
	}
}

// 无该设置行（全新库）时迁移不报错，也不应创建行。
func TestMigrateModelNormalizeMarketDedupeDefault_MissingRow(t *testing.T) {
	db := newDedupeSettingsDB(t)
	if err := migrateModelNormalizeMarketDedupeDefault(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	got, err := dedupeSettingValue(t, db, "model_normalize_market_dedupe_default")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != "" {
		t.Fatalf("unexpected row created: value=%q", got)
	}
}
