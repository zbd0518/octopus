package backup

import (
	"fmt"
	"strings"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/nodetransport"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
	"github.com/lingyuins/octopus/internal/utils/crypto"
	"gorm.io/gorm"
)

func validateNodeRestore(conn *gorm.DB, dump *model.DBDump, full bool) error {
	ids := make(map[int]bool, len(dump.Nodes))
	for _, n := range dump.Nodes {
		if n.ID <= 0 || ids[n.ID] {
			return fmt.Errorf("import nodes: invalid or duplicate node ID")
		}
		ids[n.ID] = true
	}
	channelIDs := make(map[int]bool)
	allChannelIDs := make(map[int]int, len(dump.Channels))
	for _, ch := range dump.Channels {
		allChannelIDs[ch.ID]++
		if ch.NodeID < 0 || ch.NodeID > 0 && !ids[ch.NodeID] {
			return fmt.Errorf("import channels: node reference missing from backup")
		}
		if ch.NodeID > 0 {
			if ch.ID <= 0 {
				return fmt.Errorf("import channels: invalid node channel ID")
			}
			channelIDs[ch.ID] = true
		}
	}
	for id := range channelIDs {
		if allChannelIDs[id] != 1 {
			return fmt.Errorf("import channels: duplicate node channel ID")
		}
	}
	if err := normalizeRestoredNodeChannels(dump); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	if !conn.Migrator().HasTable(&model.Node{}) {
		return fmt.Errorf("import nodes: target table missing; migrate database first")
	}
	if full {
		return nil
	}
	// Skipping conflicting IDs would silently bind restored channels to another
	// connector. Incremental imports must instead be explicitly reconciled.
	var existing []model.Node
	if err := conn.Select("id").Find(&existing).Error; err != nil {
		return err
	}
	for _, n := range existing {
		if ids[n.ID] {
			return fmt.Errorf("import nodes: node ID conflicts with target; use full restore or reconcile IDs")
		}
	}
	for id := range channelIDs {
		var count int64
		if err := conn.Model(&model.Channel{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("import channels: node channel ID conflicts with target")
		}
		if err := conn.Model(&model.ChannelKey{}).Where("channel_id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("import channel_keys: target already references restored node channel ID")
		}
	}
	// Also reject orphan target channels that would become attached to a newly
	// restored connector merely because their old numeric ID happens to match.
	if conn.Migrator().HasTable(&model.Channel{}) && conn.Migrator().HasColumn(&model.Channel{}, "node_id") {
		keys := make([]int, 0, len(ids))
		for id := range ids {
			keys = append(keys, id)
		}
		var count int64
		if err := conn.Model(&model.Channel{}).Where("node_id IN ?", keys).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("import nodes: target channels already reference restored node IDs")
		}
	}
	return nil
}

// Node backups carry identity, never network destinations or cloud credentials.
// Channel.Keys is a GORM association as well as ChannelKeys being a dump table;
// remove both input paths before materializing exactly one local placeholder.
func normalizeRestoredNodeChannels(dump *model.DBDump) error {
	ids := make(map[int]bool)
	for i := range dump.Channels {
		ch := &dump.Channels[i]
		if ch.NodeID == 0 {
			continue
		}
		switch ch.Type {
		case outbound.OutboundTypeOpenAIChat, outbound.OutboundTypeOpenAIResponse, outbound.OutboundTypeAnthropic, outbound.OutboundTypeOpenAIEmbedding:
		default:
			return fmt.Errorf("import channels: unsupported node channel protocol")
		}
		if ch.PoolID != 0 || ch.Proxy || ch.ProxyConfigID != nil || (ch.ProxyMode != "" && ch.ProxyMode != model.ProxyUsageModeDirect) || (ch.ChannelProxy != nil && strings.TrimSpace(*ch.ChannelProxy) != "") {
			return fmt.Errorf("import channels: node channel cannot use a pool or proxy")
		}
		ids[ch.ID] = true
		ch.BaseUrls = []model.BaseUrl{{URL: nodetransport.BaseURL, SuffixMode: "none"}}
		ch.ProxyMode = model.ProxyUsageModeDirect
		ch.Proxy = false
		ch.ProxyConfigID = nil
		ch.ChannelProxy = nil
		ch.Keys = nil
	}
	keys := make([]model.ChannelKey, 0, len(dump.ChannelKeys)+len(ids))
	for _, key := range dump.ChannelKeys {
		if !ids[key.ChannelID] {
			keys = append(keys, key)
		}
	}
	for _, ch := range dump.Channels {
		if !ids[ch.ID] {
			continue
		}
		placeholder, err := crypto.Encrypt("octopus-node-local-credential")
		if err != nil {
			return fmt.Errorf("import channel_keys: encrypt node placeholder: %w", err)
		}
		keys = append(keys, model.ChannelKey{ChannelID: ch.ID, Enabled: true, ChannelKey: placeholder})
	}
	dump.ChannelKeys = keys
	return nil
}
