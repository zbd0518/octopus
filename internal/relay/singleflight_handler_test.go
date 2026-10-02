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
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lingyuins/octopus/internal/db"
	dbmodel "github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op"
	"github.com/lingyuins/octopus/internal/op/channel"
	"github.com/lingyuins/octopus/internal/op/group"
	"github.com/lingyuins/octopus/internal/op/setting"
	"github.com/lingyuins/octopus/internal/op/stats"
	"github.com/lingyuins/octopus/internal/transformer/inbound"
	transmodel "github.com/lingyuins/octopus/internal/transformer/model"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
	"github.com/lingyuins/octopus/internal/utils/xurl"
)

func TestSingleflightHandlerLeaderWritesAndSavesOnce(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusBadRequest} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			if err := db.InitDB("sqlite", filepath.Join(t.TempDir(), "relay.db"), false); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			if err := op.InitCache(); err != nil {
				t.Fatal(err)
			}
			for key, value := range map[dbmodel.SettingKey]string{
				dbmodel.SettingKeySemanticCacheEnabled: "false",
				dbmodel.SettingKeyRelayRetryCount:      "0",
				dbmodel.SettingKeyRelayRouteRetries:    "1",
			} {
				if err := setting.SetString(key, value); err != nil {
					t.Fatal(err)
				}
			}
			xurl.SetSSRFAllowPrivateForTest(true)
			t.Cleanup(func() { xurl.SetSSRFAllowPrivateForTest(false) })
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				select {
				case entered <- struct{}{}:
				default:
				}
				<-release
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				if status == http.StatusOK {
					_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
				} else {
					_, _ = w.Write([]byte(`{"error":{"message":"invalid input","type":"invalid_request_error"}}`))
				}
			}))
			defer upstream.Close()
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			ch := dbmodel.Channel{ID: 90001 + status, Name: t.Name(), Enabled: true, Type: outbound.OutboundTypeOpenAIChat,
				BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}}, Keys: []dbmodel.ChannelKey{{ID: 90001 + status, Enabled: true, ChannelKey: "upstream-key"}}}
			channel.GetCache().Set(ch.ID, ch)
			t.Cleanup(func() { channel.GetCache().Del(ch.ID) })
			g := dbmodel.Group{Name: "test-model", EndpointType: dbmodel.EndpointTypeChat, OutboundFormat: "chat_only",
				Items: []dbmodel.GroupItem{{ChannelID: ch.ID, ModelName: "test-model", Priority: 1, Weight: 1}}}
			if err := group.GroupCreate(&g, context.Background()); err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"test-model","messages":[{"role":"user","content":"hello"}]}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("api_key_id", 7)
			done := make(chan struct{})
			go func() {
				defer close(done)
				Handler(dbmodel.EndpointTypeChat, inbound.InboundTypeOpenAIChat, c)
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("relay did not reach upstream")
			}
			key, ok := requestSingleflightKey(7, "chat", "test-model", "user: hello", &transmodel.InternalLLMRequest{})
			if !ok {
				t.Fatal("request is not eligible for singleflight")
			}
			follower := relayInflightGroup.DoChan(key, func() (any, error) {
				return nil, fmt.Errorf("follower unexpectedly executed")
			})
			close(release)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("relay did not finish")
			}
			result := <-follower
			if !result.Shared {
				t.Fatal("test did not exercise a shared leader")
			}
			if calls.Load() != 1 {
				t.Fatalf("upstream calls = %d, want 1", calls.Load())
			}
			if recorder.Code != status {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, status, recorder.Body.String())
			}
			if !json.Valid(recorder.Body.Bytes()) {
				t.Fatalf("response contains duplicate or invalid JSON: %s", recorder.Body.String())
			}
			total := stats.TotalGet()
			if total.RequestSuccess+total.RequestFailed != 1 {
				t.Fatalf("request accounting = %d success + %d failure, want 1", total.RequestSuccess, total.RequestFailed)
			}
		})
	}
}
