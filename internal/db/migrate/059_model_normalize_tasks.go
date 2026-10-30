package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{Version: 59, Up: ensureModelNormalizeTasks})
}

func ensureModelNormalizeTasks(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if db.Migrator().HasTable(&model.ModelNormalizeTask{}) {
		return nil
	}
	if err := db.AutoMigrate(&model.ModelNormalizeTask{}); err != nil {
		return fmt.Errorf("create model normalization tasks table: %w", err)
	}
	return nil
}
