package plugin

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lingyuins/octopus/internal/utils/xurl"
)

type HTTPInput struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	Base64  bool              `json:"base64,omitempty"`
}

func executeHTTP(ctx context.Context, m Manifest, in HostRequest) (any, error) {
	if !m.HasPermission("http") {
		return nil, fmt.Errorf("http permission denied")
	}
	var request HTTPInput
	if err := json.Unmarshal(in.Payload, &request); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(request.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, fmt.Errorf("only HTTPS URLs are allowed")
	}
	origin := strings.ToLower(parsed.Scheme + "://" + parsed.Host)
	allowed := false
	for _, item := range m.HTTPOrigins {
		if strings.EqualFold(item, origin) {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("HTTP origin is not authorized")
	}
	method := strings.ToUpper(request.Method)
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return nil, fmt.Errorf("HTTP method is not allowed")
	}
	body := []byte(request.Body)
	if request.Base64 {
		body, err = base64.StdEncoding.DecodeString(request.Body)
		if err != nil {
			return nil, err
		}
	}
	if len(body) > MaxPayloadBytes {
		return nil, ErrPayloadTooLarge
	}
	req, err := http.NewRequestWithContext(ctx, method, request.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for name, value := range request.Headers {
		lower := strings.ToLower(name)
		if lower == "host" || lower == "content-length" || lower == "connection" || lower == "transfer-encoding" {
			return nil, fmt.Errorf("HTTP header %q is not allowed", name)
		}
		req.Header.Set(name, value)
	}
	requestCtx, err := xurl.AssertSafeRequestWithPin(req)
	if err != nil {
		return nil, err
	}
	*req = *req.WithContext(requestCtx)
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: xurl.SafeDialContext, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ForceAttemptHTTP2: true},
		CheckRedirect: func(next *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			nextOrigin := strings.ToLower(next.URL.Scheme + "://" + next.URL.Host)
			if next.URL.Scheme != "https" || nextOrigin != origin {
				return fmt.Errorf("redirect to an unauthorized origin")
			}
			nextCtx, err := xurl.AssertSafeRequestWithPin(next)
			if err != nil {
				return err
			}
			*next = *next.WithContext(nextCtx)
			return nil
		}, Timeout: 15 * time.Second,
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, MaxPayloadBytes+1))
	if err != nil {
		return nil, err
	}
	if len(responseBody) > MaxPayloadBytes {
		return nil, ErrPayloadTooLarge
	}
	headers := map[string]string{}
	for name := range response.Header {
		headers[name] = response.Header.Get(name)
	}
	return map[string]any{"status": response.StatusCode, "headers": headers, "body": responseBody, "base64": true}, nil
}
