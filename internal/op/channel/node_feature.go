package channel

import (
	"context"
	"strconv"

	"github.com/lingyuins/octopus/internal/model"
	nodeop "github.com/lingyuins/octopus/internal/op/node"
	"github.com/lingyuins/octopus/internal/op/setting"
	pluginevent "github.com/lingyuins/octopus/internal/plugin/event"
	"gorm.io/gorm"
)

// SetNodesFeatureEnabled atomically pauses/resumes node channels with the setting.
// Keep the binding lock until disconnection finishes so re-enabling cannot publish
// a new session just before the previous disable removes it.
func SetNodesFeatureEnabled(ctx context.Context, enabled bool) error {
	return nodeop.WithBindingLock(func() error {
		runtimeUpdateLock.Lock()
		defer runtimeUpdateLock.Unlock()

		var changed []model.Channel
		values := map[model.SettingKey]string{model.SettingKeyNodesFeatureEnabled: strconv.FormatBool(enabled)}
		if err := setting.SetStringBatchWithUpdate(ctx, values, func(tx *gorm.DB) error {
			query := tx.Model(&model.Channel{}).Where("node_id > 0")
			if enabled {
				query = query.Where("node_feature_paused = ?", true)
			} else {
				query = query.Where("enabled = ?", true)
			}
			if err := query.Select("id", "name").Find(&changed).Error; err != nil {
				return err
			}
			if len(changed) == 0 {
				return nil
			}
			ids := make([]int, 0, len(changed))
			for _, item := range changed {
				ids = append(ids, item.ID)
			}
			return tx.Model(&model.Channel{}).Where("id IN ?", ids).
				Updates(map[string]any{"enabled": enabled, "node_feature_paused": !enabled}).Error
		}); err != nil {
			return err
		}

		for _, item := range changed {
			if cached, ok := chCache.Get(item.ID); ok {
				cached.Enabled = enabled
				cached.NodeFeaturePaused = !enabled
				chCache.Set(item.ID, cached)
			}
			pluginevent.Publish("channel.health.changed", map[string]any{"channel_id": item.ID, "channel_name": item.Name, "enabled": enabled, "passed": enabled})
		}
		if !enabled && nodeop.DisconnectAllFunc != nil {
			nodeop.DisconnectAllFunc()
		}
		return nil
	})
}
