package planprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/utils/crypto"
)

// withDeepSeekPlatformServers 起 mock 的 DeepSeek 控制台登录 + usage 服务。
func withDeepSeekPlatformServers(t *testing.T, loginHandler, usageHandler http.HandlerFunc) (*int, *int) {
	t.Helper()
	loginRequests := 0
	usageRequests := 0

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/users/login"):
			loginRequests++
			loginHandler(w, r)
		case strings.Contains(r.URL.Path, "/usage/by_api_key/amount"):
			usageRequests++
			usageHandler(w, r)
		default:
			http.NotFound(w, r)
		}
	}))

	oldLogin := deepseekPlatformLoginURL
	oldUsage := deepseekPlatformUsageURL
	deepseekPlatformLoginURL = ts.URL + "/auth-api/v0/users/login"
	deepseekPlatformUsageURL = ts.URL + "/api/v0/usage/by_api_key/amount"
	t.Cleanup(func() {
		deepseekPlatformLoginURL = oldLogin
		deepseekPlatformUsageURL = oldUsage
		ts.Close()
	})
	return &loginRequests, &usageRequests
}

// deepseekUsageBody 构造一个 usage 响应：昨日 100 请求/5000 token，今日 10 请求/800 token。
func deepseekUsageBody() string {
	yesterday := time.Now().AddDate(0, 0, -1).Unix()
	today := time.Now().Unix()
	usage := map[string]any{
		"code": 0,
		"msg":  "",
		"data": map[string]any{
			"biz_code": 0,
			"biz_msg":  "",
			"biz_data": map[string]any{
				"start":  yesterday,
				"end":    today,
				"bucket": 86400,
				"series": []map[string]any{
					{
						"model": "deepseek-v4-flash",
						"buckets": []map[string]any{
							{"time": yesterday, "usage": map[string]any{"RESPONSE_TOKEN": 3000, "REQUEST": 60, "PROMPT_CACHE_HIT_TOKEN": 1000, "PROMPT_CACHE_MISS_TOKEN": 1000}},
							{"time": today, "usage": map[string]any{"RESPONSE_TOKEN": 500, "REQUEST": 10, "PROMPT_CACHE_HIT_TOKEN": 200, "PROMPT_CACHE_MISS_TOKEN": 100}},
						},
					},
					{
						"model": "deepseek-v4-pro",
						"buckets": []map[string]any{
							{"time": yesterday, "usage": map[string]any{"RESPONSE_TOKEN": 0, "REQUEST": 40, "PROMPT_CACHE_HIT_TOKEN": 0, "PROMPT_CACHE_MISS_TOKEN": 0}},
							{"time": today, "usage": map[string]any{"RESPONSE_TOKEN": 0, "REQUEST": 0, "PROMPT_CACHE_HIT_TOKEN": 0, "PROMPT_CACHE_MISS_TOKEN": 0}},
						},
					},
				},
			},
		},
	}
	b, _ := json.Marshal(usage)
	return string(b)
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

// extractJSONField 从 JSON 字符串中提取指定字段的值（用于断言请求体）。
func extractJSONField(raw, field string) string {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return ""
	}
	v, ok := parsed[field]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// TestDeepSeekPlatformLogin 验证登录请求体与 token 解析。
func TestDeepSeekPlatformLogin(t *testing.T) {
	var gotMobile, gotEmail, gotPassword, gotDeviceID string
	_, _ = withDeepSeekPlatformServers(t,
		func(w http.ResponseWriter, r *http.Request) {
			body := make([]byte, 512)
			n, _ := r.Body.Read(body)
			raw := string(body[:n])
			gotMobile = extractJSONField(raw, "mobile")
			gotEmail = extractJSONField(raw, "email")
			gotPassword = extractJSONField(raw, "password")
			gotDeviceID = extractJSONField(raw, "device_id")
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"code":0,"msg":"","data":{"biz_code":0,"biz_msg":"","biz_data":{"token":"test-token-123"}}}`))
		},
		func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("usage should not be called")
		},
	)

	// 手机号模式：mobile 填充、email 留空
	token, err := deepseekPlatformLogin(context.Background(), "13800138000", "secret-pass")
	if err != nil {
		t.Fatalf("deepseekPlatformLogin() error = %v", err)
	}
	if token != "test-token-123" {
		t.Fatalf("token = %q, want test-token-123", token)
	}
	if gotMobile != "13800138000" {
		t.Fatalf("mobile = %q, want 13800138000", gotMobile)
	}
	if gotEmail != "" {
		t.Fatalf("email = %q, want empty for mobile login", gotEmail)
	}
	if gotPassword != "secret-pass" {
		t.Fatalf("password = %q, want secret-pass", gotPassword)
	}
	if gotDeviceID == "" {
		t.Fatal("device_id = empty, want random UUID (server requires non-empty)")
	}
}

// TestDeepSeekPlatformLoginEmail 验证邮箱模式：email 填充、mobile 留空。
func TestDeepSeekPlatformLoginEmail(t *testing.T) {
	var gotMobile, gotEmail string
	_, _ = withDeepSeekPlatformServers(t,
		func(w http.ResponseWriter, r *http.Request) {
			body := make([]byte, 512)
			n, _ := r.Body.Read(body)
			raw := string(body[:n])
			gotMobile = extractJSONField(raw, "mobile")
			gotEmail = extractJSONField(raw, "email")
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"code":0,"msg":"","data":{"biz_code":0,"biz_msg":"","biz_data":{"token":"test-token-123"}}}`))
		},
		func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("usage should not be called")
		},
	)

	if _, err := deepseekPlatformLogin(context.Background(), "mincur@qq.com", "secret-pass"); err != nil {
		t.Fatalf("deepseekPlatformLogin(email) error = %v", err)
	}
	if gotEmail != "mincur@qq.com" {
		t.Fatalf("email = %q, want mincur@qq.com", gotEmail)
	}
	if gotMobile != "" {
		t.Fatalf("mobile = %q, want empty for email login", gotMobile)
	}
}

// TestDeepSeekPlatformLoginFailure 验证登录失败（非 2xx）返回错误。
func TestDeepSeekPlatformLoginFailure(t *testing.T) {
	_, _ = withDeepSeekPlatformServers(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"code":401,"msg":"bad credentials"}`))
		},
		func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("usage should not be called")
		},
	)

	if _, err := deepseekPlatformLogin(context.Background(), "13800138000", "wrong"); err == nil {
		t.Fatal("deepseekPlatformLogin() error = nil, want error")
	}
}

// TestQueryDeepSeekUsageAggregatesBuckets 验证 usage 聚合：累计/今日 token 与请求数。
func TestQueryDeepSeekUsageAggregatesBuckets(t *testing.T) {
	_, usageRequests := withDeepSeekPlatformServers(t,
		func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("login should not be called")
		},
		func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer sess-token" {
				t.Errorf("Authorization = %q, want Bearer sess-token", got)
			}
			q := r.URL.Query()
			if q.Get("start") == "" || q.Get("end") == "" {
				t.Errorf("missing start/end params: %v", q)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(deepseekUsageBody()))
		},
	)

	result, err := queryDeepSeekUsage(context.Background(), "sess-token", time.Now())
	if err != nil {
		t.Fatalf("queryDeepSeekUsage() error = %v", err)
	}
	if *usageRequests != 1 {
		t.Fatalf("usage requests = %d, want 1", *usageRequests)
	}

	// 累计：yesterday (3000+1000+1000=5000) + today (500+200+100=800) = 5800
	// 请求数：yesterday 60+40=100 + today 10 = 110
	if result.totalTokens != 5800 {
		t.Errorf("totalTokens = %d, want 5800", result.totalTokens)
	}
	if result.totalRequests != 110 {
		t.Errorf("totalRequests = %d, want 110", result.totalRequests)
	}
	if result.todayTokens != 800 {
		t.Errorf("todayTokens = %d, want 800", result.todayTokens)
	}
	if result.todayRequests != 10 {
		t.Errorf("todayRequests = %d, want 10", result.todayRequests)
	}
}

// TestEnsureDeepSeekSessionCacheAndRelogin 验证会话缓存：首次登录后复用，过期后重登。
func TestEnsureDeepSeekSessionCacheAndRelogin(t *testing.T) {
	crypto.Init("test-encryption-key")
	loginRequests, _ := withDeepSeekPlatformServers(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"code":0,"msg":"","data":{"biz_code":0,"biz_msg":"","biz_data":{"token":"tok-1"}}}`))
		},
		func(w http.ResponseWriter, r *http.Request) {},
	)
	clearDeepSeekSession(99)
	defer clearDeepSeekSession(99)

	pwEnc, err := crypto.Encrypt("secret-pass")
	if err != nil {
		t.Fatalf("crypto.Encrypt() error = %v", err)
	}
	provider := &model.PlanProvider{
		ID:               99,
		LoginUsername:    "13800138000",
		LoginPasswordEnc: pwEnc,
	}

	token, err := ensureDeepSeekSession(context.Background(), provider)
	if err != nil {
		t.Fatalf("ensureDeepSeekSession() error = %v", err)
	}
	if token != "tok-1" {
		t.Fatalf("token = %q, want tok-1", token)
	}
	if *loginRequests != 1 {
		t.Fatalf("login requests after first = %d, want 1", *loginRequests)
	}

	// 缓存命中：不再次登录
	token2, err := ensureDeepSeekSession(context.Background(), provider)
	if err != nil {
		t.Fatalf("ensureDeepSeekSession() 2nd error = %v", err)
	}
	if token2 != "tok-1" {
		t.Fatalf("2nd token = %q, want tok-1 (cache)", token2)
	}
	if *loginRequests != 1 {
		t.Fatalf("login requests after cache hit = %d, want 1", *loginRequests)
	}

	// 过期后重登
	entryI, _ := deepseekSessionCache.Load(99)
	entry := entryI.(*deepseekSessionEntry)
	entry.mu.Lock()
	entry.s.expiresAt = time.Now().Add(-time.Minute)
	entry.mu.Unlock()

	token3, err := ensureDeepSeekSession(context.Background(), provider)
	if err != nil {
		t.Fatalf("ensureDeepSeekSession() after expiry error = %v", err)
	}
	if token3 != "tok-1" {
		t.Fatalf("3rd token = %q, want tok-1 (relogin)", token3)
	}
	if *loginRequests != 2 {
		t.Fatalf("login requests after expiry = %d, want 2", *loginRequests)
	}
}

// TestQueryPlanChannelStatsOfficialPriority 验证：配置账号密码时官方 usage 优先。
func TestQueryPlanChannelStatsOfficialPriority(t *testing.T) {
	setupPlanProviderDB(t)
	crypto.Init("test-encryption-key")

	loginRequests, usageRequests := withDeepSeekPlatformServers(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"code":0,"msg":"","data":{"biz_code":0,"biz_msg":"","biz_data":{"token":"tok"}}}`))
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(deepseekUsageBody()))
		},
	)
	clearDeepSeekSession(7)
	defer clearDeepSeekSession(7)

	pwEnc, _ := crypto.Encrypt("secret-pass")
	provider := &model.PlanProvider{
		ID:               7,
		LoginUsername:    "13800138000",
		LoginPasswordEnc: pwEnc,
	}
	// 写入本地 stats 兜底数据（官方优先时不应被使用）
	today := time.Now().Format("20060102")
	if err := db.GetDB().Create(&model.StatsDailyChannel{Date: today, ChannelID: 42, StatsMetrics: model.StatsMetrics{RequestSuccess: 1, RequestFailed: 0, InputToken: 999, OutputToken: 999}}).Error; err != nil {
		t.Fatalf("create daily stats: %v", err)
	}

	got := queryPlanChannelStats(context.Background(), provider, 42)
	if got == nil {
		t.Fatal("queryPlanChannelStats() = nil")
	}
	if *loginRequests != 1 || *usageRequests != 1 {
		t.Fatalf("login=%d usage=%d, want 1/1 (official priority)", *loginRequests, *usageRequests)
	}
	// 官方聚合结果（同 TestQueryDeepSeekUsageAggregatesBuckets）
	if got.TotalTokens != 5800 {
		t.Errorf("TotalTokens = %d, want 5800 (official)", got.TotalTokens)
	}
	if got.TodayTokens != 800 {
		t.Errorf("TodayTokens = %d, want 800 (official)", got.TodayTokens)
	}
	if got.TotalRequests != 110 {
		t.Errorf("TotalRequests = %d, want 110 (official)", got.TotalRequests)
	}
}

// TestQueryPlanChannelStatsLocalFallback 验证：无账号密码时回退本地 stats。
func TestQueryPlanChannelStatsLocalFallback(t *testing.T) {
	setupPlanProviderDB(t)

	today := time.Now().Format("20060102")
	if err := db.GetDB().Create(&model.StatsDailyChannel{Date: today, ChannelID: 42, StatsMetrics: model.StatsMetrics{RequestSuccess: 7, RequestFailed: 1, InputToken: 1000, OutputToken: 2000}}).Error; err != nil {
		t.Fatalf("create daily stats: %v", err)
	}

	got := queryPlanChannelStats(context.Background(), nil, 42)
	if got == nil {
		t.Fatal("queryPlanChannelStats() = nil")
	}
	if got.TodayRequests != 8 {
		t.Errorf("TodayRequests = %d, want 8 (local fallback)", got.TodayRequests)
	}
	if got.TodayTokens != 3000 {
		t.Errorf("TodayTokens = %d, want 3000 (local fallback)", got.TodayTokens)
	}
	if got.Source != "local" {
		t.Errorf("Source = %q, want local", got.Source)
	}
}

// TestQueryPlanChannelStatsOfficialFailureFallback 验证：配置账号密码但官方查询
// 失败（如 usage API 5xx）时回退本地 stats，且 Source 标记为 local。
func TestQueryPlanChannelStatsOfficialFailureFallback(t *testing.T) {
	setupPlanProviderDB(t)
	crypto.Init("test-encryption-key")

	_, usageRequests := withDeepSeekPlatformServers(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"code":0,"msg":"","data":{"biz_code":0,"biz_msg":"","biz_data":{"token":"tok"}}}`))
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
	)
	clearDeepSeekSession(8)
	defer clearDeepSeekSession(8)

	pwEnc, _ := crypto.Encrypt("secret-pass")
	provider := &model.PlanProvider{
		ID:               8,
		LoginUsername:    "13800138000",
		LoginPasswordEnc: pwEnc,
	}
	today := time.Now().Format("20060102")
	if err := db.GetDB().Create(&model.StatsDailyChannel{Date: today, ChannelID: 42, StatsMetrics: model.StatsMetrics{RequestSuccess: 7, RequestFailed: 1, InputToken: 1000, OutputToken: 2000}}).Error; err != nil {
		t.Fatalf("create daily stats: %v", err)
	}

	got := queryPlanChannelStats(context.Background(), provider, 42)
	if got == nil {
		t.Fatal("queryPlanChannelStats() = nil")
	}
	if *usageRequests != 1 {
		t.Fatalf("usage requests = %d, want 1", *usageRequests)
	}
	if got.Source != "local" {
		t.Errorf("Source = %q, want local (fallback)", got.Source)
	}
	if got.TodayTokens != 3000 {
		t.Errorf("TodayTokens = %d, want 3000 (local fallback)", got.TodayTokens)
	}
}

// TestDeepSeekLoginCooldown 验证：登录失败后冷却期内不再真实登录。
func TestDeepSeekLoginCooldown(t *testing.T) {
	crypto.Init("test-encryption-key")
	loginRequests, _ := withDeepSeekPlatformServers(t,
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		},
		func(w http.ResponseWriter, r *http.Request) {},
	)
	clearDeepSeekSession(100)
	defer clearDeepSeekSession(100)

	pwEnc, _ := crypto.Encrypt("secret-pass")
	provider := &model.PlanProvider{
		ID:               100,
		LoginUsername:    "13800138000",
		LoginPasswordEnc: pwEnc,
	}

	// 第一次登录失败
	if _, err := ensureDeepSeekSession(context.Background(), provider); err == nil {
		t.Fatal("ensureDeepSeekSession() error = nil, want error")
	}
	if *loginRequests != 1 {
		t.Fatalf("login requests after first failure = %d, want 1", *loginRequests)
	}
	// 冷却期内不重试
	if _, err := ensureDeepSeekSession(context.Background(), provider); err == nil {
		t.Fatal("ensureDeepSeekSession() during cooldown error = nil, want error")
	}
	if *loginRequests != 1 {
		t.Fatalf("login requests during cooldown = %d, want 1 (no retry)", *loginRequests)
	}
}
