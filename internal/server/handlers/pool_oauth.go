package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lingyuins/octopus/internal/conf"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
	"github.com/lingyuins/octopus/internal/pkg/geminicli"
	"github.com/lingyuins/octopus/internal/pkg/oauth"
	"github.com/lingyuins/octopus/internal/pkg/openai"
	"github.com/lingyuins/octopus/internal/pkg/xai"
	"github.com/lingyuins/octopus/internal/server/router"
	"github.com/lingyuins/octopus/internal/utils/log"
)

// OAuth 回调端点不挂 Auth middleware：回调阶段用 state/session 校验。
// initiate 阶段需要登录态（由调用方在前端鉴权后请求），这里仍要求 Auth 以保护 initiate。
func init() {
	router.NewGroupRouter("/api/v1/pool/oauth").
		AddRoute(
			router.NewRoute("/initiate", http.MethodGet).Handle(oauthInitiate),
		).
		AddRoute(
			router.NewRoute("/callback", http.MethodGet).Handle(oauthCallback),
		)
}

// 全局 SessionStore（4 平台共用，按 sessionID 区分）。
var (
	anthropicSessions = oauth.NewSessionStore()
	openaiSessions    = openai.NewSessionStore()
	geminiSessions    = geminicli.NewSessionStore()
	grokSessions      = xai.NewSessionStore()
)

// externalRedirectURL 返回 OAuth 回调地址：{ExternalURL}/api/v1/pool/oauth/callback。
func externalRedirectURL() (string, error) {
	base := strings.TrimRight(strings.TrimSpace(conf.AppConfig.Server.ExternalURL), "/")
	if base == "" {
		host := conf.AppConfig.Server.Host
		port := conf.AppConfig.Server.Port
		if host == "" || host == "0.0.0.0" {
			host = "127.0.0.1"
		}
		base = fmt.Sprintf("http://%s:%d", host, port)
	}
	return base + "/api/v1/pool/oauth/callback", nil
}

func oauthInitiate(c *gin.Context) {
	platform := strings.TrimSpace(c.Query("platform"))
	poolIDStr := c.Query("pool_id")
	poolID, err := strconv.Atoi(poolIDStr)
	if err != nil || poolID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pool_id"})
		return
	}

	redirectURI, err := externalRedirectURL()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	switch platform {
	case model.PoolPlatformAnthropic:
		state, err := oauth.GenerateState()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "generate state failed"})
			return
		}
		verifier, err := oauth.GenerateCodeVerifier()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "generate verifier failed"})
			return
		}
		challenge := oauth.GenerateCodeChallenge(verifier)
		sessionID, err := oauth.GenerateSessionID()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "generate session failed"})
			return
		}
		anthropicSessions.Set(sessionID, &oauth.OAuthSession{
			State:        state,
			CodeVerifier: verifier,
			Scope:        oauth.ScopeOAuth,
			PoolID:       poolID,
			RedirectURI:  redirectURI,
			CreatedAt:    time.Now(),
		})
		authURL := oauth.BuildAuthorizationURL(state, challenge, oauth.ScopeOAuth, redirectURI)
		c.JSON(http.StatusOK, gin.H{"auth_url": authURL, "session_id": sessionID})

	case model.PoolPlatformOpenAI:
		state, err := openai.GenerateState()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "generate state failed"})
			return
		}
		verifier, err := openai.GenerateCodeVerifier()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "generate verifier failed"})
			return
		}
		challenge := openai.GenerateCodeChallenge(verifier)
		sessionID, err := openai.GenerateSessionID()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "generate session failed"})
			return
		}
		openaiSessions.Set(sessionID, &openai.OAuthSession{
			State:        state,
			CodeVerifier: verifier,
			RedirectURI:  redirectURI,
			PoolID:       poolID,
			CreatedAt:    time.Now(),
		})
		authURL := openai.BuildAuthorizationURL(state, challenge, redirectURI)
		c.JSON(http.StatusOK, gin.H{"auth_url": authURL, "session_id": sessionID})

	case model.PoolPlatformGemini:
		state, err := geminicli.GenerateState()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "generate state failed"})
			return
		}
		verifier, err := geminicli.GenerateCodeVerifier()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "generate verifier failed"})
			return
		}
		challenge := geminicli.GenerateCodeChallenge(verifier)
		sessionID, err := geminicli.GenerateSessionID()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "generate session failed"})
			return
		}
		geminiSessions.Set(sessionID, &geminicli.OAuthSession{
			State:        state,
			CodeVerifier: verifier,
			RedirectURI:  redirectURI,
			PoolID:       poolID,
			CreatedAt:    time.Now(),
		})
		authURL, err := geminicli.BuildAuthorizationURL(geminicli.OAuthConfig{}, state, challenge, redirectURI)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"auth_url": authURL, "session_id": sessionID})

	case model.PoolPlatformGrok:
		state, err := xai.GenerateState()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "generate state failed"})
			return
		}
		verifier, err := xai.GenerateCodeVerifier()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "generate verifier failed"})
			return
		}
		challenge := xai.GenerateCodeChallenge(verifier)
		nonce, err := xai.GenerateNonce()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "generate nonce failed"})
			return
		}
		sessionID, err := xai.GenerateSessionID()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "generate session failed"})
			return
		}
		grokSessions.Set(sessionID, &xai.OAuthSession{
			State:         state,
			CodeVerifier:  verifier,
			CodeChallenge: challenge,
			RedirectURI:   redirectURI,
			PoolID:        poolID,
			CreatedAt:     time.Now(),
		})
		authURL, err := xai.BuildAuthorizationURL(state, challenge, redirectURI, nonce)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"auth_url": authURL, "session_id": sessionID})

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported platform for oauth: " + platform})
	}
}

func oauthCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	sessionID := c.Query("session_id")
	platform := c.Query("platform")

	if code == "" || state == "" {
		oauthRedirectResult(c, false, "missing code or state")
		return
	}

	var poolID int
	var credJSON string
	var expiresAt int64
	var err error

	switch platform {
	case model.PoolPlatformAnthropic:
		poolID, credJSON, expiresAt, err = handleAnthropicCallback(c.Request.Context(), sessionID, state, code)
	case model.PoolPlatformOpenAI:
		poolID, credJSON, expiresAt, err = handleOpenAICallback(c.Request.Context(), sessionID, state, code)
	case model.PoolPlatformGemini:
		poolID, credJSON, expiresAt, err = handleGeminiCallback(c.Request.Context(), sessionID, state, code)
	case model.PoolPlatformGrok:
		poolID, credJSON, expiresAt, err = handleGrokCallback(c.Request.Context(), sessionID, state, code)
	default:
		oauthRedirectResult(c, false, "unsupported platform: "+platform)
		return
	}

	if err != nil {
		log.Warnf("oauth callback %s failed: %v", platform, err)
		oauthRedirectResult(c, false, err.Error())
		return
	}

	// 创建号池账号。
	acct := model.PoolAccount{
		PoolID:         poolID,
		Name:           fmt.Sprintf("%s-oauth-%d", platform, time.Now().Unix()),
		Platform:       platform,
		Type:           model.PoolTypeOAuth,
		Credentials:    pool.EncryptCredentials(credJSON),
		Status:         "active",
		Schedulable:    true,
		TokenExpiresAt: expiresAt,
	}
	if err := pool.CreateAccount(&acct); err != nil {
		oauthRedirectResult(c, false, "create account failed: "+err.Error())
		return
	}
	oauthRedirectResult(c, true, strconv.Itoa(acct.ID))
}

// handleAnthropicCallback 处理 Anthropic OAuth 回调：校验 session，code exchange。
func handleAnthropicCallback(ctx context.Context, sessionID, state, code string) (int, string, int64, error) {
	session, ok := anthropicSessions.Get(sessionID)
	if !ok {
		return 0, "", 0, fmt.Errorf("session expired")
	}
	defer anthropicSessions.Delete(sessionID)
	if session.State != state {
		return 0, "", 0, fmt.Errorf("state mismatch")
	}
	tok, err := exchangeCode(ctx, "https://console.anthropic.com/v1/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {oauth.ClientID},
		"code":          {code},
		"code_verifier": {session.CodeVerifier},
		"redirect_uri":  {session.RedirectURI},
	})
	if err != nil {
		return 0, "", 0, err
	}
	cred := model.PoolCredential{
		Type:         model.PoolTypeOAuth,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
	}
	credBytes, _ := json.Marshal(cred)
	expiresAt := time.Now().Unix() + tok.ExpiresIn
	return session.PoolID, string(credBytes), expiresAt, nil
}

func handleOpenAICallback(ctx context.Context, sessionID, state, code string) (int, string, int64, error) {
	session, ok := openaiSessions.Get(sessionID)
	if !ok {
		return 0, "", 0, fmt.Errorf("session expired")
	}
	defer openaiSessions.Delete(sessionID)
	if session.State != state {
		return 0, "", 0, fmt.Errorf("state mismatch")
	}
	tok, err := exchangeCode(ctx, "https://auth.openai.com/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {openai.ClientID},
		"code":          {code},
		"code_verifier": {session.CodeVerifier},
		"redirect_uri":  {session.RedirectURI},
	})
	if err != nil {
		return 0, "", 0, err
	}
	// 从 id_token 解析 chatgpt_account_id。
	accountID := decodeOpenAIAccountID(tok.IDToken)
	cred := model.PoolCredential{
		Type:         model.PoolTypeOAuth,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		IDToken:      tok.IDToken,
		AccountID:    accountID,
	}
	credBytes, _ := json.Marshal(cred)
	expiresAt := time.Now().Unix() + tok.ExpiresIn
	return session.PoolID, string(credBytes), expiresAt, nil
}

func handleGeminiCallback(ctx context.Context, sessionID, state, code string) (int, string, int64, error) {
	session, ok := geminiSessions.Get(sessionID)
	if !ok {
		return 0, "", 0, fmt.Errorf("session expired")
	}
	defer geminiSessions.Delete(sessionID)
	if session.State != state {
		return 0, "", 0, fmt.Errorf("state mismatch")
	}
	effective, err := geminicli.EffectiveOAuthConfig(geminicli.OAuthConfig{})
	if err != nil {
		return 0, "", 0, err
	}
	tok, err := exchangeCode(ctx, "https://oauth2.googleapis.com/token", url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {effective.ClientID},
		"client_secret": {effective.ClientSecret},
		"code":          {code},
		"code_verifier": {session.CodeVerifier},
		"redirect_uri":  {session.RedirectURI},
	})
	if err != nil {
		return 0, "", 0, err
	}
	cred := model.PoolCredential{
		Type:         model.PoolTypeOAuth,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		IDToken:      tok.IDToken,
	}
	credBytes, _ := json.Marshal(cred)
	expiresAt := time.Now().Unix() + tok.ExpiresIn
	return session.PoolID, string(credBytes), expiresAt, nil
}

func handleGrokCallback(ctx context.Context, sessionID, state, code string) (int, string, int64, error) {
	session, ok := grokSessions.Get(sessionID)
	if !ok {
		return 0, "", 0, fmt.Errorf("session expired")
	}
	defer grokSessions.Delete(sessionID)
	if session.State != state {
		return 0, "", 0, fmt.Errorf("state mismatch")
	}
	tok, err := exchangeCode(ctx, "https://auth.x.ai/oauth2/token", url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {xai.DefaultClientID},
		"code":          {code},
		"code_verifier": {session.CodeVerifier},
		"redirect_uri":  {session.RedirectURI},
	})
	if err != nil {
		return 0, "", 0, err
	}
	cred := model.PoolCredential{
		Type:         model.PoolTypeOAuth,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		IDToken:      tok.IDToken,
	}
	credBytes, _ := json.Marshal(cred)
	expiresAt := time.Now().Unix() + tok.ExpiresIn
	return session.PoolID, string(credBytes), expiresAt, nil
}

// exchangeCode 通用 OAuth code exchange。
type tokenExchangeResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	ExpiresIn    int64  `json:"expires_in"`
}

func exchangeCode(ctx context.Context, tokenURL string, form url.Values) (*tokenExchangeResponse, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("token exchange failed: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var tok tokenExchangeResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}
	return &tok, nil
}

// decodeOpenAIAccountID 从 OpenAI id_token JWT payload 解析 chatgpt_account_id。
func decodeOpenAIAccountID(idToken string) string {
	if idToken == "" {
		return ""
	}
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64URLDecode(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		OpenAIAuth struct {
			ChatGPTAccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return claims.OpenAIAuth.ChatGPTAccountID
}

// base64URLDecode 解码 base64url（补齐 padding）。
func base64URLDecode(s string) ([]byte, error) {
	s = strings.ReplaceAll(s, "-", "+")
	s = strings.ReplaceAll(s, "_", "/")
	for len(s)%4 != 0 {
		s += "="
	}
	return base64.StdEncoding.DecodeString(s)
}

// oauthRedirectResult 302 回前端 /pool?oauth=success|error。
func oauthRedirectResult(c *gin.Context, success bool, msg string) {
	base := strings.TrimRight(strings.TrimSpace(conf.AppConfig.Server.ExternalURL), "/")
	if base == "" {
		// 回退到请求 Host。
		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
		base = scheme + "://" + c.Request.Host
	}
	status := "success"
	if !success {
		status = "error"
	}
	location := fmt.Sprintf("%s/pool?oauth=%s&msg=%s", base, status, url.QueryEscape(msg))
	c.Redirect(http.StatusFound, location)
}
