package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	// 清理 migration 41 半成功残留：新唯一索引已建、旧 CI 唯一索引仍在。
	// 若 41 已标 success 但旧索引未删，同步仍会撞 idx_site_account_group_model。
	RegisterAfterAutoMigration(Migration{
		Version: 42,
		Up:      migrateBoth042,
	})
}

// migrateBoth042 合并两个 migration 42：
// 1. 删除 site_models 上残留的旧唯一索引（本地分支）
// 2. 为 plan_providers 增加 team_organization_id / team_project_id 列（upstream）
func migrateBoth042(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	// Part 1: 清理 site_models 旧索引
	if db.Migrator().HasTable(&model.SiteModel{}) {
		// 没有 model_name_key 列则说明 36 未生效，交给 36 处理；此处不强行操作。
		if db.Migrator().HasColumn(&model.SiteModel{}, "ModelNameKey") {
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
		}
	}

	// Part 2: 为 plan_providers 增加 team_organization_id / team_project_id 列
	if db.Migrator().HasTable(&model.PlanProvider{}) {
		if !db.Migrator().HasColumn(&model.PlanProvider{}, "TeamOrganizationID") {
			if err := db.Migrator().AddColumn(&model.PlanProvider{}, "TeamOrganizationID"); err != nil {
				return fmt.Errorf("add column team_organization_id: %w", err)
			}
		}
		if !db.Migrator().HasColumn(&model.PlanProvider{}, "TeamProjectID") {
			if err := db.Migrator().AddColumn(&model.PlanProvider{}, "TeamProjectID"); err != nil {
				return fmt.Errorf("add column team_project_id: %w", err)
			}
		}
	}

	return nil
}
