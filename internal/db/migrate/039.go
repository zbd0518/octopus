package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 39,
		Up:      migratePlanProviderProxyFields,
	})
}

// 039: 为 plan_providers 增加 proxy_mode / proxy_config_id 列
// （ChatGPT Codex 套餐支持代理池选择：chatgpt.com 国内不可直连，
// 添加/刷新查询与自动创建的转发渠道都需要可选代理）。
// GORM AutoMigrate 通常也会加列，这里幂等兜底，确保跨方言
// （SQLite/MySQL/Postgres）以及运行时切换 DB 类型后该列存在。
func migratePlanProviderProxyFields(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.PlanProvider{}) {
		return nil
	}
	if !db.Migrator().HasColumn(&model.PlanProvider{}, "ProxyMode") {
		if err := db.Migrator().AddColumn(&model.PlanProvider{}, "ProxyMode"); err != nil {
			return fmt.Errorf("add column proxy_mode: %w", err)
		}
	}
	if !db.Migrator().HasColumn(&model.PlanProvider{}, "ProxyConfigID") {
		if err := db.Migrator().AddColumn(&model.PlanProvider{}, "ProxyConfigID"); err != nil {
			return fmt.Errorf("add column proxy_config_id: %w", err)
		}
	}
	return nil
}
