package codex

import (
	"bytes"
	"context"
	"fmt"
	"github.com/lingyuins/octopus/internal/transformer"
	"net/http"
	"net/url"
	"strings"

	"github.com/lingyuins/octopus/internal/transformer/model"
	"github.com/lingyuins/octopus/internal/transformer/outbound/openai"
)

// Outbound implements the Outbound interface for ChatGPT Codex subscription channels.
//
// Codex uses the OpenAI Responses API format but with a non-standard endpoint
// (/backend-api/codex/responses) and OAuth JSON credentials:
//   - key is a JSON object: {"access_token":"...", "account_id":"...", ...}
//   - Authorization: Bearer {access_token}
//   - chatgpt-account-id: {account_id}
//   - originator: codex_cli_rs
//   - OpenAI-Beta: responses=experimental
//
// Request adjustments (matching Codex CLI behavior):
//   - store must be false
//   - max_output_tokens removed (upstream controls it)
//   - temperature removed
//   - instructions defaults to empty string if absent
type Outbound struct {
	// Delegate stream/response parsing to the standard OpenAI Responses adapter,
	// since the wire format is identical.
	delegate openai.ResponseOutbound
}

// codexOAuthKey is the credential JSON stored as the channel key.
type codexOAuthKey struct {
	AccessToken  string `json:"access_token"`
	AccountID    string `json:"account_id"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

func parseOAuthKey(raw string) (*codexOAuthKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("codex: empty oauth key")
	}
	if !strings.HasPrefix(raw, "{") {
		return nil, fmt.Errorf("codex: key must be a JSON object containing access_token and account_id")
	}
	var key codexOAuthKey
	if err := transformer.Unmarshal([]byte(raw), &key); err != nil {
		return nil, fmt.Errorf("codex: invalid oauth key json: %w", err)
	}
	if strings.TrimSpace(key.AccessToken) == "" {
		return nil, fmt.Errorf("codex: access_token is required")
	}
	if strings.TrimSpace(key.AccountID) == "" {
		return nil, fmt.Errorf("codex: account_id is required")
	}
	return &key, nil
}

func (o *Outbound) TransformRequest(ctx context.Context, request *model.InternalLLMRequest, baseUrl, key string) (*http.Request, error) {
	if request == nil {
		return nil, fmt.Errorf("codex: request is nil")
	}

	oauthKey, err := parseOAuthKey(key)
	if err != nil {
		return nil, err
	}

	// Build the Responses API request from the internal format.
	responsesReq := openai.ConvertToResponsesRequest(request)

	// Codex-specific adjustments (matching Codex CLI behavior):
	// store must be false; rm max_output_tokens and temperature.
	store := false
	responsesReq.Store = &store
	responsesReq.MaxOutputTokens = nil
	responsesReq.Temperature = nil

	// Codex backend requires the instructions field to be present.
	// Default to empty string if absent (consistent with Codex CLI).
	if responsesReq.Instructions == "" {
		responsesReq.Instructions = ""
	}

	body, err := transformer.Marshal(responsesReq)
	if err != nil {
		return nil, fmt.Errorf("codex: failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("codex: failed to create request: %w", err)
	}

	// Codex-specific headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+oauthKey.AccessToken)
	req.Header.Set("chatgpt-account-id", oauthKey.AccountID)
	req.Header.Set("originator", "codex_cli_rs")
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	if request.Stream != nil && *request.Stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}

	// Build URL: {baseUrl}/backend-api/codex/responses
	upstreamURL, err := buildCodexURL(baseUrl, "/backend-api/codex/responses")
	if err != nil {
		return nil, err
	}
	parsedURL, err := url.Parse(upstreamURL)
	if err != nil {
		return nil, fmt.Errorf("codex: failed to parse upstream url: %w", err)
	}
	req.URL = parsedURL
	req.Method = http.MethodPost

	return req, nil
}

func (o *Outbound) TransformResponse(ctx context.Context, response *http.Response) (*model.InternalLLMResponse, error) {
	// Codex response format is identical to OpenAI Responses API.
	return o.delegate.TransformResponse(ctx, response)
}

func (o *Outbound) TransformStream(ctx context.Context, eventData []byte) (*model.InternalLLMResponse, error) {
	// Codex stream format is identical to OpenAI Responses API.
	return o.delegate.TransformStream(ctx, eventData)
}

// buildCodexURL constructs the full upstream URL for Codex requests.
// Codex uses /backend-api/codex/responses as the endpoint path, not /v1/responses.
func buildCodexURL(baseURL, endpointPath string) (string, error) {
	parsed, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil {
		return "", fmt.Errorf("codex: failed to parse base url: %w", err)
	}
	basePath := strings.TrimSuffix(parsed.Path, "/")
	parsed.Path = basePath + endpointPath
	return parsed.String(), nil
}
