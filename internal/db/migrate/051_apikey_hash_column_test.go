package migrate

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

// 051 迁移测试：api_keys 补 api_key_hash 列，幂等。
func TestMigrateAPIKeyHashColumn(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	gormDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	// 模拟旧版表（无 api_key_hash 列）
	if err := gormDB.Exec(`CREATE TABLE api_keys (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		api_key TEXT NOT NULL,
		enabled INTEGER DEFAULT 1,
		expire_at INTEGER,
		max_cost REAL,
		max_tokens INTEGER DEFAULT 0,
		supported_models TEXT,
		allowed_group_categories TEXT,
		rate_limit_rpm INTEGER DEFAULT 0,
		rate_limit_tpm INTEGER DEFAULT 0,
		per_model_quota_json TEXT,
		allowed_ips TEXT,
		tags TEXT,
		excluded_channels TEXT
	)`).Error; err != nil {
		t.Fatalf("create legacy table: %v", err)
	}

	// 第一遍：加列
	if err := migrateAPIKeyHashColumn(gormDB); err != nil {
		t.Fatalf("migrateAPIKeyHashColumn: %v", err)
	}
	if !gormDB.Migrator().HasColumn(&model.APIKey{}, "APIKeyHash") {
		t.Fatalf("api_keys missing api_key_hash column after migrate")
	}

	// 幂等：第二遍不应报错
	if err := migrateAPIKeyHashColumn(gormDB); err != nil {
		t.Fatalf("re-run migrateAPIKeyHashColumn: %v", err)
	}
}
