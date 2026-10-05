package relay

import (
	"encoding/json"
	"net/http"
	"time"

	dbmodel "github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
	"github.com/lingyuins/octopus/internal/relay/poolscheduler"
)

// poolBaseCooldown returns the pool's base cooldown duration
// (AccountPool.CooldownBaseSec, default 300s). Called once per cooldown event
// (e.g. a 429 feedback lacking reset-header evidence), not per request.
func poolBaseCooldown(poolID int) time.Duration {
	sec := 300
	if p, err := pool.GetPool(poolID); err == nil && p != nil && p.CooldownBaseSec > 0 {
		sec = p.CooldownBaseSec
	}
	return time.Duration(sec) * time.Second
}

// pool_auth_error.go — P0 号池调度健壮性（对齐 sub2api ratelimit_service.go:280-330）。
// OpenAI 403 按 180 分钟窗口计数，阈值 3 次；OAuth 401 不致直接 error，而是临时禁用 10 分钟留刷新窗口。

// tempUnschedState 透传到 DB `temp_unsched_reason` 的 JSON，可读可诊断。
type tempUnschedState struct {
	StatusCode int    `json:"status_code"`
	Trigger    string `json:"trigger"`
	At         int64  `json:"at"`
	// RuleID 非 0 表示本次临时禁用由一条 TempUnsched 规则触发（B4-#12）。
	RuleID int `json:"rule_id,omitempty"`
}

// handlePoolAuthError 根据响应 code 触发号池侧健康反馈。
// 403 → 计数 + 临时禁用（阈值满 → SetError）
// 401 oauth → 临时禁用留刷新窗口（无 refresh_token 则 SetError）
// 401 非 oauth → SetError
// 其他 code → 仅当命中 TempUnsched 规则时临时禁用（B4-#12；无命中保持既有 no-op）。
//
// Rule matching (B4-#12) runs AFTER the 403-counter logic (rules must not
// swallow the counter semantics) and BEFORE falling back to the default
// cooldown: the first enabled rule by sort_order matching the status code or
// the response-body keyword replaces the default 10-minute window. For codes
// outside 403/401 the historical behavior was a no-op — it stays a no-op
// unless a rule matches.
//
// Frozen invariant (B2-#2): this path (and everything it calls) only ever
// writes scheduling/status columns — temp_unsched_*, auth-error mirror
// counters and error state. It must NEVER write the credentials column:
// credential writes belong exclusively to the token refresh flow's CAS path
// (pool.UpdateAccountCredentialsIfUnchanged), the only writer that can prove
// it is not overwriting a newer credential.
func handlePoolAuthError(acct *dbmodel.PoolAccount, credType string, code int, bodySnippet string) {
	if acct == nil {
		return
	}
	poolID, accountID := acct.PoolID, acct.ID
	switch code {
	case http.StatusForbidden:
		if acct.Platform != dbmodel.PoolPlatformOpenAI {
			// 其他平台 403：沿用现有宽松路径（不专门计数），只临时禁用。
			applyUnschedRuleOrDefault(poolID, accountID, code, bodySnippet, "http_403")
			return
		}
		count, exceeded := poolscheduler.IncrementAuthError(poolID, accountID)
		// 同步 DB 侧窗口计数（供恢复面板查看，与计数器内存解耦）。
		_ = poolscheduler.ReportAuthErrorCount(poolID, accountID, count)
		if exceeded {
			poolscheduler.SetError(poolID, accountID)
			poolscheduler.ClearTempUnsched(poolID, accountID)
		} else {
			applyUnschedRuleOrDefault(poolID, accountID, code, bodySnippet, "http_403_counter")
		}
	case http.StatusUnauthorized:
		if credType == dbmodel.PoolTypeOAuth {
			cred := dbmodel.ParsePoolCredential(acct.Credentials)
			if cred.RefreshToken == "" {
				poolscheduler.SetError(poolID, accountID)
				return
			}
			applyUnschedRuleOrDefault(poolID, accountID, code, bodySnippet, "oauth_401_refresh_window")
			return
		}
		// 非 OAuth 401 → 直接 error（与现有行为一致）。
		poolscheduler.SetError(poolID, accountID)
	default:
		// B4-#12: rule-driven temp-unsched for other failure codes (e.g. 529).
		// No match preserves the historical no-op for these codes.
		applyUnschedRuleIfMatched(poolID, accountID, code, bodySnippet)
	}
}

// applyUnschedRuleOrDefault applies the first matching temp-unsched rule, or
// falls back to the historical default cooldown window with the given trigger
// tag.
func applyUnschedRuleOrDefault(poolID, accountID, code int, bodySnippet, fallbackTrigger string) {
	if rule, matched := pool.FirstMatchingUnschedRule(code, bodySnippet); matched {
		setRuleTempUnsched(poolID, accountID, code, rule)
		return
	}
	setTempUnschedWithReason(poolID, accountID, poolscheduler.AuthErrorCooldownDefault, code, fallbackTrigger)
}

// applyUnschedRuleIfMatched is the rules-only variant for codes whose
// historical behavior (no 403/401) is a no-op: a matching rule applies its
// duration, no match does nothing.
func applyUnschedRuleIfMatched(poolID, accountID, code int, bodySnippet string) {
	if rule, matched := pool.FirstMatchingUnschedRule(code, bodySnippet); matched {
		setRuleTempUnsched(poolID, accountID, code, rule)
	}
}

func setRuleTempUnsched(poolID, accountID, code int, rule *dbmodel.PoolUnschedRule) {
	state := tempUnschedState{
		StatusCode: code,
		Trigger:    "rule",
		RuleID:     rule.ID,
		At:         time.Now().Unix(),
	}
	b, _ := json.Marshal(state)
	poolscheduler.SetTempUnsched(poolID, accountID, time.Now().Add(time.Duration(rule.DurationMinutes)*time.Minute), string(b))
}

func setTempUnschedWithReason(poolID, accountID int, cooldown time.Duration, code int, trigger string) {
	state := tempUnschedState{
		StatusCode: code,
		Trigger:    trigger,
		At:         time.Now().Unix(),
	}
	b, _ := json.Marshal(state)
	poolscheduler.SetTempUnsched(poolID, accountID, time.Now().Add(cooldown), string(b))
}
