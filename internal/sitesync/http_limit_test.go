package sitesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/model"
)

// withSiteResponseLimit 临时把 maxSiteResponseBytes 调到测试需要的量级，
// t.Cleanup 还原。maxSiteResponseBytes 声明成 var 正是为了让本回归测试能把
// 上限调小后验证「超限即拒绝」这条路径，而不用真的分配 10 MiB+ 内存。
//
// 不并行：maxSiteResponseBytes 是包级全局值。
func withSiteResponseLimit(t *testing.T, limit int64) {
	t.Helper()
	restore := maxSiteResponseBytes
	maxSiteResponseBytes = limit
	t.Cleanup(func() { maxSiteResponseBytes = restore })
}

// TestRequestJSONRejectsOversizedBody 是 S-1 的回归守卫。
//
// 缺陷：requestJSON 用无上限的 io.ReadAll(resp.Body) 读响应体。default 档共享
// 客户端已把整体 Client.Timeout 改为 0（不限，改用 Transport.ResponseHeaderTimeout），
// 于是故障/恶意上游发完响应头后无限涓流 body 时，既没有超时也没有取消
// （裸 ctx 入口 handlers/site.go 的全量同步/签到走 safe.Go），该 goroutine 与连接
// 会被永久占用。
//
// 修复给读取加上 io.LimitReader(maxSiteResponseBytes+1)，并在超限时返回显式错误
// （而不是静默截断——截断后的半截 JSON 会让解码失败并给出误导性信息）。
//
// 未修复时本测试必然失败：无上限的 io.ReadAll 会把这段合法 JSON 完整读走并成功
// 解码，requestJSON 返回 nil 错误 ⇒ 下面的 err == nil 分支触发 Fatalf。
func TestRequestJSONRejectsOversizedBody(t *testing.T) {
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

	_, err := requestJSON(context.Background(), &model.Site{BaseURL: server.URL}, http.MethodGet, server.URL, nil, nil)
	if err == nil {
		t.Fatalf("expected requestJSON to reject oversized body, got nil error")
	}
	if !strings.Contains(err.Error(), "exceeds") || !strings.Contains(err.Error(), "bytes limit") {
		t.Fatalf("expected explicit size-limit error, got %v", err)
	}
	// 关键区分点：必须是「显式超限」而不是「静默截断后解码失败」。
	if strings.Contains(err.Error(), "decode response failed") {
		t.Fatalf("body was silently truncated instead of rejected with an explicit limit error: %v", err)
	}
}

// TestRequestJSONAcceptsBodyWithinLimit 锁住修复的另一半：上限之内的响应体
// 必须照常解析，不能被 LimitReader 误伤。
func TestRequestJSONAcceptsBodyWithinLimit(t *testing.T) {
	withSiteResponseLimit(t, 4096)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer server.Close()

	payload, err := requestJSON(context.Background(), &model.Site{BaseURL: server.URL}, http.MethodGet, server.URL, nil, nil)
	if err != nil {
		t.Fatalf("requestJSON within limit returned error: %v", err)
	}
	if nestedValue(payload, "data", "ok") != true {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

// TestRequestJSONSizeLimitBoundary 验证上限判定用的是 max+1 边界语义：
// 恰好等于上限的 body 放行，超过上限 1 字节即拒绝。上限由 body 自身长度反推，
// 不依赖任何硬编码字节数。
func TestRequestJSONSizeLimitBoundary(t *testing.T) {
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

			_, err := requestJSON(context.Background(), &model.Site{BaseURL: server.URL}, http.MethodGet, server.URL, nil, nil)
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
