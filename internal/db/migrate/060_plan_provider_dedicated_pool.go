package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 60,
		Up:      migratePlanProviderDedicatedPool,
	})
}

// 060: 为 plan_providers 增加商汤日日新专用积分池的 5 小时/7 天窗口字段。
// GORM AutoMigrate 通常也会加列，这里幂等兜底，确保旧库升级和运行时切换数据库类型都可用。
func migratePlanProviderDedicatedPool(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.PlanProvider{}) {
		return nil
	}
	columns := []string{
		"DedicatedPoolName",
		"DedicatedWeeklyTotal",
		"DedicatedWeeklyUsed",
		"DedicatedWeeklyResetAt",
		"DedicatedFiveHourTotal",
		"DedicatedFiveHourUsed",
		"DedicatedFiveHourResetAt",
	}
	for _, col := range columns {
		if db.Migrator().HasColumn(&model.PlanProvider{}, col) {
			continue
		}
		if err := db.Migrator().AddColumn(&model.PlanProvider{}, col); err != nil {
			return fmt.Errorf("add plan_providers.%s: %w", col, err)
		}
	}
	return nil
}
