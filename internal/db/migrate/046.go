package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 46,
		Up:      migratePlanProviderRefreshFields,
	})
}

// 046: 为 plan_providers 表增加自动刷新与增量快照字段
// （RefreshIntervalMin 单个覆盖间隔、LastBalance/LastQuotaUsed 上次刷新快照）。
// gorm AutoMigrate 也会加列，这里幂等兜底。
//
// 注意：本迁移原在 upstream/master 以 version 44 注册，但本仓库 version 44 已被
// 号池账号扩展迁移占用（见 044.go），故合并时重排到 46。
func migratePlanProviderRefreshFields(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.PlanProvider{}) {
		return nil
	}
	columns := []string{
		"RefreshIntervalMin",
		"LastBalance",
		"LastQuotaUsed",
	}
	for _, col := range columns {
		if !db.Migrator().HasColumn(&model.PlanProvider{}, col) {
			if err := db.Migrator().AddColumn(&model.PlanProvider{}, col); err != nil {
				return fmt.Errorf("add column %s: %w", col, err)
			}
		}
	}
	return nil
}
