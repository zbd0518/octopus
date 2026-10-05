package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 63,
		Up:      migratePoolUnschedRules,
	})
}

// 055: pool temp-unsched rules table (guide card B4-#12).
// pool_unsched_rules is not part of the main AutoMigrate list, so the table
// is created explicitly with a HasTable idempotent guard (040.go pattern).
func migratePoolUnschedRules(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.PoolUnschedRule{}) {
		if err := db.Migrator().CreateTable(&model.PoolUnschedRule{}); err != nil {
			return fmt.Errorf("create pool_unsched_rules: %w", err)
		}
	}
	return nil
}
