package channel

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/nodetransport"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
)

var ErrInvalidNodeChannel = errors.New("invalid node channel")

const NodeChannelKey = "octopus-node-local-credential"

func normalizeNodeChannel(ch *model.Channel, ctx context.Context) error {
	if ch.NodeID < 0 {
		return fmt.Errorf("%w: node id must be positive", ErrInvalidNodeChannel)
	}
	if ch.NodeID == 0 {
		return nil
	}
	switch ch.Type {
	case outbound.OutboundTypeOpenAIChat, outbound.OutboundTypeOpenAIResponse, outbound.OutboundTypeAnthropic, outbound.OutboundTypeOpenAIEmbedding:
	default:
		return fmt.Errorf("%w: unsupported protocol", ErrInvalidNodeChannel)
	}
	if ch.PoolID != 0 || ch.Proxy || ch.ProxyConfigID != nil || (ch.ProxyMode != "" && ch.ProxyMode != model.ProxyUsageModeDirect) || (ch.ChannelProxy != nil && strings.TrimSpace(*ch.ChannelProxy) != "") {
		return fmt.Errorf("%w: node channels cannot use a pool or proxy", ErrInvalidNodeChannel)
	}
	var count int64
	if err := db.GetDB().WithContext(ctx).Model(&model.Node{}).Where("id = ?", ch.NodeID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("%w: node not found", ErrInvalidNodeChannel)
	}
	ch.BaseUrls = []model.BaseUrl{{URL: nodetransport.BaseURL, SuffixMode: "none"}}
	ch.ProxyMode = model.ProxyUsageModeDirect
	ch.ProxyConfigID = nil
	ch.ChannelProxy = nil
	ch.Proxy = false
	if len(ch.Keys) > 0 {
		key := ch.Keys[0]
		key.ChannelKey = NodeChannelKey
		key.Enabled = true
		key.SupportedModels = ""
		ch.Keys = []model.ChannelKey{key}
	} else {
		ch.Keys = []model.ChannelKey{{Enabled: true, ChannelKey: NodeChannelKey}}
	}
	return nil
}

func prepareNodeUpdate(req *model.ChannelUpdateRequest, current model.Channel, ctx context.Context) error {
	effective := current
	if req.NodeID != nil {
		effective.NodeID = *req.NodeID
	}
	if effective.NodeID == 0 && current.NodeID == 0 {
		return nil
	}
	if effective.NodeID < 0 {
		return fmt.Errorf("%w: invalid node id", ErrInvalidNodeChannel)
	}
	if effective.NodeID == 0 {
		if req.BaseUrls == nil || len(*req.BaseUrls) == 0 || len(req.KeysToAdd) == 0 {
			return fmt.Errorf("%w: HTTP transport requires a new URL and credentials", ErrInvalidNodeChannel)
		}
		for _, base := range *req.BaseUrls {
			u, err := url.Parse(base.URL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || strings.EqualFold(strings.TrimSuffix(u.Hostname(), "."), "node.octopus.invalid") {
				return fmt.Errorf("%w: HTTP transport requires a real URL", ErrInvalidNodeChannel)
			}
		}
		usableCredential := false
		for _, key := range req.KeysToAdd {
			credential := strings.TrimSpace(key.ChannelKey)
			if credential == "" || credential == NodeChannelKey {
				return fmt.Errorf("%w: HTTP transport requires new credentials", ErrInvalidNodeChannel)
			}
			usableCredential = usableCredential || key.Enabled
		}
		if !usableCredential {
			return fmt.Errorf("%w: HTTP transport requires an enabled credential", ErrInvalidNodeChannel)
		}
		req.KeysToDelete = make([]int, 0, len(current.Keys))
		for _, key := range current.Keys {
			req.KeysToDelete = append(req.KeysToDelete, key.ID)
		}
		req.KeysToUpdate = nil
		return nil
	}
	if req.Type != nil {
		effective.Type = *req.Type
	}
	if req.PoolID != nil {
		effective.PoolID = *req.PoolID
	}
	if req.ProxyMode != nil {
		effective.ProxyMode = *req.ProxyMode
	}
	if req.Proxy != nil {
		effective.Proxy = *req.Proxy
	}
	if req.ProxyConfigID != nil {
		effective.ProxyConfigID = req.ProxyConfigID
	}
	if req.ChannelProxy != nil {
		effective.ChannelProxy = req.ChannelProxy
	}
	if err := normalizeNodeChannel(&effective, ctx); err != nil {
		return err
	}
	req.BaseUrls = &effective.BaseUrls
	req.ProxyMode = &effective.ProxyMode
	req.Proxy = &effective.Proxy
	empty := ""
	req.ChannelProxy = &empty
	req.KeysToDelete = make([]int, 0, len(current.Keys))
	req.KeysToUpdate = nil
	req.KeysToAdd = nil
	if current.NodeID > 0 && len(current.Keys) > 0 {
		enabled := true
		key := NodeChannelKey
		req.KeysToUpdate = []model.ChannelKeyUpdateRequest{{ID: current.Keys[0].ID, Enabled: &enabled, ChannelKey: &key, SupportedModels: &empty}}
		for _, item := range current.Keys[1:] {
			req.KeysToDelete = append(req.KeysToDelete, item.ID)
		}
	} else {
		for _, item := range current.Keys {
			req.KeysToDelete = append(req.KeysToDelete, item.ID)
		}
		req.KeysToAdd = []model.ChannelKeyAddRequest{{Enabled: true, ChannelKey: NodeChannelKey}}
	}
	return nil
}
