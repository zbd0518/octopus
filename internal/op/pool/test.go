package pool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lingyuins/octopus/internal/helper"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/transformer/outbound"
)

// testTimeout 账号连通性测试超时。
const testTimeout = 30 * time.Second

// defaultBaseURLByPlatform 各平台默认 base_url（账号未填 base_url 时回退）。
var defaultBaseURLByPlatform = map[string]string{
	model.PoolPlatformAnthropic:  "https://api.anthropic.com",
	model.PoolPlatformOpenAI:     "https://api.openai.com",
	model.PoolPlatformGemini:     "https://generativelanguage.googleapis.com",
	model.PoolPlatformGrok:       "https://api.x.ai",
	model.PoolPlatformVolcengine: "https://ark.cn-beijing.volces.com",
}

// effectiveBaseURL 返回账号生效的 base_url：账号级优先，否则按平台默认。
func effectiveBaseURL(acct *model.PoolAccount) string {
	if acct.BaseURL != "" {
		return strings.TrimRight(acct.BaseURL, "/")
	}
	if u, ok := defaultBaseURLByPlatform[acct.Platform]; ok {
		return u
	}
	return ""
}

// TestAccount 测试号池账号连通性：向账号 base_url 发送最小 "hi" 请求。
// model 为测试用的模型名（必填）。返回状态码/延迟/错误。
func TestAccount(poolID, accountID int, modelName string) (*AccountTestResult, error) {
	acct, err := GetAccount(poolID, accountID)
	if err != nil {
		return nil, err
	}
	if modelName == "" {
		return nil, fmt.Errorf("model is required for account test")
	}
	// 解密凭据。
	_ = DecryptAccountCredentials(acct)
	cred := model.ParsePoolCredential(acct.Credentials)

	baseURL := effectiveBaseURL(acct)
	if baseURL == "" {
		return &AccountTestResult{Success: false, Error: "base_url is empty and platform has no default"}, nil
	}

	// 构造最小请求体（OpenAI chat 格式，通用性最好）。
	body := map[string]interface{}{
		"model": modelName,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
		"max_tokens": 1,
		"stream":     false,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	// 决定请求路径与鉴权头。
	reqURL, headers := buildTestRequest(acct, cred, baseURL, modelName)
	if reqURL == "" {
		return &AccountTestResult{Success: false, Error: "unsupported platform for test"}, nil
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// HTTP client（含账号级代理）。
	client := &http.Client{Timeout: testTimeout}
	if pc, perr := helper.PoolAccountHttpClient(acct.ProxyConfigID); perr == nil && pc != nil {
		client = pc
		client.Timeout = testTimeout
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return &AccountTestResult{Success: false, Latency: latency, Error: err.Error()}, nil
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	result := &AccountTestResult{Status: resp.StatusCode, Latency: latency}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Success = true
	} else {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		result.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(errBody))
	}
	return result, nil
}

// buildTestRequest 按 platform/type 构造测试请求 URL 与鉴权头。
func buildTestRequest(acct *model.PoolAccount, cred model.PoolCredential, baseURL, modelName string) (string, map[string]string) {
	headers := map[string]string{}
	platform := acct.Platform
	ctype := cred.Type

	switch {
	case platform == model.PoolPlatformAnthropic:
		// Anthropic Messages API。
		reqURL := baseURL + "/v1/messages"
		if ctype == model.PoolTypeAPIKey {
			headers["x-api-key"] = cred.EffectiveKey(platform)
		} else if ctype == model.PoolTypeOAuth {
			headers["Authorization"] = "Bearer " + cred.AccessToken
		} else if ctype == model.PoolTypeCookie {
			headers["Cookie"] = cred.Cookie
		}
		headers["anthropic-version"] = "2023-06-01"
		return reqURL, headers
	case platform == model.PoolPlatformOpenAI:
		// OpenAI Chat Completions；codex OAuth 走 account_id 头。
		reqURL := baseURL + "/v1/chat/completions"
		if ctype == model.PoolTypeOAuth {
			headers["Authorization"] = "Bearer " + cred.AccessToken
			if cred.AccountID != "" {
				headers["chatgpt-account-id"] = cred.AccountID
			}
		} else if ctype == model.PoolTypeAPIKey {
			headers["Authorization"] = "Bearer " + cred.EffectiveKey(platform)
		}
		return reqURL, headers
	case platform == model.PoolPlatformGemini:
		// Gemini generateContent（API key in query）。
		reqURL := baseURL + fmt.Sprintf("/v1beta/models/%s:generateContent?key=%s", modelName, cred.EffectiveKey(platform))
		return reqURL, headers
	case platform == model.PoolPlatformGrok:
		reqURL := baseURL + "/v1/chat/completions"
		if ctype == model.PoolTypeOAuth {
			headers["Authorization"] = "Bearer " + cred.AccessToken
		} else {
			headers["Authorization"] = "Bearer " + cred.EffectiveKey(platform)
		}
		return reqURL, headers
	case platform == model.PoolPlatformVolcengine:
		// volcengine cookie 凭据无法走标准 chat 测试（需 csrf），跳过。
		return "", nil
	default:
		// custom/upstream：按 OpenAI 兼容格式测试。
		reqURL := baseURL + "/v1/chat/completions"
		headers["Authorization"] = "Bearer " + cred.EffectiveKey(platform)
		return reqURL, headers
	}
}

// 引用 outbound 包确保 codex 类型存在（避免未使用导入，同时保留未来 codex 测试路径）。
var _ = outbound.OutboundTypeCodex
