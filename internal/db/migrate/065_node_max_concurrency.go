package migrate

import (
	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

// 065: 为 nodes 表补齐最大并发等字段（AutoMigrate 幂等兜底）。
//
// 注意：upstream/master 以 version 63 注册本迁移，但本仓库 63 已被
// plan_providers 自动刷新字段迁移占用（见 063.go），64 亦被 064.go 占用，
// 故顺延到 65，避免合并后 duplicated migration version 启动失败。
func init() {
	RegisterAfterAutoMigration(Migration{Version: 65, Up: func(db *gorm.DB) error {
		return db.AutoMigrate(&model.Node{})
	}})
}
