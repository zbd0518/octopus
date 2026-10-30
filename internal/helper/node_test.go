package helper

import (
	"testing"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/nodetransport"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
)

func TestPrepareNodeChannelProbeRejectsConflictingTransports(t *testing.T) {
	proxyID := 1
	proxyURL := "http://127.0.0.1:8080"
	cases := []struct {
		name    string
		channel *model.Channel
	}{
		{"nil", nil},
		{"negative ID", &model.Channel{NodeID: -1}},
		{"pool", &model.Channel{NodeID: 1, PoolID: 1}},
		{"proxy boolean", &model.Channel{NodeID: 1, Proxy: true}},
		{"proxy ID", &model.Channel{NodeID: 1, ProxyConfigID: &proxyID}},
		{"system proxy", &model.Channel{NodeID: 1, ProxyMode: model.ProxyUsageModeSystem}},
		{"custom proxy", &model.Channel{NodeID: 1, ChannelProxy: &proxyURL}},
		{"unsupported protocol", &model.Channel{NodeID: 1, Type: outbound.OutboundType(-1)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := PrepareNodeChannelProbe(tc.channel); err == nil {
				t.Fatal("invalid node probe accepted")
			}
		})
	}
}

func TestPrepareNodeChannelProbeNormalizesOnlyNodeRequests(t *testing.T) {
	for _, kind := range []outbound.OutboundType{outbound.OutboundTypeOpenAIChat, outbound.OutboundTypeOpenAIResponse, outbound.OutboundTypeAnthropic, outbound.OutboundTypeOpenAIEmbedding} {
		channel := model.Channel{NodeID: 1, Type: kind, BaseUrls: []model.BaseUrl{{URL: "http://169.254.169.254"}}, Keys: []model.ChannelKey{{ID: 999, ChannelKey: "untrusted"}, {ChannelKey: "second"}}}
		if err := PrepareNodeChannelProbe(&channel); err != nil {
			t.Fatal(err)
		}
		if len(channel.BaseUrls) != 1 || channel.BaseUrls[0].URL != nodetransport.BaseURL || channel.BaseUrls[0].SuffixMode != "none" {
			t.Fatal("untrusted diagnostic target preserved")
		}
		if len(channel.Keys) != 1 || channel.Keys[0].ID != 0 || !channel.Keys[0].Enabled || channel.Keys[0].ChannelKey != nodeChannelCredential {
			t.Fatal("untrusted diagnostic credentials preserved")
		}
	}
	ordinary := model.Channel{BaseUrls: []model.BaseUrl{{URL: "https://example.com"}}, Keys: []model.ChannelKey{{ID: 2, ChannelKey: "ordinary-key"}}, PoolID: 5}
	if err := PrepareNodeChannelProbe(&ordinary); err != nil {
		t.Fatal(err)
	}
	if ordinary.BaseUrls[0].URL != "https://example.com" || ordinary.Keys[0].ChannelKey != "ordinary-key" || ordinary.PoolID != 5 {
		t.Fatal("ordinary HTTP probe was changed")
	}
}
