package migrate

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func TestMigrateSiteModelNameKeyBackfillAndIndex(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "site-model-key.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	defer sqlDB.Close()

	// 模拟旧表：唯一索引在 (site_account_id, group_key, model_name)，无 model_name_key。
	if err := db.Exec(`
CREATE TABLE site_models (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  site_account_id INTEGER NOT NULL,
  group_key TEXT NOT NULL DEFAULT 'default',
  model_name TEXT NOT NULL,
  source TEXT,
  route_type TEXT NOT NULL DEFAULT 'openai_chat',
  route_source TEXT NOT NULL DEFAULT 'sync_inferred',
  manual_override INTEGER DEFAULT 0,
  route_raw_payload TEXT,
  route_updated_at DATETIME,
  disabled INTEGER DEFAULT 0
)`).Error; err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX idx_site_account_group_model ON site_models(site_account_id, group_key, model_name)`).Error; err != nil {
		t.Fatalf("create legacy unique index: %v", err)
	}
	if err := db.Exec(`INSERT INTO site_models (site_account_id, group_key, model_name, source) VALUES (1, 'default', 'GLM-5.2', 'sync')`).Error; err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	if err := migrateSiteModelNameKey(db); err != nil {
		t.Fatalf("migrateSiteModelNameKey: %v", err)
	}

	if !db.Migrator().HasColumn(&model.SiteModel{}, "ModelNameKey") {
		t.Fatal("expected model_name_key column")
	}
	if db.Migrator().HasIndex(&model.SiteModel{}, "idx_site_account_group_model") {
		t.Fatal("legacy unique index should be dropped")
	}
	if !db.Migrator().HasIndex(&model.SiteModel{}, "idx_site_account_group_model_key") {
		t.Fatal("expected new unique index idx_site_account_group_model_key")
	}

	var row model.SiteModel
	if err := db.First(&row, "site_account_id = ?", 1).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	want := model.SiteModelNameKey("GLM-5.2")
	if row.ModelNameKey != want {
		t.Fatalf("backfill key = %q, want %q", row.ModelNameKey, want)
	}

	// 同组大小写变体应可并存
	variant := model.SiteModel{
		SiteAccountID: 1,
		GroupKey:      model.SiteDefaultGroupKey,
		ModelName:     "glm-5.2",
		Source:        "sync",
		RouteType:     model.SiteModelRouteTypeOpenAIChat,
		RouteSource:   model.SiteModelRouteSourceSyncInferred,
	}
	variant.EnsureModelNameKey()
	if err := db.Create(&variant).Error; err != nil {
		t.Fatalf("insert case variant should succeed after migration: %v", err)
	}

	// 幂等
	if err := migrateSiteModelNameKey(db); err != nil {
		t.Fatalf("second migrateSiteModelNameKey: %v", err)
	}
}
