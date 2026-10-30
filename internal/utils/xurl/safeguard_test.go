package xurl

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsDisallowedIP(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{"loopback v4", "127.0.0.1", true},
		{"loopback v4 high", "127.255.255.255", true},
		{"loopback v6", "::1", true},
		{"private 10/8", "10.0.0.1", true},
		{"private 172.16/12", "172.16.0.1", true},
		{"private 192.168/16", "192.168.1.1", true},
		{"link-local metadata 169.254.169.254", "169.254.169.254", true},
		{"link-local 169.254 other", "169.254.0.1", true},
		{"link-local v6 fe80", "fe80::1", true},
		{"unspecified v4", "0.0.0.0", true},
		{"unspecified v6", "::", true},
		{"multicast v4", "224.0.0.1", true},
		{"multicast v6 ff02", "ff02::1", true},
		{"public 8.8.8.8", "8.8.8.8", false},
		{"public 1.1.1.1", "1.1.1.1", false},
		{"public v6", "2606:4700:4700::1111", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("invalid test ip %q", tt.ip)
			}
			if got := IsDisallowedIP(ip); got != tt.want {
				t.Errorf("IsDisallowedIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestIsDisallowedIP_Nil(t *testing.T) {
	if !IsDisallowedIP(nil) {
		t.Error("IsDisallowedIP(nil) = false, want true")
	}
}

func TestAssertSafeURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"empty", "", true},
		{"file scheme", "file:///etc/passwd", true},
		{"ftp scheme", "ftp://example.com/", true},
		{"gopher scheme", "gopher://127.0.0.1/", true},
		{"no scheme", "example.com/", true},
		{"loopback ip", "http://127.0.0.1/", true},
		{"metadata ip", "http://169.254.169.254/latest/meta-data/", true},
		{"private ip", "http://10.0.0.1/", true},
		{"localhost", "http://localhost/", true},
		{"dotlocal", "http://foo.local/", true},
		// Literal public IPs are validated without DNS, so this stays stable.
		{"public ip", "http://8.8.8.8/", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := AssertSafeURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("AssertSafeURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestAssertSafeHost_Nil(t *testing.T) {
	if err := AssertSafeHost(nil); err == nil {
		t.Error("AssertSafeHost(nil) = nil, want error")
	}
}

func TestAssertSafeRequest(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"nil request", "", true},
		{"loopback", "http://127.0.0.1/v1/chat", true},
		{"metadata", "http://169.254.169.254/latest/meta-data/", true},
		{"private", "http://10.0.0.1/v1/chat", true},
		{"public ip", "http://8.8.8.8/v1/chat", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req *http.Request
			if tt.url != "" {
				var err error
				req, err = http.NewRequest(http.MethodPost, tt.url, nil)
				if err != nil {
					t.Fatalf("new request: %v", err)
				}
			}
			err := AssertSafeRequest(req)
			if (err != nil) != tt.wantErr {
				t.Errorf("AssertSafeRequest(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestAssertSafeRequestWithPin_PinsIP(t *testing.T) {
	// 公网 IP 不需 DNS 解析，AssertSafeRequestWithPin 应返回携带钉入 IP 的 context。
	req, err := http.NewRequest(http.MethodPost, "http://8.8.8.8/v1/chat", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	safeCtx, err := AssertSafeRequestWithPin(req)
	if err != nil {
		t.Fatalf("AssertSafeRequestWithPin: %v", err)
	}

	ip, ok := safeCtx.Value(safeIPKey{}).(net.IP)
	if !ok || ip == nil {
		t.Fatal("expected safe IP pinned in context")
	}
	if ip.String() != "8.8.8.8" {
		t.Fatalf("pinned IP = %s, want 8.8.8.8", ip)
	}
}

func TestAssertSafeRequestWithPin_RejectsMetadata(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "http://169.254.169.254/latest/meta-data/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	if _, err := AssertSafeRequestWithPin(req); err == nil {
		t.Fatal("expected error for metadata endpoint, got nil")
	}
}

func TestSafeDialContext_UsesPinnedIP(t *testing.T) {
	// 携带钉入 IP 时，SafeDialContext 应连该 IP 而非原 addr 中的 host。
	// 用一个本地 listener 验证：钉入的 IP 指向 listener，原 addr 用不存在的
	// host + listener 的端口。若未用钉入 IP 会 DNS 失败。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			conn.Close()
		}
	}()

	addr := ln.Addr().String()
	ip, port, _ := net.SplitHostPort(addr)
	pinnedIP := net.ParseIP(ip)

	ctx := context.WithValue(context.Background(), safeIPKey{}, pinnedIP)
	// addr 用不存在的 hostname + listener 端口；钉入 IP 会被使用，端口保留。
	conn, err := SafeDialContext(ctx, "tcp", "nonexistent-host-xyz.invalid:"+port)
	if err != nil {
		t.Fatalf("SafeDialContext with pinned IP: %v", err)
	}
	conn.Close()
}
func TestCheckRedirectSafeWithPinDialsCurrentHop(t *testing.T) {
	SetSSRFAllowPrivateForTest(true)
	t.Cleanup(func() { SetSSRFAllowPrivateForTest(false) })

	var destinationHits int
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationHits++
		_, _ = io.WriteString(w, r.URL.Path)
	}))
	defer destination.Close()
	var destinationURL string
	hops := 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops++
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, destinationURL+"/next", http.StatusFound)
		case "/multi":
			http.Redirect(w, r, "/middle", http.StatusFound)
		case "/middle":
			http.Redirect(w, r, destinationURL+"/final", http.StatusFound)
		default:
			_, _ = io.WriteString(w, r.URL.Path)
		}
	}))
	defer origin.Close()
	destinationURL = destination.URL

	client := &http.Client{
		Transport:     &http.Transport{DialContext: SafeDialContext},
		CheckRedirect: CheckRedirectSafeWithPin(5),
	}
	for _, tc := range []struct{ path, want string }{{"/start", "/next"}, {"/multi", "/final"}} {
		req, err := http.NewRequest(http.MethodGet, origin.URL+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		requestCtx, err := AssertSafeRequestWithPin(req)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req.WithContext(requestCtx))
		if err != nil {
			t.Fatalf("%s redirect: %v", tc.path, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil || string(body) != tc.want {
			t.Fatalf("%s body = %q, err = %v, want %q", tc.path, body, readErr, tc.want)
		}
	}
	if destinationHits != 2 || hops != 3 {
		t.Fatalf("destination hits = %d, origin hits = %d, want requests to reach each current hop", destinationHits, hops)
	}
}

func TestCheckRedirectSafeWithPinRejectsUnsafeAndExcessHops(t *testing.T) {
	SetSSRFAllowPrivateForTest(true)
	t.Cleanup(func() { SetSSRFAllowPrivateForTest(false) })

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/private" {
			http.Redirect(w, r, "http://unsafe.local/private", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/redirect", http.StatusFound)
	}))
	defer origin.Close()
	client := &http.Client{
		Transport:     &http.Transport{DialContext: SafeDialContext},
		CheckRedirect: CheckRedirectSafeWithPin(1),
	}
	for _, tc := range []struct {
		path         string
		maxRedirects int
		contains     string
	}{{"/private", 5, "not allowed"}, {"/redirect", 1, "stopped after 1 redirects"}} {
		client.CheckRedirect = CheckRedirectSafeWithPin(tc.maxRedirects)
		req, err := http.NewRequest(http.MethodGet, origin.URL+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		requestCtx, err := AssertSafeRequestWithPin(req)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req.WithContext(requestCtx))
		if resp != nil {
			_ = resp.Body.Close()
		}
		if err == nil || !strings.Contains(err.Error(), tc.contains) {
			t.Fatalf("%s error = %v, want %q", tc.path, err, tc.contains)
		}
	}

	req, err := http.NewRequest(http.MethodGet, origin.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.URL.Scheme = "file"
	if err := CheckRedirectSafeWithPin(5)(req, nil); err == nil {
		t.Fatal("non-HTTP redirect should be rejected")
	}
}

func TestSafeDialContext_FallbackNoPin(t *testing.T) {
	// 未携带钉入 IP 时按原 addr 默认拨号；已关闭的本地端口提供确定的失败结果。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	conn, err := SafeDialContext(ctx, "tcp", addr)
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil {
		t.Fatal("expected unpinned dial to the closed original address to fail")
	}
}
