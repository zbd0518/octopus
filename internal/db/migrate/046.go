package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 46,
		Up:      migratePlanProviderLoginCredentials,
	})
}

// 046: 为 plan_providers 表增加商汤日日新账号密码自动登录相关字段。
// gorm AutoMigrate 也会加列，这里幂等兜底。
func migratePlanProviderLoginCredentials(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.PlanProvider{}) {
		return nil
	}
	columns := []struct {
		name string
		typ  string
	}{
		{"LoginUsername", "varchar(191) DEFAULT ''"},
		{"LoginPasswordEnc", "text"},
		{"RefreshTokenEnc", "text"},
	}
	for _, col := range columns {
		if !db.Migrator().HasColumn(&model.PlanProvider{}, col.name) {
			if err := db.Migrator().AddColumn(&model.PlanProvider{}, col.name); err != nil {
				return fmt.Errorf("add column %s: %w", col.name, err)
			}
		}
	}
	return nil
}
