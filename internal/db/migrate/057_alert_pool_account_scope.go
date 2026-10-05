package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 61,
		Up:      migrateAlertRulePoolAccountScope,
	})
}

// 057: alert_rules gains the ScopePoolAccountID scope column (guide card
// B4-#15), following the dedicated-column-per-dimension convention of the
// four existing scope columns (migration 029 shape). AlertRule IS part of the
// main AutoMigrate list, so AutoMigrate would add the column on its own; the
// migration is recorded explicitly for the customary versioned history and
// for databases where AutoMigrate order matters. Idempotent via HasColumn.
func migrateAlertRulePoolAccountScope(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.AlertRule{}) {
		return nil
	}
	if db.Migrator().HasColumn(&model.AlertRule{}, "ScopePoolAccountID") {
		return nil
	}
	if err := db.Migrator().AddColumn(&model.AlertRule{}, "ScopePoolAccountID"); err != nil {
		return fmt.Errorf("add alert_rules.scope_pool_account_id: %w", err)
	}
	return nil
}
