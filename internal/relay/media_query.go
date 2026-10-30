package relay

import (
	"fmt"
	"net/url"
	"strings"
)

func isUnsafeMediaQueryName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "api-key", "api_key", "x-api-key", "x_api_key",
		"key", "access_token", "token", "authorization",
		"secret", "client_secret", "client-secret",
		"pass" + "word", "passwd", "credential", "credentials",
		"cookie", "session", "session_id", "sessionid",
		"conversation_id", "last_event_id", "resume_from_sequence":
		return true
	default:
		return false
	}
}

// filterSafeMediaRawQuery removes credential and relay-session parameters while
// preserving the original encoding and order of the remaining pairs.
func filterSafeMediaRawQuery(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	parts := strings.Split(raw, "&")
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		name := part
		if index := strings.IndexByte(part, '='); index >= 0 {
			name = part[:index]
		}
		decoded, err := url.QueryUnescape(name)
		if err != nil {
			return "", fmt.Errorf("invalid query parameter: %w", err)
		}
		if isUnsafeMediaQueryName(decoded) {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, "&"), nil
}

func mergeSafeMediaQuery(raw string, client url.Values) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	for name, values := range client {
		if isUnsafeMediaQueryName(name) {
			continue
		}
		if _, exists := query[name]; !exists {
			query[name] = append([]string(nil), values...)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
