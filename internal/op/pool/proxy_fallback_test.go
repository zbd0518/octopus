package pool

import (
	"fmt"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/model"
)

func backupProxyOf(t *testing.T, poolID, accountID int) *model.PoolAccount {
	t.Helper()
	acct, err := GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	return acct
}

func createProxyFallbackAccount(t *testing.T, primaryProxy, backupProxy *int) (int, int) {
	t.Helper()
	setupPoolTestDB(t)
	p := &model.AccountPool{Name: fmt.Sprintf("proxy-fallback-pool-%d", time.Now().UnixNano()), Enabled: true}
	if err := CreatePool(p); err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(func() { _ = DeletePool(p.ID) })
	acct := &model.PoolAccount{
		PoolID:        p.ID,
		Name:          "proxy-fallback-account",
		Platform:      model.PoolPlatformOpenAI,
		Type:          model.PoolTypeAPIKey,
		Credentials:   `{"type":"apikey","api_key":"sk-test"}`,
		ProxyConfigID: primaryProxy,
	}
	acct.SetExtra(model.PoolAccountExtra{BackupProxyConfigID: backupProxy})
	if err := CreateAccount(acct); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return p.ID, acct.ID
}

// TestEnterProxyFallbackSwitchesToBackupAndRecordsOrigin covers the main path:
// a proxy account with a backup configured switches to the backup and keeps
// the original id in proxy_fallback_origin_id.
func TestEnterProxyFallbackSwitchesToBackupAndRecordsOrigin(t *testing.T) {
	primary, backup := 11, 22
	poolID, accountID := createProxyFallbackAccount(t, &primary, &backup)

	entered, err := EnterProxyFallback(poolID, accountID)
	if err != nil || !entered {
		t.Fatalf("fallback must engage: entered=%v err=%v", entered, err)
	}
	acct := backupProxyOf(t, poolID, accountID)
	if acct.ProxyConfigID == nil || *acct.ProxyConfigID != backup {
		t.Fatalf("account must run on the backup proxy, got %v", acct.ProxyConfigID)
	}
	if acct.ProxyFallbackOriginID == nil || *acct.ProxyFallbackOriginID != primary {
		t.Fatalf("origin must be recorded, got %v", acct.ProxyFallbackOriginID)
	}

	// Restore puts the original proxy back and clears the marker atomically.
	restored, err := RestoreProxyOrigin(poolID, accountID)
	if err != nil || !restored {
		t.Fatalf("restore must apply: restored=%v err=%v", restored, err)
	}
	acct = backupProxyOf(t, poolID, accountID)
	if acct.ProxyConfigID == nil || *acct.ProxyConfigID != primary {
		t.Fatalf("restore must reinstate the original proxy, got %v", acct.ProxyConfigID)
	}
	if acct.ProxyFallbackOriginID != nil {
		t.Fatalf("restore must clear the fallback marker, got %v", acct.ProxyFallbackOriginID)
	}

	// Idempotency: a second restore is a no-op success.
	restored, err = RestoreProxyOrigin(poolID, accountID)
	if err != nil || restored {
		t.Fatalf("second restore must be a no-op: restored=%v err=%v", restored, err)
	}
}

// TestEnterProxyFallbackGuardsOriginAgainstConcurrentFallback: the second
// failure while already in fallback must not overwrite the recorded origin.
func TestEnterProxyFallbackGuardsOriginAgainstConcurrentFallback(t *testing.T) {
	primary, backup := 31, 32
	poolID, accountID := createProxyFallbackAccount(t, &primary, &backup)

	if entered, _ := EnterProxyFallback(poolID, accountID); !entered {
		t.Fatalf("first fallback must engage")
	}
	// Simulate the account being switched again by an admin meanwhile.
	other := 99
	if err := UpdateAccount(poolID, accountID, map[string]interface{}{"proxy_config_id": other}); err != nil {
		t.Fatalf("admin switch: %v", err)
	}

	entered, err := EnterProxyFallback(poolID, accountID)
	if err != nil {
		t.Fatalf("second fallback errored: %v", err)
	}
	if entered {
		t.Fatalf("second fallback must not engage while the marker is set (guard)")
	}
	acct := backupProxyOf(t, poolID, accountID)
	if acct.ProxyConfigID == nil || *acct.ProxyConfigID != other {
		t.Fatalf("guard must keep the current proxy untouched, got %v", acct.ProxyConfigID)
	}
	if acct.ProxyFallbackOriginID == nil || *acct.ProxyFallbackOriginID != primary {
		t.Fatalf("origin must survive the guarded no-op, got %v", acct.ProxyFallbackOriginID)
	}
}

// TestEnterProxyFallbackPolicyNoops locks in the OFF-by-default policy:
// without a configured backup nothing happens, and an account without a
// primary proxy never enters fallback (origin would be indistinguishable
// from the "not in fallback" marker; direct connections are never automatic).
func TestEnterProxyFallbackPolicyNoops(t *testing.T) {
	// No backup configured at all.
	primary := 41
	poolID, accountID := createProxyFallbackAccount(t, &primary, nil)
	entered, err := EnterProxyFallback(poolID, accountID)
	if err != nil || entered {
		t.Fatalf("no backup configured must be a no-op: entered=%v err=%v", entered, err)
	}

	// Backup configured but the account is direct (no primary proxy).
	backup := 42
	poolID2, accountID2 := createProxyFallbackAccount(t, nil, &backup)
	entered, err = EnterProxyFallback(poolID2, accountID2)
	if err != nil || entered {
		t.Fatalf("direct account must not enter fallback: entered=%v err=%v", entered, err)
	}
	acct := backupProxyOf(t, poolID2, accountID2)
	if acct.ProxyConfigID != nil || acct.ProxyFallbackOriginID != nil {
		t.Fatalf("direct account must stay direct with no marker, got proxy=%v origin=%v", acct.ProxyConfigID, acct.ProxyFallbackOriginID)
	}

	// Backup equals the primary: nothing to do.
	same := 43
	poolID3, accountID3 := createProxyFallbackAccount(t, &same, &same)
	entered, err = EnterProxyFallback(poolID3, accountID3)
	if err != nil || entered {
		t.Fatalf("backup equal to primary must be a no-op: entered=%v err=%v", entered, err)
	}
}
