package planprovider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// makeTestOasisToken 构造一个合法的 Oasis-Token 格式（access...refresh），
// refresh payload 含指定 device_id。内容不参与签名校验（测试用）。
func makeTestOasisToken(t *testing.T, deviceID string) string {
	t.Helper()
	access := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhY3RpdmF0ZWQiOnRydWUsImV4cCI6OTk5OTk5OTk5OSwib2FzaXNfaWQiOjF9.sig"
	payload, _ := json.Marshal(map[string]any{
		"app_id":    10300,
		"device_id": deviceID,
		"exp":       9999999999,
		"platform":  "web",
	})
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	refresh := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." + encodedPayload + ".sig"
	return access + "..." + refresh
}

func TestDecodeStepFunWebID(t *testing.T) {
	token := makeTestOasisToken(t, "abc123device456")
	webid := decodeStepFunWebID(token)
	if webid != "abc123device456" {
		t.Errorf("decodeStepFunWebID() = %q, want %q", webid, "abc123device456")
	}
}

func TestDecodeStepFunWebID_NoSeparator(t *testing.T) {
	webid := decodeStepFunWebID("justanaccesstoken")
	if webid != "" {
		t.Errorf("decodeStepFunWebID() = %q, want empty", webid)
	}
}

func TestQueryStepFunPlanTokenPlan_Success(t *testing.T) {
	var gotCookie, gotAppID, gotMethod string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotCookie = r.Header.Get("Cookie")
		gotAppID = r.Header.Get("oasis-appid")

		if r.Header.Get("Connect-Protocol-Version") != "1" {
			t.Errorf("missing Connect-Protocol-Version header")
		}
		if r.Header.Get("oasis-platform") != "web" {
			t.Errorf("oasis-platform = %q, want web", r.Header.Get("oasis-platform"))
		}

		resp := map[string]any{
			"status":                     1,
			"desc":                       "",
			"five_hour_usage_left_rate":  0,
			"five_hour_usage_reset_time": "0",
			"weekly_usage_left_rate":     0,
			"weekly_usage_reset_time":    "0",
			"plan_family":                2,
			"plan_credit_rate_limit": map[string]any{
				"subscription_credit_left_rate":  0.9964648,
				"subscription_credit_reset_time": "1784379705",
				"topup_credit_left_rate":         0,
				"credit_buckets": []map[string]any{
					{
						"type":            1,
						"credit_total":    "400000000",
						"credit_residual": "398585926",
						"expire_at":       "1785675705",
						"next_reset_at":   "1784379705",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	// 覆盖硬编码 URL 指向 mock server
	origURL := stepFunPlanURL
	stepFunPlanURL = ts.URL
	defer func() { stepFunPlanURL = origURL }()

	token := makeTestOasisToken(t, "test-device-id")
	result, err := queryStepFunPlanTokenPlan(context.Background(), token)
	if err != nil {
		t.Fatalf("queryStepFunPlanTokenPlan() error = %v", err)
	}

	// 校验请求头
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if !strings.Contains(gotCookie, "Oasis-Token=") {
		t.Errorf("cookie missing Oasis-Token: %s", gotCookie)
	}
	if !strings.Contains(gotCookie, "Oasis-Webid=test-device-id") {
		t.Errorf("cookie missing Oasis-Webid: %s", gotCookie)
	}
	if gotAppID != "10300" {
		t.Errorf("oasis-appid = %q, want 10300", gotAppID)
	}

	// 校验解析结果
	if result.QuotaTotal != 400000000 {
		t.Errorf("QuotaTotal = %f, want 400000000", result.QuotaTotal)
	}
	// used = total - residual = 400000000 - 398585926 = 1414074
	if result.QuotaUsed != 1414074 {
		t.Errorf("QuotaUsed = %f, want 1414074", result.QuotaUsed)
	}
	if result.QuotaResetAt == nil {
		t.Error("QuotaResetAt is nil")
	} else {
		// 1784379705 = 2026-07-18 21:01:45 UTC
		want := time.Unix(1784379705, 0)
		if !result.QuotaResetAt.Equal(want) {
			t.Errorf("QuotaResetAt = %v, want %v", result.QuotaResetAt, want)
		}
	}
}

func TestQueryStepFunPlanTokenPlan_AuthFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"code":    "unauthenticated",
			"message": "auth failed: oasis-token is embezzled",
		})
	}))
	defer ts.Close()

	origURL := stepFunPlanURL
	stepFunPlanURL = ts.URL
	defer func() { stepFunPlanURL = origURL }()

	token := makeTestOasisToken(t, "device")
	_, err := queryStepFunPlanTokenPlan(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	if !strings.Contains(err.Error(), "鉴权失败") {
		t.Errorf("error = %q, want '鉴权失败'", err.Error())
	}
}

func TestQueryStepFunPlanTokenPlan_EmptyToken(t *testing.T) {
	_, err := queryStepFunPlanTokenPlan(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
	if !strings.Contains(err.Error(), "oasis token is required") {
		t.Errorf("error = %q, want 'oasis token is required'", err.Error())
	}
}

// --- SenseNova Plan 测试 ---

// makeTestSenseNovaToken 构造一个合法的 SenseNova Bearer JWT，
// payload 含 ext.tenant_id。内容不参与签名校验（测试用）。
func makeTestSenseNovaToken(t *testing.T, tenantID string) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"client_id": "nova",
		"exp":       9999999999,
		"ext": map[string]any{
			"tenant_id":    tenantID,
			"principal_id": "test-principal",
			"username":     "testuser",
		},
	})
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	return "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9." + encodedPayload + ".sig"
}

func TestDecodeSenseNovaAccountID(t *testing.T) {
	token := makeTestSenseNovaToken(t, "tenant-abc-123")
	accountID := decodeSenseNovaAccountID(token)
	if accountID != "tenant-abc-123" {
		t.Errorf("decodeSenseNovaAccountID() = %q, want %q", accountID, "tenant-abc-123")
	}
}

func TestDecodeSenseNovaAccountID_EmptyToken(t *testing.T) {
	accountID := decodeSenseNovaAccountID("invalid")
	if accountID != "" {
		t.Errorf("decodeSenseNovaAccountID() = %q, want empty", accountID)
	}
}

func TestQuerySenseNovaPlanTokenPlan_Success(t *testing.T) {
	var gotAuth, gotMethod, gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path

		if r.URL.RawQuery != "" {
			t.Errorf("unexpected query string: %s", r.URL.RawQuery)
		}

		resp := map[string]any{
			"pools": []map[string]any{
				{
					"pool_type": "dedicated",
					"name":      "Flash-Lite积分池",
					"window_5h": map[string]any{
						"limit": "999",
						"used":  "123",
					},
					"window_7d": map[string]any{
						"limit": "9999",
						"used":  "456",
					},
				},
				{
					"pool_type": "default",
					"window_5h": map[string]any{
						"limit":     "120000",
						"used":      "0",
						"remaining": "120000",
						"reset_at":  "1793257798",
					},
					"window_7d": map[string]any{
						"limit":     "1200000",
						"used":      "0",
						"remaining": "1200000",
						"reset_at":  "1793844598",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	origURL := senseNovaPlanURL
	senseNovaPlanURL = ts.URL + "/lite/console/v1/tokenplan/pool-usage"
	defer func() { senseNovaPlanURL = origURL }()

	result, err := querySenseNovaPlanTokenPlan(context.Background(), "test-token")
	if err != nil {
		t.Fatalf("querySenseNovaPlanTokenPlan() error = %v", err)
	}

	// 校验请求
	if gotMethod != http.MethodGet {
		t.Errorf("method = %s, want GET", gotMethod)
	}
	if gotPath != "/lite/console/v1/tokenplan/pool-usage" {
		t.Errorf("path = %s, want /lite/console/v1/tokenplan/pool-usage", gotPath)
	}
	if !strings.Contains(gotAuth, "Bearer ") {
		t.Errorf("Authorization = %q, want Bearer", gotAuth)
	}

	// 校验解析结果
	if result.QuotaTotal != 0 || result.QuotaUsed != 0 || result.QuotaResetAt != nil {
		t.Errorf("monthly quota = %v/%v/%v, want empty", result.QuotaUsed, result.QuotaTotal, result.QuotaResetAt)
	}
	if result.FiveHourTotal != 120000 || result.FiveHourUsed != 0 {
		t.Errorf("5h quota = %v/%v, want 0/120000", result.FiveHourUsed, result.FiveHourTotal)
	}
	if result.WeeklyTotal != 1200000 || result.WeeklyUsed != 0 {
		t.Errorf("7d quota = %v/%v, want 0/1200000", result.WeeklyUsed, result.WeeklyTotal)
	}
	if result.DedicatedPoolName != "Flash-Lite积分池" {
		t.Errorf("dedicated pool name = %q, want Flash-Lite积分池", result.DedicatedPoolName)
	}
	if result.DedicatedFiveHourTotal != 999 || result.DedicatedFiveHourUsed != 123 {
		t.Errorf("dedicated 5h quota = %v/%v, want 123/999", result.DedicatedFiveHourUsed, result.DedicatedFiveHourTotal)
	}
	if result.DedicatedWeeklyTotal != 9999 || result.DedicatedWeeklyUsed != 456 {
		t.Errorf("dedicated 7d quota = %v/%v, want 456/9999", result.DedicatedWeeklyUsed, result.DedicatedWeeklyTotal)
	}
	if result.FiveHourResetAt == nil || !result.FiveHourResetAt.Equal(time.Unix(1793257798, 0)) {
		t.Errorf("5h reset_at = %v, want %v", result.FiveHourResetAt, time.Unix(1793257798, 0))
	}
	if result.WeeklyResetAt == nil || !result.WeeklyResetAt.Equal(time.Unix(1793844598, 0)) {
		t.Errorf("7d reset_at = %v, want %v", result.WeeklyResetAt, time.Unix(1793844598, 0))
	}
}

func TestQuerySenseNovaPlanTokenPlan_EmptyToken(t *testing.T) {
	_, err := querySenseNovaPlanTokenPlan(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
	if !strings.Contains(err.Error(), "token is required") {
		t.Errorf("error = %q, want 'token is required'", err.Error())
	}
}

func TestQuerySenseNovaPlanTokenPlan_NoPools(t *testing.T) {
	// 新接口不需要从 JWT 解码 account_id，任意非空 Bearer Token 都交给上游校验。
	// 这里覆盖空池响应，确保不会再次静默回退到 0/100。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"pools": []any{}})
	}))
	defer ts.Close()

	origURL := senseNovaPlanURL
	senseNovaPlanURL = ts.URL + "/lite/console/v1/tokenplan/pool-usage"
	defer func() { senseNovaPlanURL = origURL }()

	_, err := querySenseNovaPlanTokenPlan(context.Background(), "invalid.token")
	if err == nil || !strings.Contains(err.Error(), "no quota pools") {
		t.Fatalf("error = %v, want no quota pools error", err)
	}
}
