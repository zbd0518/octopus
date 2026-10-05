package helper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	appmodel "github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
	"github.com/lingyuins/octopus/internal/utils/xurl"
)

func TestTestGroupModelItemRespectsKeyModels(t *testing.T) {
	setupHelperDB(t)
	xurl.SetSSRFAllowPrivateForTest(true)
	t.Cleanup(func() { xurl.SetSSRFAllowPrivateForTest(false) })
	originalCooldown := appmodel.KeyCooldownFunc
	originalStrategy := appmodel.GlobalKeySelectionStrategyFunc
	appmodel.KeyCooldownFunc = nil
	appmodel.GlobalKeySelectionStrategyFunc = nil
	t.Cleanup(func() {
		appmodel.KeyCooldownFunc = originalCooldown
		appmodel.GlobalKeySelectionStrategyFunc = originalStrategy
	})

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.Header.Get("Authorization"); got != "Bearer sk-supported" {
			t.Errorf("Authorization = %q, want supported key", got)
		}
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Model != "target-model" {
			t.Errorf("model = %q, want target-model", body.Model)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"probe","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	channel := appmodel.Channel{
		ID: 42, Name: "model-isolated", Enabled: true,
		Type: outbound.OutboundTypeOpenAIChat, KeySelectionStrategy: "cost",
		BaseUrls: []appmodel.BaseUrl{{URL: server.URL}},
		Keys: []appmodel.ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "sk-unsupported", SupportedModels: "other-model"},
			{ID: 2, Enabled: true, ChannelKey: "sk-supported", SupportedModels: "target-model", TotalCost: 10},
		},
	}
	item := appmodel.GroupItem{ID: 1, ChannelID: 42, ModelName: "target-model"}
	result := testGroupModelItem(context.Background(), appmodel.EndpointTypeChat, item, map[int]appmodel.Channel{42: channel})
	if !result.Passed || requests != 1 {
		t.Fatalf("result = %#v, requests = %d, want success with one supported-key request", result, requests)
	}
	channel.Keys = channel.Keys[:1]
	result = testGroupModelItem(context.Background(), appmodel.EndpointTypeChat, item, map[int]appmodel.Channel{42: channel})
	if result.Passed || result.Message != "no available key" || requests != 1 {
		t.Fatalf("result = %#v, requests = %d, want no request when all keys are unsupported", result, requests)
	}
}
