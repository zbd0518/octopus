package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lingyuins/octopus/internal/db"
	dm "github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op"
	"github.com/lingyuins/octopus/internal/op/channel"
	"github.com/lingyuins/octopus/internal/op/group"
	"github.com/lingyuins/octopus/internal/op/setting"
	"github.com/lingyuins/octopus/internal/transformer/inbound"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
	"github.com/lingyuins/octopus/internal/utils/semantic_cache"
	"github.com/lingyuins/octopus/internal/utils/xurl"
)

func setupDiagnosticsRelay(t *testing.T, cache bool) {
	t.Helper()
	previousSettings := setting.GetCache().GetAll()
	t.Cleanup(func() {
		setting.GetCache().Clear()
		for key, value := range previousSettings {
			setting.GetCache().Set(key, value)
		}
	})
	if err := db.InitDB("sqlite", filepath.Join(t.TempDir(), "relay.db"), false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); semantic_cache.Reset() })
	if err := op.InitCache(); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[dm.SettingKey]string{
		dm.SettingKeySemanticCacheEnabled: fmt.Sprint(cache), dm.SettingKeyRelayRetryCount: "1", dm.SettingKeyRelayRouteRetries: "1", dm.SettingKeyRatelimitCooldown: "0",
	} {
		if err := setting.SetString(key, value); err != nil {
			t.Fatal(err)
		}
	}
	xurl.SetSSRFAllowPrivateForTest(true)
	t.Cleanup(func() { xurl.SetSSRFAllowPrivateForTest(false) })
}

func diagnosticsRequest(model string, stream bool) (*gin.Context, *httptest.ResponseRecorder) {
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{"model":%q,"stream":%t,"messages":[{"role":"user","content":"hello"}]}`, model, stream)))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("api_key_id", 929001)
	return c, writer
}

func TestDiagnosticsHandlerRoundRobinAndNextChannel(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			setupDiagnosticsRelay(t, false)
			var calls [2]atomic.Int32
			for index := range 2 {
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls[index].Add(1)
					w.Header().Set("Content-Type", "application/json")
					if failure && index == 0 {
						w.WriteHeader(500)
						fmt.Fprint(w, `{"error":{"message":"failed"}}`)
						return
					}
					fmt.Fprint(w, `{"id":"ok","object":"chat.completion","model":"upstream","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`)
				}))
				t.Cleanup(s.Close)
				id := 929010 + index
				channel.GetCache().Set(id, dm.Channel{ID: id, Name: fmt.Sprint(id), Enabled: true, Type: outbound.OutboundTypeOpenAIChat, BaseUrls: []dm.BaseUrl{{URL: s.URL}}, Keys: []dm.ChannelKey{{ID: id * 10, Enabled: true, ChannelKey: "a"}, {ID: id*10 + 1, Enabled: true, ChannelKey: "b"}}})
				t.Cleanup(func() { channel.GetCache().Del(id) })
			}
			mode := dm.GroupModeRoundRobin
			if failure {
				mode = dm.GroupModeFailover
			}
			g := dm.Group{Name: "diagnostics-model", EndpointType: "chat", Mode: mode, OutboundFormat: "chat_only", Items: []dm.GroupItem{{ChannelID: 929010, ModelName: "upstream", Weight: 1, Priority: 1}, {ChannelID: 929011, ModelName: "upstream", Weight: 1, Priority: 2}}}
			if err := group.GroupCreate(&g, context.Background()); err != nil {
				t.Fatal(err)
			}
			count := 6
			if failure {
				count = 1
			}
			for range count {
				c, w := diagnosticsRequest(g.Name, false)
				Handler("chat", inbound.InboundTypeOpenAIChat, c)
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			if failure {
				if calls[0].Load() != 1 || calls[1].Load() != 1 {
					t.Fatalf("failed channel retried keys: %d:%d", calls[0].Load(), calls[1].Load())
				}
			} else if calls[0].Load() != 3 || calls[1].Load() != 3 {
				t.Fatalf("unbalanced requests: %d:%d", calls[0].Load(), calls[1].Load())
			}
		})
	}
}

func TestDiagnosticsCanonicalResponsesCacheAndResume(t *testing.T) {
	setupDiagnosticsRelay(t, true)
	var embeddings, upstream atomic.Int32
	embed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		embeddings.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"embedding":[1,0,0]}]}`)
	}))
	defer embed.Close()
	for key, value := range map[dm.SettingKey]string{dm.SettingKeySemanticCacheEmbeddingBaseURL: embed.URL, dm.SettingKeySemanticCacheEmbeddingAPIKey: "test", dm.SettingKeySemanticCacheEmbeddingModel: "test"} {
		if err := setting.SetString(key, value); err != nil {
			t.Fatal(err)
		}
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"ok","object":"chat.completion","model":"upstream","choices":[{"index":0,"message":{"role":"assistant","content":"hello cache"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`)
	}))
	defer s.Close()
	ch := dm.Channel{ID: 929020, Enabled: true, Type: outbound.OutboundTypeOpenAIChat, BaseUrls: []dm.BaseUrl{{URL: s.URL}}, Keys: []dm.ChannelKey{{ID: 929021, Enabled: true, ChannelKey: "test"}}}
	channel.GetCache().Set(ch.ID, ch)
	defer channel.GetCache().Del(ch.ID)
	g := dm.Group{Name: "cache-model", EndpointType: "responses", OutboundFormat: "chat_only", Items: []dm.GroupItem{{ChannelID: ch.ID, ModelName: "upstream", Weight: 1, Priority: 1}}}
	if err := group.GroupCreate(&g, context.Background()); err != nil {
		t.Fatal(err)
	}
	request := func(stream bool, session string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"cache-model","input":"hello","stream":%t}`, stream)))
		c.Request.Header.Set("Content-Type", "application/json")
		if session != "" {
			c.Request.Header.Set("X-Conversation-ID", session)
		}
		c.Set("api_key_id", 929001)
		Handler("responses", inbound.InboundTypeOpenAIResponse, c)
		return w
	}
	first := request(false, "")
	if first.Code != 200 || !json.Valid(first.Body.Bytes()) {
		t.Fatal(first.Code, first.Body.String())
	}
	cached := request(true, "cache-replay-diagnostics")
	if cached.Code != 200 || !strings.Contains(cached.Body.String(), "response.output_text.delta") || !strings.Contains(cached.Body.String(), "hello cache") || !strings.Contains(cached.Body.String(), "response.completed") {
		t.Fatal(cached.Code, cached.Body.String())
	}
	replayed := request(true, "cache-replay-diagnostics")
	if replayed.Code != 200 || !strings.Contains(replayed.Body.String(), "hello cache") {
		t.Fatal(replayed.Code, replayed.Body.String())
	}
	if upstream.Load() != 1 {
		t.Fatalf("upstream calls=%d", upstream.Load())
	}
	if embeddings.Load() != 2 {
		t.Fatalf("embedding lookup/store duplicated: %d", embeddings.Load())
	}
}
