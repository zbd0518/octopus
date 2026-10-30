package helper

import (
	"fmt"
	"strings"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/nodetransport"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
)

const nodeChannelCredential = "octopus-node-local-credential"

// PrepareNodeChannelProbe never trusts a diagnostic payload's destination or key.
func PrepareNodeChannelProbe(channel *model.Channel) error {
	if channel == nil {
		return fmt.Errorf("channel is nil")
	}
	if channel.NodeID < 0 {
		return fmt.Errorf("invalid node id")
	}
	if channel.NodeID == 0 {
		return nil
	}
	switch channel.Type {
	case outbound.OutboundTypeOpenAIChat, outbound.OutboundTypeOpenAIResponse, outbound.OutboundTypeAnthropic, outbound.OutboundTypeOpenAIEmbedding:
	default:
		return fmt.Errorf("unsupported node channel protocol")
	}
	if channel.PoolID != 0 || channel.Proxy || channel.ProxyConfigID != nil || (channel.ProxyMode != "" && channel.ProxyMode != model.ProxyUsageModeDirect) || (channel.ChannelProxy != nil && strings.TrimSpace(*channel.ChannelProxy) != "") {
		return fmt.Errorf("node channels cannot use a pool or proxy")
	}
	channel.BaseUrls = []model.BaseUrl{{URL: nodetransport.BaseURL, SuffixMode: "none"}}
	channel.Keys = []model.ChannelKey{{Enabled: true, ChannelKey: nodeChannelCredential}}
	return nil
}
