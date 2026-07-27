package pool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lingyuins/octopus/internal/model"
)

// QuotaResult 号池账号额度查询结果（前端展示用）。
type QuotaResult struct {
	Used    float64 `json:"used"`
	Total   float64 `json:"total"`
	ResetAt int64   `json:"reset_at"` // unix 秒，0 表示无
	Raw     string  `json:"raw,omitempty"`
}

// FetchAccountQuotaFunc 由 op/pool/quota.go 在 init 时注入（避免循环依赖）。
// nil 表示额度查询未启用。
var FetchAccountQuotaFunc func(ctx context.Context, acct *model.PoolAccount) (*QuotaResult, error)

// FetchAccountQuota 查询单个账号额度并写回 PoolAccount.Quota（加密）。
func FetchAccountQuota(ctx context.Context, poolID, accountID int) (*QuotaResult, error) {
	acct, err := GetAccount(poolID, accountID)
	if err != nil {
		return nil, err
	}
	if FetchAccountQuotaFunc == nil {
		return nil, fmt.Errorf("quota query is not initialized")
	}
	result, err := FetchAccountQuotaFunc(ctx, acct)
	if err != nil {
		// 查询失败：写 ErrorMessage，不阻断。
		_ = UpdateAccount(poolID, accountID, map[string]interface{}{
			"error_message": err.Error(),
		})
		return nil, err
	}
	// 写回 Quota（JSON，加密）。
	quotaJSON, _ := json.Marshal(result)
	if err := UpdateAccount(poolID, accountID, map[string]interface{}{
		"quota":         EncryptCredentials(string(quotaJSON)),
		"error_message": "",
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// SyncAllQuotasFunc 由 op/pool/quota.go 注入：遍历所有账号查询额度。
var SyncAllQuotasFunc func(ctx context.Context)

// SyncAllQuotas 同步所有账号额度（后台任务入口）。
func SyncAllQuotas(ctx context.Context) {
	if SyncAllQuotasFunc == nil {
		return
	}
	SyncAllQuotasFunc(ctx)
}

// RefreshAccountTokenFunc 由 pooltokenrefresh 包注入。
var RefreshAccountTokenFunc func(ctx context.Context, poolID, accountID int) error

// RefreshAccountToken 手动/调度触发单账号 token 刷新。
func RefreshAccountToken(ctx context.Context, poolID, accountID int) error {
	if RefreshAccountTokenFunc == nil {
		return fmt.Errorf("token refresh is not initialized")
	}
	return RefreshAccountTokenFunc(ctx, poolID, accountID)
}
