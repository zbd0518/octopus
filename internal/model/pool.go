package model

import (
	"encoding/json"
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
	ID               int       `json:"id" gorm:"primaryKey"`
	PoolID           int       `json:"pool_id" gorm:"index;not null"`
	Name             string    `json:"name" gorm:"size:128"`
	Credentials      string    `json:"credentials" gorm:"type:text"`
	BaseURL          string    `json:"base_url" gorm:"size:512"`
	Status           string    `json:"status" gorm:"type:varchar(32);not null;default:'active'"`
	Schedulable      bool      `json:"schedulable" gorm:"default:true"`
	Priority         int       `json:"priority" gorm:"default:0"`
	Concurrency      int       `json:"concurrency" gorm:"default:0"`
	ProxyConfigID    *int      `json:"proxy_config_id"`
	RateLimitResetAt int64     `json:"rate_limit_reset_at" gorm:"default:0"`
	OverloadUntil    int64     `json:"overload_until" gorm:"default:0"`
	TotalRequests    int64     `json:"total_requests" gorm:"default:0"`
	TotalErrors      int64     `json:"total_errors" gorm:"default:0"`
	TotalTokens      int64     `json:"total_tokens" gorm:"default:0"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (PoolAccount) TableName() string { return "pool_accounts" }

// IsSchedulable 判断账号当前是否可参与调度。
func (a *PoolAccount) IsSchedulable() bool {
	if a.Status != "active" || !a.Schedulable {
		return false
	}
	now := time.Now().Unix()
	if a.RateLimitResetAt > now || a.OverloadUntil > now {
		return false
	}
	return true
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

// PoolCredential 凭据 JSON 结构。
type PoolCredential struct {
	Type  string `json:"type"`  // "bearer" | "cookie"
	Token string `json:"token"` // bearer token 或 cookie value
}

// ParsePoolCredential 解析凭据 JSON。解析失败时返回空 PoolCredential（Token 为空）。
func ParsePoolCredential(raw string) PoolCredential {
	var cred PoolCredential
	if raw == "" {
		return cred
	}
	_ = json.Unmarshal([]byte(raw), &cred)
	return cred
}
