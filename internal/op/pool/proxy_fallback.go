package pool

import (
	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"gorm.io/gorm"
)

// Proxy fallback (B4-#13): on a proxy-layer dial failure the account can
// switch to a per-account backup proxy (PoolAccountExtra.BackupProxyConfigID,
// Extra JSON — no extra column). Policy per the execution decision:
//   - backup-proxy-only: without a configured backup nothing happens (never
//     an automatic direct connection);
//   - the original proxy id is preserved in pool_accounts.proxy_fallback_origin_id
//     so an admin can restore it;
//   - re-entering fallback must never overwrite the recorded origin, hence the
//     "proxy_fallback_origin_id IS NULL" guard on the conditional UPDATE.

// EnterProxyFallback switches the account to its configured backup proxy and
// records the origin. It reads fresh state, so callers may pass the values
// from a stale snapshot. Returns entered=false (with a nil error) when:
//   - no backup proxy is configured (default OFF — caller logs at most),
//   - the account has no primary proxy (nothing to fail over FROM; a NULL
//     origin is indistinguishable from the "not in fallback" marker),
//   - the backup equals the current proxy,
//   - another writer already put the account into fallback (guard hit).
func EnterProxyFallback(poolID, accountID int) (bool, error) {
	acct, err := GetAccount(poolID, accountID)
	if err != nil {
		return false, err
	}
	backup := acct.GetExtra().BackupProxyConfigID
	if backup == nil || *backup <= 0 {
		return false, nil
	}
	if acct.ProxyConfigID == nil || *acct.ProxyConfigID <= 0 {
		// No primary proxy: per policy we never auto-direct, and there is no
		// origin to record, so a backup cannot be engaged meaningfully.
		return false, nil
	}
	if *acct.ProxyConfigID == *backup {
		return false, nil
	}
	origin := *acct.ProxyConfigID
	result := db.GetDB().Model(&model.PoolAccount{}).
		Where("pool_id = ? AND id = ? AND proxy_fallback_origin_id IS NULL", poolID, accountID).
		Updates(map[string]interface{}{
			"proxy_config_id":          *backup,
			"proxy_fallback_origin_id": origin,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// RestoreProxyOrigin reverts a proxy fallback in a single atomic statement:
// proxy_config_id = proxy_fallback_origin_id, marker cleared — only when the
// marker is set, so the endpoint is idempotent and a double restore (or a
// restore racing a fresh fallback) is a no-op. Returns restored=false when
// the account was not in fallback.
func RestoreProxyOrigin(poolID, accountID int) (bool, error) {
	result := db.GetDB().Model(&model.PoolAccount{}).
		Where("pool_id = ? AND id = ? AND proxy_fallback_origin_id IS NOT NULL", poolID, accountID).
		Updates(map[string]interface{}{
			"proxy_config_id":          gorm.Expr("proxy_fallback_origin_id"),
			"proxy_fallback_origin_id": nil,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}
