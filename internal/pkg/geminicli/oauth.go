// Package geminicli 提供 Google Gemini CLI OAuth 流程辅助工具。
//
// 移植自 sub2api backend/internal/pkg/geminicli，去除 sub2api 特有依赖。
// 内置 Gemini CLI 公开 OAuth 客户端凭据（client_secret 需通过环境变量提供，
// 与 sub2api 一致的安全策略）。
package geminicli

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	AuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenURL     = "https://oauth2.googleapis.com/token"

	// 默认 redirect URI（copy/paste 回调流程）。
	DefaultRedirectURI = "http://localhost:1455/auth/callback"

	// Code Assist 默认 scopes
	DefaultCodeAssistScopes = "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile"

	// DefaultAIStudioScopes is the scope set for AI Studio mode (official
	// Generative Language API with OAuth, no project_id required).
	// Ported from sub2api geminicli/constants.go.
	DefaultAIStudioScopes = "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/generative-language.retriever"

	// 内置 Gemini CLI 公开 OAuth 客户端凭据。
	GeminiCLIOAuthClientID = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"

	// GeminiCLIOAuthClientSecret is the public OAuth client secret paired with
	// GeminiCLIOAuthClientID. It is a public credential embedded in Google's
	// own Gemini CLI distribution (mirrors sub2api geminicli/constants.go).
	// B3-#5: with this built-in fallback, refresh/authorize work with zero
	// configuration; operators can still override it via the
	// pool_gemini_client_secret setting or the environment variable below.
	GeminiCLIOAuthClientSecret = "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"

	GeminiCLIOAuthClientSecretEnv = "GEMINI_CLI_OAUTH_CLIENT_SECRET"

	SessionTTL = 30 * time.Minute
)

// envGetter reads environment variables (os.Getenv wrapper, overridable in
// tests). Mirrors the pooltokenrefresh envGetter pattern.
var envGetter = os.Getenv

// OAuthConfig Gemini OAuth 客户端配置。
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	Scopes       string
}

// OAuthSession 存储 Gemini OAuth 流程状态。
type OAuthSession struct {
	State        string    `json:"state"`
	CodeVerifier string    `json:"code_verifier"`
	RedirectURI  string    `json:"redirect_uri"`
	PoolID       int       `json:"pool_id,omitempty"`
	OAuthType    string    `json:"oauth_type,omitempty"` // B3-#5: code_assist / ai_studio / google_one
	CreatedAt    time.Time `json:"created_at"`
}

// SessionStore 内存管理 OAuth 会话。
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*OAuthSession
	stopOnce sync.Once
	stopCh   chan struct{}
}

func NewSessionStore() *SessionStore {
	store := &SessionStore{
		sessions: make(map[string]*OAuthSession),
		stopCh:   make(chan struct{}),
	}
	go store.cleanup()
	return store
}

func (s *SessionStore) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

func (s *SessionStore) Set(sessionID string, session *OAuthSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionID] = session
}

func (s *SessionStore) Get(sessionID string) (*OAuthSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, false
	}
	if time.Since(session.CreatedAt) > SessionTTL {
		return nil, false
	}
	return session, true
}

// GetByState returns the session matching an OAuth state value.
//
// B3-#5: Google's redirect only carries code+state back to the callback URL,
// so the callback must be able to locate the session by state instead of the
// session_id query parameter. Expired sessions are treated as not found.
func (s *SessionStore) GetByState(state string) (*OAuthSession, string, bool) {
	if state == "" {
		return nil, "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for id, session := range s.sessions {
		if session.State != state {
			continue
		}
		if time.Since(session.CreatedAt) > SessionTTL {
			return nil, "", false
		}
		return session, id, true
	}
	return nil, "", false
}

func (s *SessionStore) Delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

func (s *SessionStore) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.mu.Lock()
			for id, session := range s.sessions {
				if time.Since(session.CreatedAt) > SessionTTL {
					delete(s.sessions, id)
				}
			}
			s.mu.Unlock()
		}
	}
}

func GenerateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

func GenerateState() (string, error) {
	b, err := GenerateRandomBytes(32)
	if err != nil {
		return "", err
	}
	return base64URLEncode(b), nil
}

func GenerateSessionID() (string, error) {
	b, err := GenerateRandomBytes(16)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func GenerateCodeVerifier() (string, error) {
	b, err := GenerateRandomBytes(32)
	if err != nil {
		return "", err
	}
	return base64URLEncode(b), nil
}

func GenerateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64URLEncode(hash[:])
}

func base64URLEncode(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

// EffectiveOAuthConfig 返回生效的 OAuth 配置。
//
// B3-#5: client secret 走三级回退链——调用方解析好的 settings 值（cfg.ClientSecret）
// → 环境变量（GeminiCLIOAuthClientSecretEnv）→ 内置公开凭证。内置客户端路径
// （client_id 为空）永不为缺 secret 报错；自定义 client_id 仍要求显式提供
// secret（内置公开 secret 不得配自定义 client_id，与 sub2api 策略一致）。
func EffectiveOAuthConfig(cfg OAuthConfig) (OAuthConfig, error) {
	effective := OAuthConfig{
		ClientID:     strings.TrimSpace(cfg.ClientID),
		ClientSecret: strings.TrimSpace(cfg.ClientSecret),
		Scopes:       strings.TrimSpace(cfg.Scopes),
	}
	if effective.ClientID == "" {
		// 内置 Gemini CLI 客户端：secret 按三级回退链解析，兜底内置公开凭证。
		effective.ClientID = GeminiCLIOAuthClientID
		if effective.ClientSecret == "" {
			effective.ClientSecret = strings.TrimSpace(envGetter(GeminiCLIOAuthClientSecretEnv))
		}
		if effective.ClientSecret == "" {
			effective.ClientSecret = GeminiCLIOAuthClientSecret
		}
	} else if effective.ClientSecret == "" {
		return OAuthConfig{}, fmt.Errorf("OAuth client not configured: set both client_id and client_secret (or leave both empty for built-in client)")
	}
	if effective.Scopes == "" {
		effective.Scopes = DefaultCodeAssistScopes
	}
	return effective, nil
}

// BuildAuthorizationURL 构造 Gemini OAuth 授权 URL。
func BuildAuthorizationURL(cfg OAuthConfig, state, codeChallenge, redirectURI string) (string, error) {
	effectiveCfg, err := EffectiveOAuthConfig(cfg)
	if err != nil {
		return "", err
	}
	redirectURI = strings.TrimSpace(redirectURI)
	if redirectURI == "" {
		redirectURI = DefaultRedirectURI
	}
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", effectiveCfg.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", effectiveCfg.Scopes)
	params.Set("state", state)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	params.Set("include_granted_scopes", "true")
	return fmt.Sprintf("%s?%s", AuthorizeURL, params.Encode()), nil
}

// TokenResponse Google OAuth token 端点响应。
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}
