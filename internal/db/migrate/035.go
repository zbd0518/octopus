package migrate

import (
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 35,
		Up:      migrateRelayLogReasoningMetrics,
	})
}

// 035: 为 relay_logs 增加 reasoning_effort / reasoning_tokens / reasoning_chars 列。
// reasoning_effort 记录出站最终思考强度；
// reasoning_tokens 记录上游 usage 中的官方思考 token；
// reasoning_chars 在无官方 token 时记录思考文本字符数（UTF-8 rune）作为展示回退。
// gorm AutoMigrate 通常也会加列，这里幂等兜底，确保跨方言与运行时切换 DB 类型后列存在。
func migrateRelayLogReasoningMetrics(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.RelayLog{}) {
		return nil
	}
	for _, col := range []string{"ReasoningEffort", "ReasoningTokens", "ReasoningChars"} {
		if db.Migrator().HasColumn(&model.RelayLog{}, col) {
			continue
		}
		if err := db.Migrator().AddColumn(&model.RelayLog{}, col); err != nil {
			return fmt.Errorf("add relay_logs.%s: %w", col, err)
		}
	}
	return nil
}
