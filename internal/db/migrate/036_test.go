package migrate

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

// TestMigrateDropLeftoverSiteModelNameUniqueIndex 模拟 35 半成功：
// 新唯一索引已在、旧 CI 唯一索引仍在；36 应删掉旧索引且保留新索引。
func TestMigrateDropLeftoverSiteModelNameUniqueIndex(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "site-model-key-leftover.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	defer sqlDB.Close()

	if err := db.Exec(`
CREATE TABLE site_models (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  site_account_id INTEGER NOT NULL,
  group_key TEXT NOT NULL DEFAULT 'default',
  model_name TEXT NOT NULL,
  model_name_key TEXT NOT NULL DEFAULT '',
  source TEXT,
  route_type TEXT NOT NULL DEFAULT 'openai_chat',
  route_source TEXT NOT NULL DEFAULT 'sync_inferred',
  manual_override INTEGER DEFAULT 0,
  route_raw_payload TEXT,
  route_updated_at DATETIME,
  disabled INTEGER DEFAULT 0
)`).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	key := model.SiteModelNameKey("GLM-5.2")
	if err := db.Exec(`INSERT INTO site_models (site_account_id, group_key, model_name, model_name_key, source)
VALUES (1, 'default', 'GLM-5.2', ?, 'sync')`, key).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_site_account_group_model ON site_models(site_account_id, group_key, model_name)`).Error; err != nil {
		t.Fatalf("create old unique: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_site_account_group_model_key ON site_models(site_account_id, group_key, model_name_key)`).Error; err != nil {
		t.Fatalf("create new unique: %v", err)
	}

	if err := migrateDropLeftoverSiteModelNameUniqueIndex(db); err != nil {
		t.Fatalf("migrate 36: %v", err)
	}
	if db.Migrator().HasIndex(&model.SiteModel{}, "idx_site_account_group_model") {
		t.Fatal("leftover old unique index should be dropped by migration 36")
	}
	if !db.Migrator().HasIndex(&model.SiteModel{}, "idx_site_account_group_model_key") {
		t.Fatal("new unique index must remain")
	}

	// 幂等
	if err := migrateDropLeftoverSiteModelNameUniqueIndex(db); err != nil {
		t.Fatalf("second migrate 36: %v", err)
	}

	// 大小写变体可插入（raw SQL，避免依赖 master 新增的 price/perf 列）
	variantKey := model.SiteModelNameKey("glm-5.2")
	if err := db.Exec(`
INSERT INTO site_models (site_account_id, group_key, model_name, model_name_key, source, route_type, route_source)
VALUES (1, 'default', 'glm-5.2', ?, 'sync', 'openai_chat', 'sync_inferred')`, variantKey).Error; err != nil {
		t.Fatalf("insert case variant after 36: %v", err)
	}
}
