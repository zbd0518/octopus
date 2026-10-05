package pool

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/model"
)

// B3-#5: the gemini connectivity test must route by credential shape.
// OAuth accounts previously stuffed the whole OAuth JSON into ?key= (guaranteed
// 401/403); now they send a real Bearer-authenticated request to the endpoint
// matching their oauth_type, while apikey keeps the historical ?key= form.
func TestBuildGeminiTestRequest(t *testing.T) {
	acct := func(oauthType string) *model.PoolAccount {
		extra := model.PoolAccountExtra{OAuthType: oauthType, ProjectID: "proj-1"}
		a := &model.PoolAccount{Platform: model.PoolPlatformGemini}
		a.SetExtra(extra)
		return a
	}
	oauthCred := model.PoolCredential{Type: model.PoolTypeOAuth, AccessToken: "ya29.token"}
	apikeyCred := model.PoolCredential{Type: model.PoolTypeAPIKey, APIKey: "AIzaSyKey"}
	const openAIBody = `{"model":"gemini-2.5-pro"}`

	t.Run("apikey keeps query key form and body", func(t *testing.T) {
		reqURL, body, headers := buildGeminiTestRequest(acct(""), apikeyCred,
			"https://generativelanguage.googleapis.com", "gemini-2.5-pro", []byte(openAIBody))
		if !strings.Contains(reqURL, "/v1beta/models/gemini-2.5-pro:generateContent?key=AIzaSyKey") {
			t.Fatalf("reqURL = %q, want ?key= form", reqURL)
		}
		if len(body) != 0 {
			t.Fatalf("body = %s, want no override (caller body preserved)", string(body))
		}
		if _, ok := headers["Authorization"]; ok {
			t.Fatalf("apikey test must not set Authorization")
		}
	})

	t.Run("ai_studio goes to official endpoint with Bearer", func(t *testing.T) {
		reqURL, body, headers := buildGeminiTestRequest(acct(model.OAuthTypeAIStudio), oauthCred,
			"https://generativelanguage.googleapis.com", "gemini-2.5-pro", []byte(openAIBody))
		if reqURL != "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-pro:generateContent" {
			t.Fatalf("reqURL = %q", reqURL)
		}
		if headers["Authorization"] != "Bearer ya29.token" {
			t.Fatalf("Authorization = %q", headers["Authorization"])
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatalf("body not json: %v", err)
		}
		if _, ok := decoded["request"]; ok {
			t.Fatalf("ai_studio body must not be Code Assist wrapped")
		}
		if _, ok := decoded["contents"]; !ok {
			t.Fatalf("ai_studio body missing contents")
		}
	})

	t.Run("code_assist with official base URL switches to Code Assist endpoint", func(t *testing.T) {
		reqURL, body, headers := buildGeminiTestRequest(acct(model.OAuthTypeCodeAssist), oauthCred,
			"https://generativelanguage.googleapis.com", "models/gemini-2.5-pro", []byte(openAIBody))
		if reqURL != "https://cloudcode-pa.googleapis.com/v1internal:generateContent" {
			t.Fatalf("reqURL = %q", reqURL)
		}
		if headers["Authorization"] != "Bearer ya29.token" {
			t.Fatalf("Authorization = %q", headers["Authorization"])
		}
		var decoded struct {
			Model   string `json:"model"`
			Project string `json:"project"`
		}
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatalf("body not json: %v", err)
		}
		if decoded.Model != "gemini-2.5-pro" || decoded.Project != "proj-1" {
			t.Fatalf("wrapped body = model=%q project=%q", decoded.Model, decoded.Project)
		}
	})

	t.Run("google_one falls back to Code Assist endpoint", func(t *testing.T) {
		reqURL, _, headers := buildGeminiTestRequest(acct(model.OAuthTypeGoogleOne), oauthCred,
			"", "gemini-2.5-pro", []byte(openAIBody))
		if reqURL != "https://cloudcode-pa.googleapis.com/v1internal:generateContent" {
			t.Fatalf("reqURL = %q", reqURL)
		}
		if headers["Authorization"] != "Bearer ya29.token" {
			t.Fatalf("Authorization = %q", headers["Authorization"])
		}
	})
}
