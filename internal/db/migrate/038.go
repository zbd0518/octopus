package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 38,
		Up:      migratePlanProviderFiveHour,
	})
}

// 038: 为 plan_providers 增加 5 小时窗口配额列（five_hour_total/five_hour_used/five_hour_reset_at）。
// 火山方舟 Agent Plan 返回 5h/周/月三档配额，此前仅持久化月（主）与周（次），5h 档被丢弃。
// gorm AutoMigrate 通常也会加列，这里幂等兜底，确保跨方言与运行时切换 DB 类型后列存在。
func migratePlanProviderFiveHour(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.PlanProvider{}) {
		return nil
	}
	for _, col := range []string{"FiveHourTotal", "FiveHourUsed", "FiveHourResetAt"} {
		if db.Migrator().HasColumn(&model.PlanProvider{}, col) {
			continue
		}
		if err := db.Migrator().AddColumn(&model.PlanProvider{}, col); err != nil {
			return fmt.Errorf("add plan_providers.%s: %w", col, err)
		}
	}
	return nil
}
