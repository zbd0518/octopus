package planprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
)

// makeTestCodexOAuthKey 构造合法的 Codex OAuth JSON 凭据（测试用）。
func makeTestCodexOAuthKey(accessToken, accountID string) string {
	b, _ := json.Marshal(map[string]any{
		"access_token": accessToken,
		"account_id":   accountID,
		"type":         "codex",
	})
	return string(b)
}

func TestParseCodexOAuthKey_Success(t *testing.T) {
	raw := makeTestCodexOAuthKey("tok-abc", "acct-123")
	key, err := parseCodexOAuthKey(raw)
	if err != nil {
		t.Fatalf("parseCodexOAuthKey() error = %v", err)
	}
	if key.AccessToken != "tok-abc" {
		t.Errorf("AccessToken = %q, want %q", key.AccessToken, "tok-abc")
	}
	if key.AccountID != "acct-123" {
		t.Errorf("AccountID = %q, want %q", key.AccountID, "acct-123")
	}
}

func TestParseCodexOAuthKey_Empty(t *testing.T) {
	if _, err := parseCodexOAuthKey(""); err == nil {
		t.Fatal("expected error for empty key, got nil")
	}
}

func TestParseCodexOAuthKey_NotJSON(t *testing.T) {
	if _, err := parseCodexOAuthKey("sk-not-json"); err == nil {
		t.Fatal("expected error for non-JSON key, got nil")
	}
}

func TestQueryCodexTokenPlan_Success(t *testing.T) {
	var gotAuth, gotAccountID, gotOriginator string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccountID = r.Header.Get("chatgpt-account-id")
		gotOriginator = r.Header.Get("originator")

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"plan_type": "pro",
			"rate_limit": map[string]any{
				"primary_window": map[string]any{
					"used_percent":         42.5,
					"reset_at":             1750000000,
					"limit_window_seconds": 604800,
				},
				"secondary_window": map[string]any{
					"used_percent":         10.0,
					"reset_at":             1750001800,
					"limit_window_seconds": 18000,
				},
			},
		})
	}))
	defer ts.Close()

	origURL := codexWhamUsageURL
	codexWhamUsageURL = ts.URL + "/backend-api/wham/usage"
	defer func() { codexWhamUsageURL = origURL }()

	keyJSON := makeTestCodexOAuthKey("tok-test", "acct-999")
	result, err := queryCodexTokenPlan(context.Background(), keyJSON, model.ProxyUsageModeDirect, nil)
	if err != nil {
		t.Fatalf("queryCodexTokenPlan() error = %v", err)
	}

	// 校验请求头
	if gotAuth != "Bearer tok-test" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer tok-test")
	}
	if gotAccountID != "acct-999" {
		t.Errorf("chatgpt-account-id = %q, want %q", gotAccountID, "acct-999")
	}
	if gotOriginator != "codex_cli_rs" {
		t.Errorf("originator = %q, want %q", gotOriginator, "codex_cli_rs")
	}

	// 校验解析
	if result.QuotaTotal != 100 {
		t.Errorf("QuotaTotal = %v, want 100", result.QuotaTotal)
	}
	if result.QuotaUsed != 42.5 {
		t.Errorf("QuotaUsed = %v, want 42.5", result.QuotaUsed)
	}
	if result.QuotaResetAt == nil {
		t.Fatal("QuotaResetAt is nil")
	}
	if !result.QuotaResetAt.Equal(time.Unix(1750000000, 0)) {
		t.Errorf("QuotaResetAt = %v, want %v", result.QuotaResetAt, time.Unix(1750000000, 0))
	}

	// secondary -> weekly
	if result.WeeklyTotal != 100 {
		t.Errorf("WeeklyTotal = %v, want 100", result.WeeklyTotal)
	}
	if result.WeeklyUsed != 10.0 {
		t.Errorf("WeeklyUsed = %v, want 10.0", result.WeeklyUsed)
	}
	if result.WeeklyResetAt == nil {
		t.Fatal("WeeklyResetAt is nil")
	}
	if !result.WeeklyResetAt.Equal(time.Unix(1750001800, 0)) {
		t.Errorf("WeeklyResetAt = %v, want %v", result.WeeklyResetAt, time.Unix(1750001800, 0))
	}
}

func TestQueryCodexTokenPlan_AuthFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"detail": "invalid token",
		})
	}))
	defer ts.Close()

	origURL := codexWhamUsageURL
	codexWhamUsageURL = ts.URL + "/backend-api/wham/usage"
	defer func() { codexWhamUsageURL = origURL }()

	keyJSON := makeTestCodexOAuthKey("bad-token", "acct-1")
	_, err := queryCodexTokenPlan(context.Background(), keyJSON, model.ProxyUsageModeDirect, nil)
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("error should mention auth failure, got: %v", err)
	}
}

func TestQueryCodexTokenPlan_EmptyAccessToken(t *testing.T) {
	keyJSON := makeTestCodexOAuthKey("", "acct-1")
	_, err := queryCodexTokenPlan(context.Background(), keyJSON, model.ProxyUsageModeDirect, nil)
	if err == nil {
		t.Fatal("expected error for empty access_token, got nil")
	}
	if !strings.Contains(err.Error(), "access_token") {
		t.Errorf("error should mention access_token, got: %v", err)
	}
}

func TestQueryCodexTokenPlan_EmptyAccountID(t *testing.T) {
	keyJSON := makeTestCodexOAuthKey("tok-abc", "")
	_, err := queryCodexTokenPlan(context.Background(), keyJSON, model.ProxyUsageModeDirect, nil)
	if err == nil {
		t.Fatal("expected error for empty account_id, got nil")
	}
	if !strings.Contains(err.Error(), "account_id") {
		t.Errorf("error should mention account_id, got: %v", err)
	}
}

func TestQueryCodexTokenPlan_InvalidJSON(t *testing.T) {
	_, err := queryCodexTokenPlan(context.Background(), "not-json-at-all", model.ProxyUsageModeDirect, nil)
	if err == nil {
		t.Fatal("expected error for non-JSON key, got nil")
	}
}

// --- 代理链路测试 ---

func TestPlanQueryHTTPClient_Direct(t *testing.T) {
	client, err := planQueryHTTPClient(model.ProxyUsageModeDirect, nil)
	if err != nil {
		t.Fatalf("planQueryHTTPClient() error = %v", err)
	}
	// direct 模式 Transport 为 nil（使用 http.DefaultTransport，无自定义代理）
	if client.Transport != nil {
		t.Errorf("direct mode Transport = %T, want nil (default transport)", client.Transport)
	}
	if client.Timeout != requestTimeout {
		t.Errorf("Timeout = %v, want %v", client.Timeout, requestTimeout)
	}
}

func TestPlanQueryHTTPClient_System(t *testing.T) {
	client, err := planQueryHTTPClient(model.ProxyUsageModeSystem, nil)
	if err != nil {
		t.Fatalf("planQueryHTTPClient() error = %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport is %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy == nil {
		t.Fatal("system mode should have proxy func set")
	}
	// 无环境变量代理时应解析为 nil（直连）
	req, _ := http.NewRequest(http.MethodGet, "https://chatgpt.com", nil)
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("Proxy() error = %v", err)
	}
	if proxyURL != nil {
		t.Logf("system proxy resolved from env: %s (expected in proxied environments)", proxyURL)
	}
}

func TestPlanQueryHTTPClient_Pool(t *testing.T) {
	const resolvedURL = "http://127.0.0.1:7890"
	orig := ProxyURLByConfigFunc
	ProxyURLByConfigFunc = func(id int, ctx context.Context) (string, error) {
		if id != 7 {
			t.Errorf("ProxyURLByConfigFunc id = %d, want 7", id)
		}
		return resolvedURL, nil
	}
	defer func() { ProxyURLByConfigFunc = orig }()

	configID := 7
	client, err := planQueryHTTPClient(model.ProxyUsageModePool, &configID)
	if err != nil {
		t.Fatalf("planQueryHTTPClient() error = %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport is %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy == nil {
		t.Fatal("pool mode should have proxy func set")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://chatgpt.com", nil)
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("Proxy() error = %v", err)
	}
	if proxyURL == nil || proxyURL.String() != resolvedURL {
		t.Errorf("proxy URL = %v, want %s", proxyURL, resolvedURL)
	}
}

func TestPlanQueryHTTPClient_PoolMissingConfigID(t *testing.T) {
	if _, err := planQueryHTTPClient(model.ProxyUsageModePool, nil); err == nil {
		t.Fatal("expected error for pool mode without config id")
	}
	zero := 0
	if _, err := planQueryHTTPClient(model.ProxyUsageModePool, &zero); err == nil {
		t.Fatal("expected error for pool mode with config id 0")
	}
}

func TestPlanQueryHTTPClient_PoolResolverNotInjected(t *testing.T) {
	orig := ProxyURLByConfigFunc
	ProxyURLByConfigFunc = nil
	defer func() { ProxyURLByConfigFunc = orig }()

	configID := 7
	if _, err := planQueryHTTPClient(model.ProxyUsageModePool, &configID); err == nil {
		t.Fatal("expected error when resolver not injected")
	}
}

func TestPlanQueryHTTPClient_PoolResolverError(t *testing.T) {
	orig := ProxyURLByConfigFunc
	ProxyURLByConfigFunc = func(id int, ctx context.Context) (string, error) {
		return "", http.ErrHijacked
	}
	defer func() { ProxyURLByConfigFunc = orig }()

	configID := 7
	if _, err := planQueryHTTPClient(model.ProxyUsageModePool, &configID); err == nil {
		t.Fatal("expected error from resolver")
	}
}

func TestQueryCodexTokenPlan_PoolProxyUsed(t *testing.T) {
	// 起一个 httptest 作为"上游"（WHAM API），再验证 pool 模式的 client 配置会走代理。
	// 这里直接验证 queryCodexTokenPlan 在 pool 模式下使用 planQueryHTTPClient 解析代理：
	// 用一个不可达的代理地址，请求应失败（证明确实尝试走代理而非直连成功）。
	var hit bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"plan_type": "pro"})
	}))
	defer ts.Close()

	origURL := codexWhamUsageURL
	codexWhamUsageURL = ts.URL + "/backend-api/wham/usage"
	defer func() { codexWhamUsageURL = origURL }()

	// 注入一个必然失败的代理（端口 1 不可连接）
	orig := ProxyURLByConfigFunc
	ProxyURLByConfigFunc = func(id int, ctx context.Context) (string, error) {
		return "http://127.0.0.1:1", nil
	}
	defer func() { ProxyURLByConfigFunc = orig }()

	configID := 1
	keyJSON := makeTestCodexOAuthKey("tok-test", "acct-999")
	_, err := queryCodexTokenPlan(context.Background(), keyJSON, model.ProxyUsageModePool, &configID)
	if err == nil {
		t.Fatal("expected error when proxy is unreachable, got nil (request bypassed proxy?)")
	}
	if hit {
		t.Error("upstream was hit directly; pool mode should route through proxy")
	}
}

func TestAddProviderCodexWithPoolProxy(t *testing.T) {
	setupPlanProviderDB(t)

	// WHAM 查询走 pool 代理：注入一个指向 httptest 上游的"代理"……实际上
	// planQueryHTTPClient 的 pool 模式用 http.ProxyURL，CONNECT 代理对 http:// 上游
	// 直接转发。这里用 http:// 的 WHAM URL，让 httptest 服务器充当代理+上游（对
	// http:// 请求，代理收到完整 URL 的请求行，httptest handler 照样能响应）。
	var hit bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"plan_type": "pro",
			"rate_limit": map[string]any{
				"primary_window": map[string]any{"used_percent": 42.5, "reset_at": 1750000000},
			},
		})
	}))
	defer ts.Close()

	origURL := codexWhamUsageURL
	codexWhamUsageURL = ts.URL + "/backend-api/wham/usage"
	defer func() { codexWhamUsageURL = origURL }()

	orig := ProxyURLByConfigFunc
	ProxyURLByConfigFunc = func(id int, ctx context.Context) (string, error) {
		return ts.URL, nil // httptest 服务器充当代理
	}
	defer func() { ProxyURLByConfigFunc = orig }()

	configID := 3
	keyJSON := makeTestCodexOAuthKey("tok-test", "acct-999")
	provider, err := AddProvider(context.Background(), model.PlanProviderCodex, keyJSON, "", "", 0, model.ProxyUsageModePool, &configID, "", "", "", "")
	if err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}
	if !hit {
		t.Fatal("WHAM query was not executed")
	}

	// provider 落库代理字段
	if provider.ProxyMode != model.ProxyUsageModePool {
		t.Errorf("provider.ProxyMode = %q, want %q", provider.ProxyMode, model.ProxyUsageModePool)
	}
	if provider.ProxyConfigID == nil || *provider.ProxyConfigID != 3 {
		t.Errorf("provider.ProxyConfigID = %v, want 3", provider.ProxyConfigID)
	}

	// 创建的渠道继承代理配置
	if provider.ChannelID <= 0 {
		t.Fatal("provider.ChannelID = 0, want created channel")
	}
	var channelCount int64
	if err := db.GetDB().Model(&model.Channel{}).Where("id = ?", provider.ChannelID).Count(&channelCount).Error; err != nil {
		t.Fatalf("count channel: %v", err)
	}
	if channelCount != 1 {
		t.Fatalf("channel count = %d, want 1", channelCount)
	}
	var ch model.Channel
	if err := db.GetDB().First(&ch, provider.ChannelID).Error; err != nil {
		t.Fatalf("load channel: %v", err)
	}
	if ch.ProxyMode != model.ProxyUsageModePool {
		t.Errorf("channel.ProxyMode = %q, want %q", ch.ProxyMode, model.ProxyUsageModePool)
	}
	if ch.ProxyConfigID == nil || *ch.ProxyConfigID != 3 {
		t.Errorf("channel.ProxyConfigID = %v, want 3", ch.ProxyConfigID)
	}
}

func TestAddProviderCodexPoolRequiresConfigID(t *testing.T) {
	setupPlanProviderDB(t)
	keyJSON := makeTestCodexOAuthKey("tok-test", "acct-999")
	if _, err := AddProvider(context.Background(), model.PlanProviderCodex, keyJSON, "", "", 0, model.ProxyUsageModePool, nil, "", "", "", ""); err == nil {
		t.Fatal("expected error for pool mode without config id")
	}
}

func TestAddProviderNonCodexIgnoresProxy(t *testing.T) {
	setupPlanProviderDB(t)
	withMiMoPlanServers(t)

	// MiMo 携带 pool 代理参数应被忽略（强制 direct）
	configID := 9
	provider, err := AddProvider(context.Background(), model.PlanProviderMiMoPlan, testMiMoCookie, "", "MiMo monitor", 0, model.ProxyUsageModePool, &configID, "", "", "", "")
	if err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}
	if provider.ProxyMode != model.ProxyUsageModeDirect {
		t.Errorf("provider.ProxyMode = %q, want %q (non-codex proxy must be ignored)", provider.ProxyMode, model.ProxyUsageModeDirect)
	}
	if provider.ProxyConfigID != nil {
		t.Errorf("provider.ProxyConfigID = %v, want nil", *provider.ProxyConfigID)
	}
}
