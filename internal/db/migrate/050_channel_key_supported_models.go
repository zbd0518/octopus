package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 50,
		Up:      migrateChannelKeySupportedModels,
	})
}

// 050: channel_keys 表增加 supported_models 列。
// 给 ChannelKey 增加 SupportedModels 字段（逗号分隔的模型列表），用于 key 级
// 模型过滤：当 key 的 SupportedModels 非空时，只在该 key 支持的模型上选用它，
// 避免把不支持当前模型的 key 发给上游（如上游中转站某 token 无某模型权限，
// 发过去上游回 model_not_found 503）。空表示不限制（兼容存量 key）。幂等：
// HasColumn 守卫，重复执行安全。
func migrateChannelKeySupportedModels(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if db.Migrator().HasTable(&model.ChannelKey{}) {
		if !db.Migrator().HasColumn(&model.ChannelKey{}, "SupportedModels") {
			if err := db.Migrator().AddColumn(&model.ChannelKey{}, "SupportedModels"); err != nil {
				return fmt.Errorf("add column channel_keys.supported_models: %w", err)
			}
		}
	}
	return nil
}
