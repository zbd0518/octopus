// Package pooltokenrefresh 实现号池 OAuth 账号的 access_token 自动刷新服务。
//
// 设计：
//   - RefreshAccount 单账号刷新，singleflight 合并并发调用，按 platform 路由到
//     per-platform refresher（移植 sub2api token_refresher.go 逻辑，用 octopus
//     http client + internal/pkg OAuth 常量）。
//   - RefreshLoop 后台扫描所有即将过期的 oauth 账号，并发刷新（semaphore 限 4）。
//   - 通过 init 注入 poolscheduler.TriggerRefreshAsync 与 pool.RefreshAccountTokenFunc。
package pooltokenrefresh

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
	"github.com/lingyuins/octopus/internal/relay/poolscheduler"
	"github.com/lingyuins/octopus/internal/utils/log"
	"golang.org/x/sync/singleflight"
)

var (
	sf singleflight.Group
	// refreshSem 限制后台刷新并发。
	refreshSem = make(chan struct{}, 4)
)

const (
	refreshHTTPTimeout = 20 * time.Second
	// 即将过期阈值：expires_at - now < 5min 视为需刷新。
	refreshLeadTime = 5 * time.Minute
)

func init() {
	// 注入选号触发刷新 + 手动刷新入口。
	poolscheduler.TriggerRefreshAsync = func(poolID, accountID int) {
		go func() {
			if err := RefreshAccount(context.Background(), poolID, accountID); err != nil {
				log.Warnf("pooltokenrefresh: trigger refresh account %d/%d failed: %v", poolID, accountID, err)
			}
		}()
	}
	pool.RefreshAccountTokenFunc = RefreshAccount
}

// RefreshAccount 刷新单个 OAuth 账号的 access_token。
// singleflight 按 accountID 合并并发刷新。刷新成功写回 credentials + token_expires_at，
// 清空 error_message；失败写 error_message。
func RefreshAccount(ctx context.Context, poolID, accountID int) error {
	key := fmt.Sprintf("%d:%d", poolID, accountID)
	_, err, _ := sf.Do(key, func() (interface{}, error) {
		return nil, refreshAccountImpl(ctx, poolID, accountID)
	})
	return err
}

func refreshAccountImpl(ctx context.Context, poolID, accountID int) error {
	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		return err
	}
	if acct.Type != model.PoolTypeOAuth {
		return fmt.Errorf("account %d is not oauth type", accountID)
	}
	// 解密凭据。
	_ = pool.DecryptAccountCredentials(acct)
	cred := model.ParsePoolCredential(acct.Credentials)
	if cred.RefreshToken == "" {
		return fmt.Errorf("account %d has no refresh_token", accountID)
	}

	newCred, expiresAt, err := refreshByPlatform(ctx, acct.Platform, cred)
	if err != nil {
		// 写 error_message。
		_ = pool.UpdateAccount(poolID, accountID, map[string]interface{}{
			"error_message": err.Error(),
		})
		return err
	}

	// 构造新凭据 JSON 并加密。
	credBytes, _ := json.Marshal(newCred)
	updates := map[string]interface{}{
		"credentials":      pool.EncryptCredentials(string(credBytes)),
		"token_expires_at": expiresAt,
		"error_message":    "",
	}
	return pool.UpdateAccount(poolID, accountID, updates)
}

// refreshByPlatform 按 platform 路由到对应刷新逻辑，返回新凭据与过期时间戳。
func refreshByPlatform(ctx context.Context, platform string, cred model.PoolCredential) (model.PoolCredential, int64, error) {
	client := &http.Client{Timeout: refreshHTTPTimeout}

	switch platform {
	case model.PoolPlatformAnthropic:
		return refreshAnthropic(ctx, client, cred)
	case model.PoolPlatformOpenAI:
		return refreshOpenAI(ctx, client, cred)
	case model.PoolPlatformGemini:
		return refreshGemini(ctx, client, cred)
	case model.PoolPlatformGrok:
		return refreshGrok(ctx, client, cred)
	default:
		return cred, 0, fmt.Errorf("unsupported oauth platform for refresh: %s", platform)
	}
}

// tokenRefreshResult 通用刷新响应。
type tokenRefreshResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int64  `json:"expires_in"`
	IDToken      string `json:"id_token,omitempty"`
}

func doRefresh(ctx context.Context, client *http.Client, tokenURL string, form url.Values) (*tokenRefreshResult, error) {
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
		return nil, fmt.Errorf("token refresh failed: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var result tokenRefreshResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	if result.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}
	return &result, nil
}

func refreshAnthropic(ctx context.Context, client *http.Client, cred model.PoolCredential) (model.PoolCredential, int64, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {"9d1c250a-e61b-44d9-88ed-5944d1962f5e"},
		"refresh_token": {cred.RefreshToken},
	}
	// Anthropic OAuth: https://console.anthropic.com/v1/oauth/token
	r, err := doRefresh(ctx, client, "https://console.anthropic.com/v1/oauth/token", form)
	if err != nil {
		return cred, 0, err
	}
	newCred := cred
	newCred.AccessToken = r.AccessToken
	if r.RefreshToken != "" {
		newCred.RefreshToken = r.RefreshToken
	}
	expiresAt := time.Now().Unix() + r.ExpiresIn
	return newCred, expiresAt, nil
}

func refreshOpenAI(ctx context.Context, client *http.Client, cred model.PoolCredential) (model.PoolCredential, int64, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {"app_EMoamEEZ73f0CkXaXp7hrann"},
		"refresh_token": {cred.RefreshToken},
		"scope":         {"openid profile email"},
	}
	r, err := doRefresh(ctx, client, "https://auth.openai.com/oauth/token", form)
	if err != nil {
		return cred, 0, err
	}
	newCred := cred
	newCred.AccessToken = r.AccessToken
	if r.RefreshToken != "" {
		newCred.RefreshToken = r.RefreshToken
	}
	if r.IDToken != "" {
		newCred.IDToken = r.IDToken
	}
	expiresAt := time.Now().Unix() + r.ExpiresIn
	return newCred, expiresAt, nil
}

func refreshGemini(ctx context.Context, client *http.Client, cred model.PoolCredential) (model.PoolCredential, int64, error) {
	clientID := "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
	clientSecret := strings.TrimSpace(getEnvDefault("GEMINI_CLI_OAUTH_CLIENT_SECRET", ""))
	if clientSecret == "" {
		return cred, 0, fmt.Errorf("gemini refresh requires GEMINI_CLI_OAUTH_CLIENT_SECRET env")
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"refresh_token": {cred.RefreshToken},
	}
	r, err := doRefresh(ctx, client, "https://oauth2.googleapis.com/token", form)
	if err != nil {
		return cred, 0, err
	}
	newCred := cred
	newCred.AccessToken = r.AccessToken
	if r.RefreshToken != "" {
		newCred.RefreshToken = r.RefreshToken
	}
	if r.IDToken != "" {
		newCred.IDToken = r.IDToken
	}
	expiresAt := time.Now().Unix() + r.ExpiresIn
	return newCred, expiresAt, nil
}

func refreshGrok(ctx context.Context, client *http.Client, cred model.PoolCredential) (model.PoolCredential, int64, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {"b1a00492-073a-47ea-816f-4c329264a828"},
		"refresh_token": {cred.RefreshToken},
	}
	r, err := doRefresh(ctx, client, "https://auth.x.ai/oauth2/token", form)
	if err != nil {
		return cred, 0, err
	}
	newCred := cred
	newCred.AccessToken = r.AccessToken
	if r.RefreshToken != "" {
		newCred.RefreshToken = r.RefreshToken
	}
	if r.IDToken != "" {
		newCred.IDToken = r.IDToken
	}
	expiresAt := time.Now().Unix() + r.ExpiresIn
	return newCred, expiresAt, nil
}

// RefreshLoop 后台扫描所有即将过期的 oauth 账号，并发刷新。
// 由 task 包周期调用。
func RefreshLoop() {
	ctx := context.Background()
	accounts, err := pool.ListAllAccounts()
	if err != nil {
		log.Warnf("pooltokenrefresh: list accounts failed: %v", err)
		return
	}
	now := time.Now().Unix()
	var wg sync.WaitGroup
	for i := range accounts {
		acct := &accounts[i]
		if acct.Type != model.PoolTypeOAuth {
			continue
		}
		// 即将过期或已过期才刷新（expires_at==0 也刷新，避免遗漏未记录过期的）。
		if acct.TokenExpiresAt != 0 && acct.TokenExpiresAt-now > int64(refreshLeadTime.Seconds()) {
			continue
		}
		wg.Add(1)
		refreshSem <- struct{}{}
		go func(p *model.PoolAccount) {
			defer wg.Done()
			defer func() { <-refreshSem }()
			if err := RefreshAccount(ctx, p.PoolID, p.ID); err != nil {
				log.Warnf("pooltokenrefresh: refresh account %d/%d failed: %v", p.PoolID, p.ID, err)
			}
		}(acct)
	}
	wg.Wait()
}

func getEnvDefault(key, fallback string) string {
	if v := strings.TrimSpace(envGetter(key)); v != "" {
		return v
	}
	return fallback
}

// envGetter 读取环境变量（os.Getenv 封装，便于测试覆盖）。
var envGetter = os.Getenv
