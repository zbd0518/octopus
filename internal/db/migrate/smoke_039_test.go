package migrate

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

// 039 迁移测试：plan_providers 补 proxy_mode / proxy_config_id 两列，幂等。
func TestMigratePlanProviderProxyFields(t *testing.T) {
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

	// 模拟旧版表（无代理列）
	if err := gormDB.Exec(`CREATE TABLE plan_providers (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		category TEXT NOT NULL,
		provider_type TEXT NOT NULL DEFAULT 'balance',
		api_key TEXT NOT NULL,
		base_url TEXT NOT NULL,
		channel_id INTEGER NOT NULL DEFAULT 0
	)`).Error; err != nil {
		t.Fatalf("create legacy table: %v", err)
	}

	// 第一遍：加列
	if err := migratePlanProviderProxyFields(gormDB); err != nil {
		t.Fatalf("migratePlanProviderProxyFields: %v", err)
	}
	for _, col := range []string{"ProxyMode", "ProxyConfigID"} {
		if !gormDB.Migrator().HasColumn(&model.PlanProvider{}, col) {
			t.Fatalf("plan_providers missing column %s after migrate", col)
		}
	}

	// 幂等：第二遍不应报错
	if err := migratePlanProviderProxyFields(gormDB); err != nil {
		t.Fatalf("re-run migratePlanProviderProxyFields: %v", err)
	}
}
