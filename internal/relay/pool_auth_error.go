package relay

import (
	"encoding/json"
	"net/http"
	"time"

	dbmodel "github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/relay/poolscheduler"
)

// pool_auth_error.go — P0 号池调度健壮性（对齐 sub2api ratelimit_service.go:280-330）。
// OpenAI 403 按 180 分钟窗口计数，阈值 3 次；OAuth 401 不致直接 error，而是临时禁用 10 分钟留刷新窗口。

// tempUnschedState 透传到 DB `temp_unsched_reason` 的 JSON，可读可诊断。
type tempUnschedState struct {
	StatusCode int    `json:"status_code"`
	Trigger    string `json:"trigger"`
	At         int64  `json:"at"`
}

// handlePoolAuthError 根据响应 code 触发号池侧健康反馈。
// 403 → 计数 + 临时禁用（阈值满 → SetError）
// 401 oauth → 临时禁用留刷新窗口（无 refresh_token 则 SetError）
// 401 非 oauth → SetError
func handlePoolAuthError(acct *dbmodel.PoolAccount, credType string, code int) {
	if acct == nil {
		return
	}
	poolID, accountID := acct.PoolID, acct.ID
	switch code {
	case http.StatusForbidden:
		if acct.Platform != dbmodel.PoolPlatformOpenAI {
			// 其他平台 403：沿用现有宽松路径（不专门计数），只临时禁用。
			setTempUnschedWithReason(poolID, accountID, poolscheduler.AuthErrorCooldownDefault, code, "http_403")
			return
		}
		count, exceeded := poolscheduler.IncrementAuthError(poolID, accountID)
		// 同步 DB 侧窗口计数（供恢复面板查看，与计数器内存解耦）。
		_ = poolscheduler.ReportAuthErrorCount(poolID, accountID, count)
		if exceeded {
			poolscheduler.SetError(poolID, accountID)
			poolscheduler.ClearTempUnsched(poolID, accountID)
		} else {
			setTempUnschedWithReason(poolID, accountID, poolscheduler.AuthErrorCooldownDefault, code, "http_403_counter")
		}
	case http.StatusUnauthorized:
		if credType == dbmodel.PoolTypeOAuth {
			cred := dbmodel.ParsePoolCredential(acct.Credentials)
			if cred.RefreshToken == "" {
				poolscheduler.SetError(poolID, accountID)
				return
			}
			setTempUnschedWithReason(poolID, accountID, poolscheduler.AuthErrorCooldownDefault, code, "oauth_401_refresh_window")
			return
		}
		// 非 OAuth 401 → 直接 error（与现有行为一致）。
		poolscheduler.SetError(poolID, accountID)
	}
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
