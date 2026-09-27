package sitesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/model"
)

// 本文件是 anyrouter.go 的响应体大小上限回归守卫，与 http_limit_test.go 覆盖的
// requestJSON 属于同一类漏网点：anyRouterRequestJSONWithCookies 同样用无上限的
// io.ReadAll(resp.Body) 读响应体，且从同样的裸 ctx 路径可达
// （handlers/site.go 的 syncAllSiteAccounts / checkinAllSiteAccounts 用
// context.Background() 包在 safe.Go 里，既无超时也无取消）。
//
// 复用 http_limit_test.go 的 withSiteResponseLimit 临时调小 maxSiteResponseBytes
// 并在 t.Cleanup 还原；该值是包级全局变量，因此本文件的测试一律不 t.Parallel()。

// TestAnyRouterRequestJSONRejectsOversizedBody 锁住「超限即显式拒绝」这一半。
//
// 未修复时本测试必然失败：无上限的 io.ReadAll 会把这段合法 JSON 完整读走，
// anyRouterParseJSONObject 成功解码 ⇒ 函数返回 nil 错误，下面的 err == nil
// 分支触发 Fatalf。
//
// 注意 anyrouter 路径与 requestJSON 的静默截断后果不同：2xx 响应即使 JSON 非法，
// anyRouterRequestJSONWithCookies 也会走到「return nil, cookieHeader, nil」而完全不报错，
// 也就是说静默截断在这里连一个误导性错误都不会留下，只会让调用方拿到空 payload。
// 这正是必须报显式错误的原因。
func TestAnyRouterRequestJSONRejectsOversizedBody(t *testing.T) {
	withSiteResponseLimit(t, 64)

	// 构造一个合法 JSON，但体积明显超过 64 字节上限。
	bigBody := `{"data":{"items":[` + strings.Repeat(`{"id":"x"},`, 20) + `{"id":"y"}]}}`
	if int64(len(bigBody)) <= 64 {
		t.Fatalf("测试自构造的 body 必须超过上限，len=%d", len(bigBody))
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(bigBody))
	}))
	defer server.Close()

	site := &model.Site{BaseURL: server.URL}
	_, _, err := anyRouterRequestJSONWithCookies(context.Background(), site, http.MethodGet, server.URL, nil, nil)
	if err == nil {
		t.Fatalf("expected anyRouterRequestJSONWithCookies to reject oversized body, got nil error")
	}
	if !strings.Contains(err.Error(), "exceeds") || !strings.Contains(err.Error(), "bytes limit") {
		t.Fatalf("expected explicit size-limit error, got %v", err)
	}
	// 错误文案必须能区分来源：anyrouter 路径报 "anyrouter response exceeds ..."，
	// 与 http.go 的 "site response exceeds ..." 分开，便于日志排查。
	if !strings.Contains(err.Error(), "anyrouter response exceeds") {
		t.Fatalf("expected error to be attributed to the anyrouter path, got %v", err)
	}
	// 关键区分点：必须是「显式超限」而不是「静默截断后解码失败」。
	if strings.Contains(strings.ToLower(err.Error()), "decode") {
		t.Fatalf("body was silently truncated instead of rejected with an explicit limit error: %v", err)
	}
}

// TestAnyRouterRequestJSONAcceptsBodyWithinLimit 锁住修复的另一半：上限之内的响应体
// 必须照常解析，不能被 LimitReader 误伤。
func TestAnyRouterRequestJSONAcceptsBodyWithinLimit(t *testing.T) {
	withSiteResponseLimit(t, 4096)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"items":[{"key":"normal-key","group":"default"}]}}`))
	}))
	defer server.Close()

	site := &model.Site{BaseURL: server.URL}
	payload, _, err := anyRouterRequestJSONWithCookies(context.Background(), site, http.MethodGet, server.URL, nil, nil)
	if err != nil {
		t.Fatalf("anyRouterRequestJSONWithCookies within limit returned error: %v", err)
	}
	if !jsonBool(payload["success"]) {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if tokens := buildSiteTokensFromPayload(payload); len(tokens) != 1 || tokens[0].Token != "normal-key" {
		t.Fatalf("expected the normal response to be parsed as usual, got %#v", payload)
	}
}

// TestAnyRouterRequestJSONSizeLimitBoundary 验证上限判定用的是 max+1 边界语义：
// 恰好等于上限的 body 放行，超过上限 1 字节即拒绝。上限由 body 自身长度反推，
// 不依赖任何硬编码字节数。
func TestAnyRouterRequestJSONSizeLimitBoundary(t *testing.T) {
	// 填充到确定长度的合法 JSON 对象。
	body := `{"data":{"pad":"` + strings.Repeat("a", 100) + `"}}`

	for _, tc := range []struct {
		name    string
		limit   int64
		wantErr bool
	}{
		{name: "恰好等于上限→放行", limit: int64(len(body)), wantErr: false},
		{name: "超上限 1 字节→拒绝", limit: int64(len(body)) - 1, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withSiteResponseLimit(t, tc.limit)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()

			site := &model.Site{BaseURL: server.URL}
			_, _, err := anyRouterRequestJSONWithCookies(context.Background(), site, http.MethodGet, server.URL, nil, nil)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("limit=%d body=%d: expected rejection, got nil error", tc.limit, len(body))
				}
				if !strings.Contains(err.Error(), "exceeds") {
					t.Fatalf("expected size-limit error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("limit=%d body=%d: expected success, got %v", tc.limit, len(body), err)
			}
		})
	}
}
