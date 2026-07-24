package codex

import (
	"context"
	"github.com/lingyuins/octopus/internal/transformer"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/transformer/model"
)

func makeOAuthKey(t *testing.T, accessToken, accountID string) string {
	t.Helper()
	b, _ := transformer.Marshal(map[string]any{
		"access_token": accessToken,
		"account_id":   accountID,
	})
	return string(b)
}

func TestParseOAuthKey_Success(t *testing.T) {
	key, err := parseOAuthKey(makeOAuthKey(t, "tok-abc", "acct-123"))
	if err != nil {
		t.Fatalf("parseOAuthKey() error = %v", err)
	}
	if key.AccessToken != "tok-abc" {
		t.Errorf("AccessToken = %q, want %q", key.AccessToken, "tok-abc")
	}
	if key.AccountID != "acct-123" {
		t.Errorf("AccountID = %q, want %q", key.AccountID, "acct-123")
	}
}

func TestParseOAuthKey_Empty(t *testing.T) {
	// After commit aff85d16d, empty key is allowed (returns zero-value oauth key)
	key, err := parseOAuthKey("")
	if err != nil {
		t.Fatalf("parseOAuthKey(\"\") error = %v, want nil (empty key allowed)", err)
	}
	if key == nil {
		t.Fatal("parseOAuthKey(\"\") returned nil key")
	}
	if key.AccessToken != "" || key.AccountID != "" {
		t.Errorf("parseOAuthKey(\"\") = %+v, want zero-value oauth key", key)
	}
}

func TestParseOAuthKey_NotJSON(t *testing.T) {
	if _, err := parseOAuthKey("sk-not-json"); err == nil {
		t.Fatal("expected error for non-JSON key")
	}
}

func TestParseOAuthKey_MissingAccessToken(t *testing.T) {
	b, _ := transformer.Marshal(map[string]any{"account_id": "acct-1"})
	if _, err := parseOAuthKey(string(b)); err == nil {
		t.Fatal("expected error for missing access_token")
	}
}

func TestParseOAuthKey_MissingAccountID(t *testing.T) {
	b, _ := transformer.Marshal(map[string]any{"access_token": "tok-1"})
	if _, err := parseOAuthKey(string(b)); err == nil {
		t.Fatal("expected error for missing account_id")
	}
}

func TestTransformRequest_URLAndHeaders(t *testing.T) {
	o := &Outbound{}
	stream := false
	req := &model.InternalLLMRequest{
		Model:        "gpt-5-codex",
		RawAPIFormat: model.APIFormatOpenAIResponse,
		Stream:       &stream,
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: strPtr("hi")}},
		},
	}

	httpReq, err := o.TransformRequest(context.Background(), req, "https://chatgpt.com", makeOAuthKey(t, "tok-test", "acct-999"))
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	// URL should be https://chatgpt.com/backend-api/codex/responses
	if got, want := httpReq.URL.String(), "https://chatgpt.com/backend-api/codex/responses"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}

	// Headers
	if got := httpReq.Header.Get("Authorization"); got != "Bearer tok-test" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer tok-test")
	}
	if got := httpReq.Header.Get("chatgpt-account-id"); got != "acct-999" {
		t.Errorf("chatgpt-account-id = %q, want %q", got, "acct-999")
	}
	if got := httpReq.Header.Get("originator"); got != "codex_cli_rs" {
		t.Errorf("originator = %q, want %q", got, "codex_cli_rs")
	}
	if got := httpReq.Header.Get("OpenAI-Beta"); got != "responses=experimental" {
		t.Errorf("OpenAI-Beta = %q, want %q", got, "responses=experimental")
	}
	if got := httpReq.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	if got := httpReq.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q, want %q", got, "application/json")
	}
}

func TestTransformRequest_StreamAcceptHeader(t *testing.T) {
	o := &Outbound{}
	stream := true
	req := &model.InternalLLMRequest{
		Model:        "gpt-5-codex",
		RawAPIFormat: model.APIFormatOpenAIResponse,
		Stream:       &stream,
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: strPtr("hi")}},
		},
	}

	httpReq, err := o.TransformRequest(context.Background(), req, "https://chatgpt.com", makeOAuthKey(t, "tok", "acct"))
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	if got := httpReq.Header.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q, want %q", got, "text/event-stream")
	}
}

func TestTransformRequest_StoreFalseAndNoMaxOutputTokens(t *testing.T) {
	o := &Outbound{}
	stream := false
	maxTokens := int64(4096)
	temp := 0.7
	req := &model.InternalLLMRequest{
		Model:               "gpt-5-codex",
		RawAPIFormat:        model.APIFormatOpenAIResponse,
		Stream:              &stream,
		MaxCompletionTokens: &maxTokens,
		Temperature:         &temp,
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: strPtr("hi")}},
		},
	}

	httpReq, err := o.TransformRequest(context.Background(), req, "https://chatgpt.com", makeOAuthKey(t, "tok", "acct"))
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	body, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	var payload map[string]any
	if err := transformer.Unmarshal(body, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	// store must be false
	store, ok := payload["store"]
	if !ok {
		t.Fatal("store field missing from request body")
	}
	if store != false {
		t.Errorf("store = %v, want false", store)
	}

	// max_output_tokens should be absent
	if _, exists := payload["max_output_tokens"]; exists {
		t.Error("max_output_tokens should be removed from Codex request")
	}

	// temperature should be absent
	if _, exists := payload["temperature"]; exists {
		t.Error("temperature should be removed from Codex request")
	}
}

func TestTransformRequest_InvalidKey(t *testing.T) {
	o := &Outbound{}
	stream := false
	req := &model.InternalLLMRequest{
		Model:        "gpt-5-codex",
		RawAPIFormat: model.APIFormatOpenAIResponse,
		Stream:       &stream,
	}

	_, err := o.TransformRequest(context.Background(), req, "https://chatgpt.com", "not-json")
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}

func TestTransformRequest_NilRequest(t *testing.T) {
	o := &Outbound{}
	_, err := o.TransformRequest(context.Background(), nil, "https://chatgpt.com", makeOAuthKey(t, "tok", "acct"))
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

func TestBuildCodexURL(t *testing.T) {
	tests := []struct {
		name         string
		baseURL      string
		endpointPath string
		want         string
	}{
		{"simple", "https://chatgpt.com", "/backend-api/codex/responses", "https://chatgpt.com/backend-api/codex/responses"},
		{"trailing slash", "https://chatgpt.com/", "/backend-api/codex/responses", "https://chatgpt.com/backend-api/codex/responses"},
		{"with path", "https://chatgpt.com/api", "/backend-api/codex/responses", "https://chatgpt.com/api/backend-api/codex/responses"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildCodexURL(tt.baseURL, tt.endpointPath)
			if err != nil {
				t.Fatalf("buildCodexURL() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("buildCodexURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTransformResponse_DelegatesToOpenAI(t *testing.T) {
	// Codex non-stream response is identical to OpenAI Responses API.
	respBody := `{
		"object": "response",
		"id": "resp_123",
		"model": "gpt-5-codex",
		"created_at": 1700000000,
		"output": [
			{"type": "output_text", "text": "Hello world"}
		],
		"status": "completed",
		"usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(respBody))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}

	o := &Outbound{}
	result, err := o.TransformResponse(context.Background(), resp)
	if err != nil {
		t.Fatalf("TransformResponse() error = %v", err)
	}

	if result.ID != "resp_123" {
		t.Errorf("ID = %q, want %q", result.ID, "resp_123")
	}
	if result.Model != "gpt-5-codex" {
		t.Errorf("Model = %q, want %q", result.Model, "gpt-5-codex")
	}
	if len(result.Choices) != 1 {
		t.Fatalf("Choices len = %d, want 1", len(result.Choices))
	}
	if result.Choices[0].Message.Content.Content == nil || *result.Choices[0].Message.Content.Content != "Hello world" {
		t.Errorf("Content = %v, want 'Hello world'", result.Choices[0].Message.Content.Content)
	}
	if result.Usage == nil || result.Usage.TotalTokens != 15 {
		t.Errorf("Usage TotalTokens = %v, want 15", result.Usage)
	}
}

func TestTransformResponse_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"code":401,"message":"invalid token"}}`))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}

	o := &Outbound{}
	_, err = o.TransformResponse(context.Background(), resp)
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should contain status code 401, got: %v", err)
	}
}

func TestTransformStream_DelegatesToOpenAI(t *testing.T) {
	// Codex stream format is identical to OpenAI Responses API stream events.
	streamEvent := `{"type":"response.output_text.delta","delta":"Hello"}`

	o := &Outbound{}
	result, err := o.TransformStream(context.Background(), []byte(streamEvent))
	if err != nil {
		t.Fatalf("TransformStream() error = %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if len(result.Choices) != 1 {
		t.Fatalf("Choices len = %d, want 1", len(result.Choices))
	}
	if result.Choices[0].Delta.Content.Content == nil || *result.Choices[0].Delta.Content.Content != "Hello" {
		t.Errorf("Delta content = %v, want 'Hello'", result.Choices[0].Delta.Content.Content)
	}
}

func TestTransformStream_DoneMarker(t *testing.T) {
	o := &Outbound{}
	result, err := o.TransformStream(context.Background(), []byte("[DONE]"))
	if err != nil {
		t.Fatalf("TransformStream() error = %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.Object != "[DONE]" {
		t.Errorf("Object = %q, want %q", result.Object, "[DONE]")
	}
}

// strPtr returns a pointer to the given string.
func strPtr(s string) *string {
	return &s
}
