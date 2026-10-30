package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 49,
		Up:      migrateGroupEndpointTypeAllToChat,
	})
}

// 049: 把存量 endpoint_type='*'（"全部"分类）的 group 统一迁移为 'chat'。
// 移除前端"全部"分类选项后，group 的 endpoint_type 不再接受 '*'，
// 存量值为 '*' 的 group 需回填为 'chat'（与 NormalizeEndpointType 空值回退
// 及前端默认值一致）。幂等：只更新 endpoint_type='*' 的行，重复执行安全。
func migrateGroupEndpointTypeAllToChat(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.Group{}) {
		return nil
	}
	return db.Model(&model.Group{}).
		Where("endpoint_type = ?", model.EndpointTypeAll).
		Update("endpoint_type", model.EndpointTypeChat).Error
}
