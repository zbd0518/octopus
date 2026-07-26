package planprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 火山方舟 Agent Plan 真实响应结构（2026-07-25 抓包）。
const volcengineUsageResponse = `{
  "ResponseMetadata": {
    "RequestId": "2026072511091503C760FFB7F4916A7413",
    "Action": "GetAgentPlanAFPUsage",
    "Version": "2024-01-01",
    "Service": "ark",
    "Region": "cn-beijing"
  },
  "Result": {
    "PlanType": "medium",
    "AFPFiveHour": {"Quota": 10000, "Used": 34.5528, "SubscribeTime": 1784948376000, "ResetTime": 1784966376000},
    "AFPWeekly":   {"Quota": 35000, "Used": 34.5528, "SubscribeTime": 1784476800000, "ResetTime": 1785081600000},
    "AFPMonthly":  {"Quota": 100000, "Used": 35210.3625, "SubscribeTime": 1783491488000, "ResetTime": 1786204799000},
    "AFPDaily":    {"Quota": 50000, "Used": 0, "SubscribeTime": 1784908800000, "ResetTime": 1784995200000}
  }
}`

func TestParseVolcengineCredential(t *testing.T) {
	cookie, csrf, err := parseVolcengineCredential("sessionid=abc; uid=1|||csrf-token-xyz")
	if err != nil {
		t.Fatalf("parseVolcengineCredential() err = %v", err)
	}
	if cookie != "sessionid=abc; uid=1" {
		t.Errorf("cookie = %q", cookie)
	}
	if csrf != "csrf-token-xyz" {
		t.Errorf("csrf = %q", csrf)
	}
}

func TestParseVolcengineCredential_TrimSpace(t *testing.T) {
	cookie, csrf, err := parseVolcengineCredential("  a=1  |||  tok  ")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if cookie != "a=1" || csrf != "tok" {
		t.Errorf("cookie=%q csrf=%q", cookie, csrf)
	}
}

func TestParseVolcengineCredential_Errors(t *testing.T) {
	cases := []string{
		"",                    // 空
		"onlycookie",          // 无分隔符
		"|||csftoken",         // 空 cookie
		"cookievalue|||",      // 空 csrf
		"cookie|||csrf|||ext", // 多段（SplitN=2 时第三段并入 csrf，属合法）
	}
	for _, c := range cases[:4] {
		if _, _, err := parseVolcengineCredential(c); err == nil {
			t.Errorf("parseVolcengineCredential(%q) 应报错", c)
		}
	}
	// 多段场景：SplitN=2 保证 csrf 含后续分隔符也合法
	_, csrf, err := parseVolcengineCredential(cases[4])
	if err != nil {
		t.Errorf("多段不应报错: %v", err)
	}
	if csrf != "csrf|||ext" {
		t.Errorf("csrf = %q", csrf)
	}
}

func TestQueryVolcenginePlanTokenPlan_Success(t *testing.T) {
	var gotCookie, gotCSRF, gotMethod, gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotCookie = r.Header.Get("Cookie")
		gotCSRF = r.Header.Get("x-csrf-token")
		buf := make([]byte, r.ContentLength)
		r.Body.Read(buf)
		gotBody = string(buf)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(volcengineUsageResponse))
	}))
	defer ts.Close()

	old := volcenginePlanUsageURL
	volcenginePlanUsageURL = ts.URL
	defer func() { volcenginePlanUsageURL = old }()

	result, err := queryVolcenginePlanTokenPlan(context.Background(), "sessionid=abc|||csrf-123")
	if err != nil {
		t.Fatalf("queryVolcenginePlanTokenPlan() err = %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotCookie != "sessionid=abc" {
		t.Errorf("Cookie = %q", gotCookie)
	}
	if gotCSRF != "csrf-123" {
		t.Errorf("x-csrf-token = %q", gotCSRF)
	}
	if gotBody != "{}" {
		t.Errorf("body = %q, want {}", gotBody)
	}

	// 月配额为主配额
	// API 返回 Used=35210.3625（剩余），实际已用 = 100000 - 35210.3625 = 64789.6375
	if result.QuotaTotal != 100000 {
		t.Errorf("QuotaTotal = %v, want 100000", result.QuotaTotal)
	}
	if result.QuotaUsed != 64789.6375 {
		t.Errorf("QuotaUsed = %v, want 64789.6375", result.QuotaUsed)
	}
	// 周配额为次配额
	// API 返回 Used=34.5528（剩余），实际已用 = 35000 - 34.5528 = 34965.4472
	if result.WeeklyTotal != 35000 {
		t.Errorf("WeeklyTotal = %v, want 35000", result.WeeklyTotal)
	}
	if result.WeeklyUsed != 34965.4472 {
		t.Errorf("WeeklyUsed = %v, want 34965.4472", result.WeeklyUsed)
	}
	// 5 小时档
	// API 返回 Used=34.5528（剩余），实际已用 = 10000 - 34.5528 = 9965.4472
	if result.FiveHourTotal != 10000 {
		t.Errorf("FiveHourTotal = %v, want 10000", result.FiveHourTotal)
	}
	if result.FiveHourUsed != 9965.4472 {
		t.Errorf("FiveHourUsed = %v, want 9965.4472", result.FiveHourUsed)
	}
	if result.FiveHourResetAt == nil {
		t.Fatal("FiveHourResetAt 不应为 nil")
	}
	if want := time.UnixMilli(1784966376000); !result.FiveHourResetAt.Equal(want) {
		t.Errorf("FiveHourResetAt = %v, want %v", result.FiveHourResetAt, want)
	}
	// ResetTime 毫秒时间戳转换
	if result.QuotaResetAt == nil {
		t.Fatal("QuotaResetAt 不应为 nil")
	}
	if want := time.UnixMilli(1786204799000); !result.QuotaResetAt.Equal(want) {
		t.Errorf("QuotaResetAt = %v, want %v", result.QuotaResetAt, want)
	}
	if result.WeeklyResetAt == nil {
		t.Fatal("WeeklyResetAt 不应为 nil")
	}
	if want := time.UnixMilli(1785081600000); !result.WeeklyResetAt.Equal(want) {
		t.Errorf("WeeklyResetAt = %v, want %v", result.WeeklyResetAt, want)
	}
}

func TestQueryVolcenginePlanTokenPlan_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ResponseMetadata": map[string]any{
				"Error": map[string]any{"Code": "InvalidCookie", "Message": "会话已过期"},
			},
			"Result": nil,
		})
	}))
	defer ts.Close()

	old := volcenginePlanUsageURL
	volcenginePlanUsageURL = ts.URL
	defer func() { volcenginePlanUsageURL = old }()

	_, err := queryVolcenginePlanTokenPlan(context.Background(), "bad|||csrf")
	if err == nil {
		t.Fatal("应返回 API 错误")
	}
	if !strings.Contains(err.Error(), "InvalidCookie") {
		t.Errorf("err = %v, 应包含错误码", err)
	}
}

func TestQueryVolcenginePlanTokenPlan_BadCredential(t *testing.T) {
	_, err := queryVolcenginePlanTokenPlan(context.Background(), "no-separator")
	if err == nil {
		t.Fatal("格式错误应报错")
	}
	if !strings.Contains(err.Error(), "凭据格式错误") {
		t.Errorf("err = %v", err)
	}
}
