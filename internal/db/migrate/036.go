package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 36,
		Up:      addReasoningBufferStrategyToGroups,
	})
}

// 036: 为 groups 增加 reasoning_buffer_strategy 列（issue #155 Cloudflare 超时问题）。
// "" = 使用全局设置；"buffer" = 缓冲直到可见内容（安全重试但 CF 可能超时）；
// "immediate" = 立即流式发送（实时体验但空输出不可重试）。
func addReasoningBufferStrategyToGroups(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.Group{}) {
		return nil
	}
	if db.Migrator().HasColumn(&model.Group{}, "ReasoningBufferStrategy") {
		return nil
	}
	if err := db.Migrator().AddColumn(&model.Group{}, "ReasoningBufferStrategy"); err != nil {
		return fmt.Errorf("add groups.reasoning_buffer_strategy: %w", err)
	}
	return nil
}
