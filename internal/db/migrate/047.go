package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 47,
		Up:      migratePlanProviderTotalUsed,
	})
}

// 047: 为 plan_providers 表增加累计已用额度字段（TotalUsed）。
// gorm AutoMigrate 也会加列，这里幂等兜底。
//
// 注意：本迁移原在 upstream/master 以 version 45 注册，但本仓库 version 45 已被
// 号池账号生命周期迁移占用（见 045.go），故合并时重排到 47。
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
