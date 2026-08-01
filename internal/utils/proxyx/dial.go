package proxyx

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sagernet/sing-shadowsocks/shadowimpl"
	"github.com/sagernet/sing-vmess"
	"github.com/sagernet/sing-vmess/vless"
	"github.com/sagernet/sing/common/metadata"
	"golang.org/x/net/proxy"
)

// silentLogger implements the sing logger.Logger interface by discarding
// everything. sing clients use the logger only for diagnostics.
type silentLogger struct{}

func (silentLogger) Trace(...any) {}
func (silentLogger) Debug(...any) {}
func (silentLogger) Info(...any)  {}
func (silentLogger) Warn(...any)  {}
func (silentLogger) Error(...any) {}
func (silentLogger) Fatal(...any) {}
func (silentLogger) Panic(...any) {}

// DialContextFunc dials a target address through the configured proxy.
// It matches the http.Transport.DialContext signature.
type DialContextFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// NewDialContext parses proxyURL and returns a dial function that routes
// connections through the proxy. scheme must be one of http/https/socks5
// (handled via Transport.Proxy when used with NewTransport) or one of
// ss/vmess/vless/trojan (handled by wrapping the raw connection).
func NewDialContext(proxyURL string) (DialContextFunc, error) {
	normalized, err := NormalizeURL(proxyURL)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy url: %w", err)
	}

	switch ProxyScheme(parsed.Scheme) {
	case SchemeSS:
		cfg, err := parseSS(normalized)
		if err != nil {
			return nil, err
		}
		return newSSDialContext(cfg), nil
	case SchemeVMess:
		cfg, err := parseVMess(normalized)
		if err != nil {
			return nil, err
		}
		return newVMessDialContext(cfg), nil
	case SchemeVLESS:
		cfg, err := parseVLESS(normalized)
		if err != nil {
			return nil, err
		}
		return newVLESSDialContext(cfg), nil
	case SchemeTrojan:
		cfg, err := parseTrojan(normalized)
		if err != nil {
			return nil, err
		}
		return newTrojanDialContext(cfg), nil
	default:
		// http/https/socks5 are handled at the transport level (ProxyURL /
		// socks5 dialer); expose a dialer for socks5 and CONNECT-style proxies
		// here too for completeness.
		switch ProxyScheme(parsed.Scheme) {
		case SchemeSOCKS5:
			dialer, err := proxy.FromURL(parsed, proxy.Direct)
			if err != nil {
				return nil, fmt.Errorf("invalid socks5 proxy: %w", err)
			}
			return func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			}, nil
		case SchemeHTTP, SchemeHTTPS:
			// HTTP proxies require CONNECT semantics that are only available
			// through http.Transport.Proxy. Provide a dialer that establishes
			// the CONNECT tunnel manually so callers can still use it.
			return newHTTPConnectDialContext(normalized), nil
		default:
			return nil, fmt.Errorf("unsupported proxy scheme: %s", parsed.Scheme)
		}
	}
}

// newSSDialContext returns a dialer that wraps connections with Shadowsocks.
func newSSDialContext(cfg *ssParsed) DialContextFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if network != "tcp" {
			return nil, fmt.Errorf("shadowsocks only supports tcp, got %q", network)
		}
		method, err := shadowimpl.FetchMethod(cfg.Method, cfg.Password, time.Now)
		if err != nil {
			return nil, fmt.Errorf("shadowsocks method %q: %w", cfg.Method, err)
		}
		serverAddr := parseServerAddr(cfg.Server, cfg.Port)
		raw, err := dialTCP(ctx, serverAddr)
		if err != nil {
			return nil, fmt.Errorf("dial shadowsocks server %s: %w", serverAddr, err)
		}
		destination := metadata.ParseSocksaddr(addr)
		conn, err := method.DialConn(raw, destination)
		if err != nil {
			raw.Close()
			return nil, fmt.Errorf("shadowsocks handshake: %w", err)
		}
		return conn, nil
	}
}

// newVMessDialContext returns a dialer that wraps connections with VMess.
func newVMessDialContext(cfg *vmessParsed) DialContextFunc {
	client, err := vmess.NewClient(cfg.UUID, cfg.Security, cfg.AlterID)
	if err != nil {
		// Configuration is validated at construction; surface as a dialer that
		// always errors.
		return func(context.Context, string, string) (net.Conn, error) {
			return nil, fmt.Errorf("invalid vmess client: %w", err)
		}
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if network != "tcp" {
			return nil, fmt.Errorf("vmess only supports tcp, got %q", network)
		}
		serverAddr := parseServerAddr(cfg.Server, cfg.Port)
		raw, err := dialTCP(ctx, serverAddr)
		if err != nil {
			return nil, fmt.Errorf("dial vmess server %s: %w", serverAddr, err)
		}
		if cfg.TLS {
			tlsConn := tls.Client(raw, &tls.Config{
				ServerName:         cfg.SNI,
				InsecureSkipVerify: cfg.AllowInsecure, //nolint:gosec // user-configured proxy
			})
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				raw.Close()
				return nil, fmt.Errorf("vmess tls handshake: %w", err)
			}
			raw = tlsConn
		}
		destination := metadata.ParseSocksaddr(addr)
		conn, err := client.DialConn(raw, destination)
		if err != nil {
			raw.Close()
			return nil, fmt.Errorf("vmess handshake: %w", err)
		}
		return conn, nil
	}
}

// newVLESSDialContext returns a dialer that wraps connections with VLESS.
func newVLESSDialContext(cfg *vlessParsed) DialContextFunc {
	client, err := vless.NewClient(cfg.UUID, cfg.Flow, silentLogger{})
	if err != nil {
		return func(context.Context, string, string) (net.Conn, error) {
			return nil, fmt.Errorf("invalid vless client: %w", err)
		}
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if network != "tcp" {
			return nil, fmt.Errorf("vless only supports tcp, got %q", network)
		}
		serverAddr := parseServerAddr(cfg.Server, cfg.Port)
		raw, err := dialTCP(ctx, serverAddr)
		if err != nil {
			return nil, fmt.Errorf("dial vless server %s: %w", serverAddr, err)
		}
		if cfg.TLS {
			tlsConn := tls.Client(raw, &tls.Config{
				ServerName:         cfg.SNI,
				InsecureSkipVerify: cfg.AllowInsecure, //nolint:gosec // user-configured proxy
			})
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				raw.Close()
				return nil, fmt.Errorf("vless tls handshake: %w", err)
			}
			raw = tlsConn
		}
		destination := metadata.ParseSocksaddr(addr)
		conn, err := client.DialConn(raw, destination)
		if err != nil {
			raw.Close()
			return nil, fmt.Errorf("vless handshake: %w", err)
		}
		return conn, nil
	}
}

// newTrojanDialContext returns a dialer that wraps connections with the
// Trojan protocol. Trojan is a plaintext protocol over TLS: the client
// connects, sends hex(SHA224(password))\r\n<target>\r\n, then streams data.
func newTrojanDialContext(cfg *trojanParsed) DialContextFunc {
	hash := sha256.Sum224([]byte(cfg.Password))
	passwordHash := hex.EncodeToString(hash[:])
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if network != "tcp" {
			return nil, fmt.Errorf("trojan only supports tcp, got %q", network)
		}
		serverAddr := parseServerAddr(cfg.Server, cfg.Port)
		raw, err := dialTCP(ctx, serverAddr)
		if err != nil {
			return nil, fmt.Errorf("dial trojan server %s: %w", serverAddr, err)
		}
		tlsConn := tls.Client(raw, &tls.Config{
			ServerName:         cfg.SNI,
			InsecureSkipVerify: cfg.AllowInsecure, //nolint:gosec // user-configured proxy
		})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			raw.Close()
			return nil, fmt.Errorf("trojan tls handshake: %w", err)
		}

		request, err := trojanRequest(passwordHash, addr)
		if err != nil {
			tlsConn.Close()
			return nil, err
		}
		if _, err := tlsConn.Write(request); err != nil {
			tlsConn.Close()
			return nil, fmt.Errorf("trojan handshake: %w", err)
		}
		return tlsConn, nil
	}
}

// trojanRequest builds the initial Trojan handshake payload:
//
//	hex(SHA224(password)) "\r\n" address "\r\n"
//
// where address is one byte type + address bytes + 2-byte big-endian port.
func trojanRequest(passwordHash, target string) ([]byte, error) {
	var builder strings.Builder
	builder.WriteString(passwordHash)
	builder.WriteString("\r\n")
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return nil, fmt.Errorf("invalid trojan target %q: %w", target, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid trojan target port %q: %w", portStr, err)
	}
	writeTrojanAddr(&builder, host)
	_ = binary.Write(&byteWriter{builder: &builder}, binary.BigEndian, uint16(port))
	builder.WriteString("\r\n")
	return []byte(builder.String()), nil
}

// byteWriter adapts strings.Builder to binary.Writer.
type byteWriter struct{ builder *strings.Builder }

func (w *byteWriter) Write(p []byte) (int, error) { return w.builder.Write(p) }

// writeTrojanAddr encodes a host into the trojan address format:
// 1 = IPv4, 2 = domain, 3 = IPv6.
func writeTrojanAddr(builder *strings.Builder, host string) {
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			builder.WriteByte(1)
			builder.Write(v4)
			return
		}
		builder.WriteByte(3)
		builder.Write(ip.To16())
		return
	}
	builder.WriteByte(2)
	builder.WriteByte(byte(len(host)))
	builder.WriteString(host)
}

// newHTTPConnectDialContext manually establishes an HTTP CONNECT tunnel so
// http:// and https:// proxies can also be used via DialContext.
func newHTTPConnectDialContext(proxyURL string) DialContextFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if network != "tcp" {
			return nil, fmt.Errorf("http proxy only supports tcp, got %q", network)
		}
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}
		proxyAddr := parsed.Host
		raw, err := dialTCP(ctx, proxyAddr)
		if err != nil {
			return nil, fmt.Errorf("dial http proxy %s: %w", proxyAddr, err)
		}
		if parsed.Scheme == "https" {
			tlsConn := tls.Client(raw, &tls.Config{ServerName: parsed.Hostname()})
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				raw.Close()
				return nil, fmt.Errorf("https proxy tls handshake: %w", err)
			}
			raw = tlsConn
		}

		req := "CONNECT " + addr + " HTTP/1.1\r\nHost: " + addr + "\r\n"
		if parsed.User != nil {
			username := parsed.User.Username()
			password, _ := parsed.User.Password()
			req += "Proxy-Authorization: Basic " + basicAuth(username, password) + "\r\n"
		}
		req += "\r\n"
		if _, err := raw.Write([]byte(req)); err != nil {
			raw.Close()
			return nil, fmt.Errorf("http connect: %w", err)
		}

		// Parse the response with bufio so header parsing is buffered (no
		// per-byte syscalls) and any bytes the server sent right after the
		// headers are kept for the tunnel via bufferedConn.
		br := bufio.NewReader(raw)
		resp, err := http.ReadResponse(br, nil)
		if err != nil {
			raw.Close()
			return nil, fmt.Errorf("http connect: %w", err)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw.Close()
			return nil, fmt.Errorf("http proxy connect failed: %d", resp.StatusCode)
		}
		if br.Buffered() > 0 {
			return &bufferedConn{Conn: raw, reader: br}, nil
		}
		return raw, nil
	}
}

// bufferedConn keeps bytes already read into br available to subsequent Read
// calls, so tunnel data that arrived together with the CONNECT response is not
// lost.
type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

// dialTCP dials addr honoring ctx cancellation and a dial timeout.
func dialTCP(ctx context.Context, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 20 * time.Second}
	return dialer.DialContext(ctx, "tcp", addr)
}

// basicAuth returns the base64 encoding of user:pass for proxy auth.
func basicAuth(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}
