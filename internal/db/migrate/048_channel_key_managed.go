package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 48,
		Up:      migrateChannelKeyManaged,
	})
}

// 048: channel_keys 表增加 managed 列。
// site 同步投影为区分"自动生成的 key"与"用户手动添加的 key"，给
// ChannelKey 增加 Managed bool 字段。存量 key 一律置 false（视为手动
// key），之后的同步会重新写入 Managed=true 的 key；diff 时只删除
// Managed=true 的 key，保留用户手动添加的 key 不被清除。幂等：
// HasColumn 守卫，重复执行安全。
func migrateChannelKeyManaged(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if db.Migrator().HasTable(&model.ChannelKey{}) {
		if !db.Migrator().HasColumn(&model.ChannelKey{}, "Managed") {
			if err := db.Migrator().AddColumn(&model.ChannelKey{}, "Managed"); err != nil {
				return fmt.Errorf("add column channel_keys.managed: %w", err)
			}
		}
	}
	return nil
}
