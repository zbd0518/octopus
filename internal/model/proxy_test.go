package model

import "testing"

func TestNormalizeProxyURL(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"http", "http://proxy.example.com:8080", "http://proxy.example.com:8080", false},
		{"https with auth", "https://user:pass@proxy.example.com:8443", "https://user:pass@proxy.example.com:8443", false},
		{"socks5", "socks5://127.0.0.1:1080", "socks5://127.0.0.1:1080", false},
		{"legacy socks alias", "socks://127.0.0.1:1080", "socks5://127.0.0.1:1080", false},
		{"legacy socks alias uppercase scheme", "SOCKS://127.0.0.1:1080", "socks5://127.0.0.1:1080", false},
		// base64 payload must survive verbatim (url.URL.String would strip '=')
		{"ss base64", "ss://YWVzLTI1Ni1nY206cGFzcw@example.com:8388", "ss://YWVzLTI1Ni1nY206cGFzcw@example.com:8388", false},
		{"vmess base64", "vmess://eyJhZGQiOiJleGFtcGxlLmNvbSIsInBvcnQiOjQ0MywiaWQiOiJ1dWlkIn0=", "vmess://eyJhZGQiOiJleGFtcGxlLmNvbSIsInBvcnQiOjQ0MywiaWQiOiJ1dWlkIn0=", false},
		{"vless", "vless://uuid@example.com:443?security=tls", "vless://uuid@example.com:443?security=tls", false},
		{"trojan", "trojan://password@example.com:443?security=tls", "trojan://password@example.com:443?security=tls", false},
		{"empty", "", "", true},
		{"unsupported scheme", "quic://example.com:443", "", true},
		{"missing host", "http://", "", true},
		{"legacy socks missing host", "socks://", "", true},
		{"legacy socks empty host", "socks://:1080", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeProxyURL(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NormalizeProxyURL(%q) = %q, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeProxyURL(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeProxyURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestProxyConfigurationValidate(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"http ok", "http://p:8080", false},
		{"ss ok", "ss://YWVzLTI1Ni1nY206cGFzcw@example.com:8388", false},
		{"vmess ok", "vmess://eyJhZGQiOiJleGFtcGxlLmNvbSJ9", false},
		{"trojan ok", "trojan://pass@example.com:443", false},
		{"bad scheme", "ftp://p:21", true},
		{"empty url", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &ProxyConfiguration{Name: "t", URL: tc.url}
			err := p.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Validate(%q) = nil, want error", tc.url)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate(%q) error: %v", tc.url, err)
			}
		})
	}
}
