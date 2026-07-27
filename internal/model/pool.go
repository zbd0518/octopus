package model

import (
	"encoding/json"
	"strings"
	"time"
)

// AccountPool 号池：集中管理上游账号凭据，渠道通过 PoolID 关联。
type AccountPool struct {
	ID                 int       `json:"id" gorm:"primaryKey"`
	Name               string    `json:"name" gorm:"size:128;uniqueIndex;not null"`
	Description        string    `json:"description" gorm:"size:512"`
	Strategy           string    `json:"strategy" gorm:"type:varchar(32);not null;default:'ewma'"`
	DefaultConcurrency int       `json:"default_concurrency" gorm:"default:1"`
	CooldownBaseSec    int       `json:"cooldown_base_sec" gorm:"default:300"`
	Enabled            bool      `json:"enabled" gorm:"default:true"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

func (AccountPool) TableName() string { return "account_pools" }

// PoolAccount 号池内的单个上游账号。
type PoolAccount struct {
	ID               int        `json:"id" gorm:"primaryKey"`
	PoolID           int        `json:"pool_id" gorm:"index;not null"`
	Name             string     `json:"name" gorm:"size:128"`
	Platform         string     `json:"platform" gorm:"type:varchar(32);not null;default:'custom'"`
	Type             string     `json:"type" gorm:"type:varchar(32);not null;default:'apikey'"`
	Models           string     `json:"models" gorm:"type:text"`      // 逗号分隔模型列表，空=不限
	Credentials      string     `json:"credentials" gorm:"type:text"` // 加密存储（crypto.Encrypt）
	BaseURL          string     `json:"base_url" gorm:"size:512"`
	Quota            string     `json:"quota" gorm:"type:text"` // JSON 额度快照缓存（加密存储）
	Status           string     `json:"status" gorm:"type:varchar(32);not null;default:'active'"`
	Schedulable      bool       `json:"schedulable" gorm:"default:true"`
	Priority         int        `json:"priority" gorm:"default:0"`
	Concurrency      int        `json:"concurrency" gorm:"default:0"`
	ProxyConfigID    *int       `json:"proxy_config_id"`
	RateLimitResetAt int64      `json:"rate_limit_reset_at" gorm:"default:0"`
	OverloadUntil    int64      `json:"overload_until" gorm:"default:0"`
	TokenExpiresAt   int64      `json:"token_expires_at" gorm:"default:0"` // OAuth access_token 过期 unix 秒
	TotalRequests    int64      `json:"total_requests" gorm:"default:0"`
	TotalErrors      int64      `json:"total_errors" gorm:"default:0"`
	TotalTokens      int64      `json:"total_tokens" gorm:"default:0"`
	LastUsedAt       *time.Time `json:"last_used_at"`
	ErrorMessage     string     `json:"error_message" gorm:"type:text"`
	Notes            string     `json:"notes" gorm:"size:512"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func (PoolAccount) TableName() string { return "pool_accounts" }

// 号池账号平台常量
const (
	PoolPlatformAnthropic  = "anthropic"
	PoolPlatformOpenAI     = "openai"
	PoolPlatformGemini     = "gemini"
	PoolPlatformGrok       = "grok"
	PoolPlatformVolcengine = "volcengine"
	PoolPlatformCustom     = "custom"
)

// 号池账号凭据类型常量
const (
	PoolTypeOAuth      = "oauth"
	PoolTypeAPIKey     = "apikey"
	PoolTypeCookie     = "cookie"
	PoolTypeUpstream   = "upstream"
	PoolTypeSetupToken = "setup-token"
)

// IsSchedulable 判断账号当前是否可参与调度。
func (a *PoolAccount) IsSchedulable() bool {
	if a.Status != "active" || !a.Schedulable {
		return false
	}
	now := time.Now().Unix()
	if a.RateLimitResetAt > now || a.OverloadUntil > now {
		return false
	}
	if a.IsTokenExpired() {
		return false
	}
	return true
}

// IsTokenExpired 判断 OAuth 账号的 access_token 是否即将过期（提前 60 秒视为过期）。
// 非 OAuth 类型或未记录过期时间的账号返回 false。
func (a *PoolAccount) IsTokenExpired() bool {
	if a.Type != PoolTypeOAuth {
		return false
	}
	if a.TokenExpiresAt <= 0 {
		return false
	}
	return a.TokenExpiresAt < time.Now().Unix()+60
}

// EffectiveConcurrency 返回生效的并发上限：账号级优先，0 则继承池默认。
func (a *PoolAccount) EffectiveConcurrency(poolDefault int) int {
	if a.Concurrency > 0 {
		return a.Concurrency
	}
	if poolDefault > 0 {
		return poolDefault
	}
	return 1
}

// PoolCredential 凭据 JSON 结构。按 Type 解析不同字段：
//   - apikey:   {"type":"apikey","api_key":"sk-..."}
//   - cookie:   {"type":"cookie","cookie":"sessionKey=..."}
//   - oauth:    {"type":"oauth","access_token":"...","refresh_token":"...","account_id":"...","id_token":"..."}
//   - upstream: {"type":"upstream","api_key":"...","base_url":"https://..."}
//
// 旧格式 {"type":"bearer","token":"..."} 与 {"type":"cookie","token":"..."} 向后兼容：
// Token 字段保留，bearer 归入 apikey 语义（出站按 Bearer 头发送），cookie.token 归入 cookie。
type PoolCredential struct {
	Type         string `json:"type"`
	Token        string `json:"token,omitempty"`         // 向后兼容：bearer token 或 cookie value
	APIKey       string `json:"api_key,omitempty"`       // apikey / upstream
	Cookie       string `json:"cookie,omitempty"`        // cookie
	AccessToken  string `json:"access_token,omitempty"`  // oauth
	RefreshToken string `json:"refresh_token,omitempty"` // oauth
	AccountID    string `json:"account_id,omitempty"`    // oauth (openai/codex)
	IDToken      string `json:"id_token,omitempty"`      // oauth (openai/grok)
	BaseURL      string `json:"base_url,omitempty"`      // upstream
}

// ParsePoolCredential 解析凭据 JSON。解析失败时返回空 PoolCredential。
func ParsePoolCredential(raw string) PoolCredential {
	var cred PoolCredential
	if raw == "" {
		return cred
	}
	_ = json.Unmarshal([]byte(raw), &cred)
	// 旧格式兼容：type=bearer -> 当作 apikey，token 映射到 APIKey。
	if cred.Type == "bearer" {
		if cred.APIKey == "" {
			cred.APIKey = cred.Token
		}
		cred.Type = PoolTypeAPIKey
	}
	// 旧格式 cookie：token 字段映射到 Cookie。
	if cred.Type == "cookie" && cred.Cookie == "" && cred.Token != "" {
		cred.Cookie = cred.Token
	}
	return cred
}

// EffectiveKey 返回用于出站鉴权的 ChannelKey 字符串。
// 按 Type 选取最合适的字段；oauth 的 openai/codex 返回完整 OAuth JSON（供 codex 适配器解析）。
func (c PoolCredential) EffectiveKey(platform string) string {
	switch c.Type {
	case PoolTypeAPIKey:
		if c.APIKey != "" {
			return c.APIKey
		}
		return c.Token
	case PoolTypeCookie:
		if c.Cookie != "" {
			return c.Cookie
		}
		return c.Token
	case PoolTypeUpstream:
		return c.APIKey
	case PoolTypeOAuth:
		// openai/codex 平台需要完整 OAuth JSON（含 account_id），交给 codex 适配器解析。
		if platform == PoolPlatformOpenAI {
			b, _ := json.Marshal(map[string]string{
				"access_token":  c.AccessToken,
				"account_id":    c.AccountID,
				"refresh_token": c.RefreshToken,
				"id_token":      c.IDToken,
			})
			return string(b)
		}
		// 其他平台用 access_token 作为 Bearer。
		return c.AccessToken
	}
	return c.Token
}

// ModelMatches 判断账号绑定的模型列表是否包含目标模型。
// models 为空表示不限（返回 true）；匹配采用 trim 后精确比较，不做模糊。
func ModelMatches(modelsCSV, model string) bool {
	if modelsCSV == "" {
		return true
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return true
	}
	for _, m := range strings.Split(modelsCSV, ",") {
		if strings.TrimSpace(m) == model {
			return true
		}
	}
	return false
}
