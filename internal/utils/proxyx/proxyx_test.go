package proxyx

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func sha224Hex(s string) string {
	h := sha256.Sum224([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestNormalizeURL(t *testing.T) {
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
		{"ss", "ss://YWVzLTI1Ni1nY206cGFzcw@example.com:8388", "ss://YWVzLTI1Ni1nY206cGFzcw@example.com:8388", false},
		{"vmess", "vmess://eyJhZGQiOiJleGFtcGxlLmNvbSIsInBvcnQiOjQ0MywiaWQiOiJ1dWlkIn0=", "vmess://eyJhZGQiOiJleGFtcGxlLmNvbSIsInBvcnQiOjQ0MywiaWQiOiJ1dWlkIn0=", false},
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
			got, err := NormalizeURL(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NormalizeURL(%q) = %q, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeURL(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseSS(t *testing.T) {
	// base64("aes-256-gcm:password")
	creds := base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:password"))
	t.Run("sip002 base64 form", func(t *testing.T) {
		cfg, err := parseSS("ss://" + creds + "@example.com:8388")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Method != "aes-256-gcm" || cfg.Password != "password" || cfg.Server != "example.com" || cfg.Port != 8388 {
			t.Fatalf("unexpected cfg: %+v", cfg)
		}
	})
	t.Run("plain method:password form", func(t *testing.T) {
		cfg, err := parseSS("ss://aes-256-gcm:password@example.com:8388")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Method != "aes-256-gcm" || cfg.Password != "password" {
			t.Fatalf("unexpected cfg: %+v", cfg)
		}
	})
	t.Run("missing port", func(t *testing.T) {
		if _, err := parseSS("ss://aes-256-gcm:password@example.com"); err == nil {
			t.Fatal("want error for missing port")
		}
	})
	t.Run("missing password", func(t *testing.T) {
		if _, err := parseSS("ss://YWVzLTI1Ni1nY20@example.com:8388"); err == nil {
			t.Fatal("want error for missing password")
		}
	})
	t.Run("legacy whole-link base64 form", func(t *testing.T) {
		// base64("aes-256-gcm:password@example.com:8388")
		whole := base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:password@example.com:8388"))
		cfg, err := parseSS("ss://" + whole)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Method != "aes-256-gcm" || cfg.Password != "password" || cfg.Server != "example.com" || cfg.Port != 8388 {
			t.Fatalf("unexpected cfg: %+v", cfg)
		}
	})
	t.Run("standard base64 with slash in credentials", func(t *testing.T) {
		// "\xfc\xff\xff" encodes to "/+/+" in base64 (first 6-bit group is
		// 111111), verifying the host is not truncated at the '/'.
		cred := "chacha20-ietf-poly1305:\xfc\xff\xff"
		enc := base64.StdEncoding.EncodeToString([]byte(cred))
		if !strings.Contains(enc, "/") {
			t.Fatalf("expected a '/' in base64, got %s", enc)
		}
		cfg, err := parseSS("ss://" + enc + "@example.com:8388")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Method != "chacha20-ietf-poly1305" || string(cfg.Password) != "\xfc\xff\xff" || cfg.Server != "example.com" || cfg.Port != 8388 {
			t.Fatalf("unexpected cfg: %+v", cfg)
		}
	})
}

func TestParseVMess(t *testing.T) {
	payload := map[string]any{
		"v":    "2",
		"ps":   "test",
		"add":  "example.com",
		"port": 443,
		"id":   "b831381d-6324-4d53-ad4f-8cda48b30811",
		"aid":  0,
		"net":  "tcp",
		"type": "none",
		"tls":  "tls",
		"sni":  "example.com",
	}
	raw, _ := json.Marshal(payload)
	url := "vmess://" + base64.StdEncoding.EncodeToString(raw)

	cfg, err := parseVMess(url)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "example.com" || cfg.Port != 443 {
		t.Fatalf("unexpected server/port: %+v", cfg)
	}
	if cfg.UUID != "b831381d-6324-4d53-ad4f-8cda48b30811" {
		t.Fatalf("unexpected uuid: %+v", cfg)
	}
	if !cfg.TLS || cfg.SNI != "example.com" {
		t.Fatalf("expected tls+sni: %+v", cfg)
	}
	if cfg.AlterID != 0 {
		t.Fatalf("unexpected alter id: %+v", cfg)
	}
}

func TestParseVMessPortAsString(t *testing.T) {
	payload := map[string]any{
		"add":  "example.com",
		"port": "443",
		"id":   "b831381d-6324-4d53-ad4f-8cda48b30811",
	}
	raw, _ := json.Marshal(payload)
	cfg, err := parseVMess("vmess://" + base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 443 {
		t.Fatalf("unexpected port: %+v", cfg)
	}
}

func TestParseVLESS(t *testing.T) {
	cfg, err := parseVLESS("vless://b831381d-6324-4d53-ad4f-8cda48b30811@example.com:443?security=tls&sni=cdn.example.com&type=tcp&flow=xtls-rprx-vision")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != "example.com" || cfg.Port != 443 {
		t.Fatalf("unexpected server/port: %+v", cfg)
	}
	if !cfg.TLS || cfg.SNI != "cdn.example.com" {
		t.Fatalf("expected tls+sni override: %+v", cfg)
	}
	if cfg.Flow != "xtls-rprx-vision" {
		t.Fatalf("unexpected flow: %+v", cfg)
	}
}

func TestParseVLESSNoTLS(t *testing.T) {
	cfg, err := parseVLESS("vless://uuid@example.com:443?security=none")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLS {
		t.Fatalf("expected no tls: %+v", cfg)
	}
}

func TestParseTrojan(t *testing.T) {
	cfg, err := parseTrojan("trojan://my-password@example.com:443?security=tls&sni=cdn.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Password != "my-password" || cfg.Server != "example.com" || cfg.Port != 443 {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
	if cfg.SNI != "cdn.example.com" {
		t.Fatalf("unexpected sni: %+v", cfg)
	}
}

func TestParseTrojanNonTLS(t *testing.T) {
	if _, err := parseTrojan("trojan://pass@example.com:443?security=none"); err == nil {
		t.Fatal("want error for non-tls trojan")
	}
}

func TestTrojanRequest(t *testing.T) {
	// sha224("password") hex
	hash := sha224Hex("password")
	req, err := trojanRequest(hash, "example.com:80")
	if err != nil {
		t.Fatal(err)
	}
	want := hash + "\r\n" + string([]byte{2, byte(len("example.com")), 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm', 0, 80}) + "\r\n"
	if string(req) != want {
		t.Fatalf("trojan request mismatch:\n got %q\nwant %q", req, want)
	}
}

func TestTrojanRequestIPv4(t *testing.T) {
	hash := sha224Hex("pass")
	req, err := trojanRequest(hash, "127.0.0.1:1080")
	if err != nil {
		t.Fatal(err)
	}
	// type 1 (IPv4) + 127.0.0.1 + port 1080 (0x04 0x38)
	if !strings.HasPrefix(string(req), hash+"\r\n\x01\x7f\x00\x00\x01\x04\x38\r\n") {
		t.Fatalf("unexpected ipv4 trojan request: %q", req)
	}
}

func TestTrojanRequestMalformedTarget(t *testing.T) {
	hash := sha224Hex("pass")
	if _, err := trojanRequest(hash, "not-a-host:port"); err == nil {
		t.Fatal("want error for malformed trojan target")
	}
}
