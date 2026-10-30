package migrate

import (
	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{Version: 62, Up: func(db *gorm.DB) error {
		return db.AutoMigrate(&model.Node{})
	}})
}
