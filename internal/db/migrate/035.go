package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	// 清理 migration 34 半成功残留：新唯一索引已建、旧 CI 唯一索引仍在。
	// 若 34 已标 success 但旧索引未删，同步仍会撞 idx_site_account_group_model。
	RegisterAfterAutoMigration(Migration{
		Version: 35,
		Up:      migrateDropLeftoverSiteModelNameUniqueIndex,
	})
}

// 035: 删除 site_models 上残留的旧唯一索引 idx_site_account_group_model。
//
// 幂等：新索引不存在时先补建；旧索引不存在则 no-op。
// MySQL 删除时临时关闭 FOREIGN_KEY_CHECKS，避免 Error 1553。
func migrateDropLeftoverSiteModelNameUniqueIndex(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.SiteModel{}) {
		return nil
	}

	// 没有 model_name_key 列则说明 34 未生效，交给 34 处理；此处不强行操作。
	if !db.Migrator().HasColumn(&model.SiteModel{}, "ModelNameKey") {
		return nil
	}

	// 确保新唯一索引存在，再删旧索引。
	if !db.Migrator().HasIndex(&model.SiteModel{}, "idx_site_account_group_model_key") {
		// 回填空 key，避免建唯一索引时撞空串。
		if err := backfillSiteModelNameKeys(db); err != nil {
			return err
		}
		if err := db.Migrator().CreateIndex(&model.SiteModel{}, "idx_site_account_group_model_key"); err != nil {
			return fmt.Errorf("create site_models idx_site_account_group_model_key: %w", err)
		}
	}

	if err := dropSiteModelOldUniqueIndexes(db); err != nil {
		return err
	}
	return nil
}
