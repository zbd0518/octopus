package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 64,
		Up:      migratePlanProviderTotalUsed,
	})
}

// 064: 为 plan_providers 表增加累计已用额度字段（TotalUsed）。
// gorm AutoMigrate 也会加列，这里幂等兜底。
//
// 注意：本迁移在 upstream/master 以 version 45 注册，但本仓库 45 已被号池账号生命周期
// 迁移占用（见 045.go），故顺延到 64（其后 48-62 与 upstream 保持一致）。
func migratePlanProviderTotalUsed(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.PlanProvider{}) {
		return nil
	}
	if !db.Migrator().HasColumn(&model.PlanProvider{}, "TotalUsed") {
		if err := db.Migrator().AddColumn(&model.PlanProvider{}, "TotalUsed"); err != nil {
			return fmt.Errorf("add column TotalUsed: %w", err)
		}
	}
	return nil
}
