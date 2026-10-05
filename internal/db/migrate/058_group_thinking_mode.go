package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{Version: 58, Up: addGroupThinkingMode})
}

func addGroupThinkingMode(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.Group{}) || db.Migrator().HasColumn(&model.Group{}, "ThinkingMode") {
		return nil
	}
	if err := db.Migrator().AddColumn(&model.Group{}, "ThinkingMode"); err != nil {
		return fmt.Errorf("add groups.thinking_mode: %w", err)
	}
	return nil
}
