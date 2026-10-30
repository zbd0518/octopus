package migrate

import (
	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{Version: 61, Up: func(db *gorm.DB) error {
		return db.AutoMigrate(&model.Plugin{}, &model.PluginKV{}, &model.PluginSecret{})
	}})
}
