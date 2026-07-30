package planprovider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op"
	"github.com/lingyuins/octopus/internal/op/channel"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
)

// withVolcenginePlanServer 起一个 mock GetAgentPlanAFPUsage 服务，返回固定用量。
// 返回的请求头捕获变量供断言新凭据是否被携带。
func withVolcenginePlanServer(t *testing.T) *string {
	t.Helper()
	gotCred := ""
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCred = r.Header.Get("Cookie") + "|||" + r.Header.Get("x-csrf-token")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(volcengineUsageResponse))
	}))
	old := volcenginePlanUsageURL
	volcenginePlanUsageURL = ts.URL
	t.Cleanup(func() {
		volcenginePlanUsageURL = old
		ts.Close()
	})
	return &gotCred
}

// withDeepSeekBalanceServer 起一个 mock DeepSeek 余额查询服务。
func withDeepSeekBalanceServer(t *testing.T, balance string) *string {
	t.Helper()
	gotKey := ""
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"is_available":true,"balance_infos":[{"total_balance":"` + balance + `","currency":"CNY"}]}`))
	}))
	old := deepSeekBalanceURL
	deepSeekBalanceURL = ts.URL
	t.Cleanup(func() {
		deepSeekBalanceURL = old
		ts.Close()
	})
	return &gotKey
}

// createProviderRow 直接写一条 PlanProvider 行（绕过 AddProvider 的渠道创建链路）。
func createProviderRow(t *testing.T, p *model.PlanProvider) *model.PlanProvider {
	t.Helper()
	now := time.Now()
	p.LastRefresh = &now
	p.CreatedAt = now
	p.UpdatedAt = now
	if err := db.GetDB().Create(p).Error; err != nil {
		t.Fatalf("create provider row: %v", err)
	}
	return p
}

// TestUpdateProviderCredentials_VolcengineNewAPIKeyRefreshesUsage 验证：
// 控制台会话凭据（火山方舟）失效后，仅换主凭据（Cookie|||csrf）、forward 不变时，
// 新凭据落库、用量用新凭据查询、forward_api_key 与 channel 不被触碰。
func TestUpdateProviderCredentials_VolcengineNewAPIKeyRefreshesUsage(t *testing.T) {
	setupPlanProviderDB(t)
	gotCred := withVolcenginePlanServer(t)

	const oldAPIKey = "sessionid=expired|||csrf-old"
	const newAPIKey = "sessionid=fresh|||csrf-new"
	const forwardKey = "ark-forward-secret"

	provider := createProviderRow(t, &model.PlanProvider{
		Name:          "Volcengine monitor",
		Category:      model.PlanProviderVolcenginePlan,
		ProviderType:  model.PlanProviderTypeTokenPlan,
		APIKey:        oldAPIKey,
		ForwardAPIKey: forwardKey,
		BaseURL:       "https://console.volcengine.com",
		QuotaTotal:    1,
		QuotaUsed:     1,
	})

	updated, err := UpdateProviderCredentials(context.Background(), provider.ID, newAPIKey, forwardKey, "", "")
	if err != nil {
		t.Fatalf("UpdateProviderCredentials() error = %v", err)
	}

	if updated.APIKey != newAPIKey {
		t.Errorf("APIKey = %q, want %q", updated.APIKey, newAPIKey)
	}
	if updated.ForwardAPIKey != forwardKey {
		t.Errorf("ForwardAPIKey = %q, want %q (不应改变)", updated.ForwardAPIKey, forwardKey)
	}
	// 新凭据被携带到查询请求
	if *gotCred != newAPIKey {
		t.Errorf("查询请求携带的凭据 = %q, want %q", *gotCred, newAPIKey)
	}
	// 用量被刷新（mock 返回 QuotaTotal=100000, QuotaUsed=35210.3625）
	if updated.QuotaTotal != 100000 {
		t.Errorf("QuotaTotal = %v, want 100000", updated.QuotaTotal)
	}
	if updated.QuotaUsed != 35210.3625 {
		t.Errorf("QuotaUsed = %v, want 35210.3625", updated.QuotaUsed)
	}

	// DB 持久化校验
	var stored model.PlanProvider
	if err := db.GetDB().First(&stored, provider.ID).Error; err != nil {
		t.Fatalf("load stored: %v", err)
	}
	if stored.APIKey != newAPIKey {
		t.Errorf("stored APIKey = %q, want %q", stored.APIKey, newAPIKey)
	}
	if stored.ForwardAPIKey != forwardKey {
		t.Errorf("stored ForwardAPIKey = %q, want %q", stored.ForwardAPIKey, forwardKey)
	}
}

// TestUpdateProviderCredentials_BalanceClearsForwardAPIKey 验证：
// 非控制台类（balance deepseek）换 api_key 时，forward_api_key 被清空（normalizePlanForwardAPIKey 行为）。
func TestUpdateProviderCredentials_BalanceClearsForwardAPIKey(t *testing.T) {
	setupPlanProviderDB(t)
	gotKey := withDeepSeekBalanceServer(t, "123.45")

	const oldAPIKey = "sk-old-deepseek"
	const newAPIKey = "sk-new-deepseek"

	provider := createProviderRow(t, &model.PlanProvider{
		Name:          "DeepSeek monitor",
		Category:      model.PlanProviderDeepSeek,
		ProviderType:  model.PlanProviderTypeBalance,
		APIKey:        oldAPIKey,
		ForwardAPIKey: "should-be-cleared",
		BaseURL:       "https://api.deepseek.com/v1",
		Balance:       0,
	})

	updated, err := UpdateProviderCredentials(context.Background(), provider.ID, newAPIKey, "ignored-forward", "", "")
	if err != nil {
		t.Fatalf("UpdateProviderCredentials() error = %v", err)
	}

	if updated.APIKey != newAPIKey {
		t.Errorf("APIKey = %q, want %q", updated.APIKey, newAPIKey)
	}
	if updated.ForwardAPIKey != "" {
		t.Errorf("ForwardAPIKey = %q, want empty (非控制台类应清空)", updated.ForwardAPIKey)
	}
	// 新凭据携带 Bearer 前缀
	if !strings.HasPrefix(*gotKey, "Bearer sk-new-deepseek") {
		t.Errorf("查询请求 Authorization = %q, 应携带新 key", *gotKey)
	}
	if updated.Balance != 123.45 {
		t.Errorf("Balance = %v, want 123.45", updated.Balance)
	}
}

// TestUpdateProviderCredentials_EmptyAPIKeyErrors 验证空 apiKey 报错。
func TestUpdateProviderCredentials_EmptyAPIKeyErrors(t *testing.T) {
	setupPlanProviderDB(t)
	provider := createProviderRow(t, &model.PlanProvider{
		Name:         "test",
		Category:     model.PlanProviderVolcenginePlan,
		ProviderType: model.PlanProviderTypeTokenPlan,
		APIKey:       "old",
		BaseURL:      "https://console.volcengine.com",
	})

	if _, err := UpdateProviderCredentials(context.Background(), provider.ID, "   ", "", "", ""); err == nil {
		t.Fatal("空 apiKey 应报错")
	}
}

// TestUpdateProviderCredentials_NotFound 验证不存在的 ID 报错。
func TestUpdateProviderCredentials_NotFound(t *testing.T) {
	setupPlanProviderDB(t)
	if _, err := UpdateProviderCredentials(context.Background(), 99999, "k", "", "", ""); err == nil {
		t.Fatal("不存在的 provider 应报错")
	}
}

// TestUpdateProviderCredentials_VolcengineNewForwardKeySyncsChannelKey 验证：
// 控制台类换 forward key 时，渠道里匹配旧 forward 值的那把 key 被更新为新值。
func TestUpdateProviderCredentials_VolcengineNewForwardKeySyncsChannelKey(t *testing.T) {
	setupPlanProviderDB(t)
	withVolcenginePlanServer(t)

	const oldAPIKey = "sessionid=ok|||csrf"
	const oldForward = "ark-old-forward"
	const newForward = "ark-new-forward"

	// 直接构造 channel + key + provider 行（绕过 AddProvider）
	planGroup := &model.ChannelGroup{Name: "Plan"}
	if err := db.GetDB().Create(planGroup).Error; err != nil {
		t.Fatalf("create channel group: %v", err)
	}
	ch := &model.Channel{
		Name:     "[Volcengine] test",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: "https://ark.cn-beijing.volces.com/api/plan/v3"}},
		Keys: []model.ChannelKey{
			{Enabled: true, ChannelKey: oldForward},
		},
		Model: "auto,doubao-seed-evolving",
	}
	if err := db.GetDB().Create(ch).Error; err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := channel.RefreshCache(context.Background()); err != nil {
		t.Fatalf("RefreshCache: %v", err)
	}

	provider := createProviderRow(t, &model.PlanProvider{
		Name:          "Volcengine monitor",
		Category:      model.PlanProviderVolcenginePlan,
		ProviderType:  model.PlanProviderTypeTokenPlan,
		APIKey:        oldAPIKey,
		ForwardAPIKey: oldForward,
		BaseURL:       "https://console.volcengine.com",
		ChannelID:     ch.ID,
	})

	if _, err := UpdateProviderCredentials(context.Background(), provider.ID, oldAPIKey, newForward, "", ""); err != nil {
		t.Fatalf("UpdateProviderCredentials() error = %v", err)
	}

	// 校验渠道 key 已更新为新值
	updatedCh, err := op.ChannelGet(ch.ID, context.Background())
	if err != nil {
		t.Fatalf("ChannelGet: %v", err)
	}
	var keyValues []string
	for _, k := range updatedCh.Keys {
		keyValues = append(keyValues, k.ChannelKey)
	}
	foundNew := false
	for _, v := range keyValues {
		if v == newForward {
			foundNew = true
		}
		if v == oldForward {
			t.Errorf("旧 forward key %q 仍存在，应已被更新", oldForward)
		}
	}
	if !foundNew {
		t.Errorf("新 forward key %q 未在渠道 keys %v 中找到", newForward, keyValues)
	}

	// DB 持久化
	var stored model.PlanProvider
	if err := db.GetDB().First(&stored, provider.ID).Error; err != nil {
		t.Fatalf("load stored: %v", err)
	}
	if stored.ForwardAPIKey != newForward {
		t.Errorf("stored ForwardAPIKey = %q, want %q", stored.ForwardAPIKey, newForward)
	}
}
