package pooltokenrefresh

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
	"github.com/lingyuins/octopus/internal/relay/poolscheduler"
)

// Shared test DB (on Windows TempDir handles stay open across tests, so the DB
// cannot be rebuilt per test).
var (
	refreshTestDBOnce sync.Once
	refreshTestDBErr  error
)

func ensureRefreshTestDB(t *testing.T) {
	t.Helper()
	refreshTestDBOnce.Do(func() {
		dir, err := os.MkdirTemp("", "pooltokenrefresh-test-*")
		if err != nil {
			refreshTestDBErr = err
			return
		}
		dsn := filepath.Join(dir, "refresh-test.db")
		refreshTestDBErr = db.InitDB("sqlite", dsn, false)
	})
	if refreshTestDBErr != nil {
		t.Fatalf("init shared refresh test db: %v", refreshTestDBErr)
	}
}

var refreshAccountSeq int64

// createRefreshTestAccount creates an isolated pool + OAuth account
// (credentials as plaintext JSON; when crypto is uninitialized
// EncryptCredentials passes the value through unchanged).
func createRefreshTestAccount(t *testing.T, credJSON string) (poolID, accountID int) {
	t.Helper()
	ensureRefreshTestDB(t)
	seq := atomic.AddInt64(&refreshAccountSeq, 1)
	p := &model.AccountPool{
		Name:               fmt.Sprintf("refresh-test-%d", seq),
		Strategy:           "ewma",
		DefaultConcurrency: 1,
		CooldownBaseSec:    300,
		Enabled:            true,
	}
	if err := pool.CreatePool(p); err != nil {
		t.Fatalf("create pool: %v", err)
	}
	a := &model.PoolAccount{
		PoolID:      p.ID,
		Name:        fmt.Sprintf("acct-%d", seq),
		Platform:    model.PoolPlatformOpenAI,
		Type:        model.PoolTypeOAuth,
		Credentials: credJSON,
	}
	if err := pool.CreateAccount(a); err != nil {
		t.Fatalf("create account: %v", err)
	}
	t.Cleanup(func() {
		poolscheduler.RemovePool(p.ID)
		_ = pool.DeletePool(p.ID)
	})
	return p.ID, a.ID
}

// overrideRefreshByPlatform swaps the per-platform refresh implementation and
// restores it after the test.
func overrideRefreshByPlatform(t *testing.T, fn func(ctx context.Context, platform string, cred model.PoolCredential) (model.PoolCredential, int64, error)) {
	t.Helper()
	prev := refreshByPlatformFunc
	refreshByPlatformFunc = fn
	t.Cleanup(func() { refreshByPlatformFunc = prev })
}

const testRefreshCredJSON = `{"type":"oauth","access_token":"at-old","refresh_token":"rt-old"}`

// TestRefreshInFlightBlocksScheduling: while the refresh is in flight the
// account is temporarily unschedulable (invisible to a concurrent scheduling
// scan via ListSchedulableAccounts) and becomes schedulable again within 1s
// after completion.
func TestRefreshInFlightBlocksScheduling(t *testing.T) {
	poolID, accountID := createRefreshTestAccount(t, testRefreshCredJSON)

	inFlight := make(chan struct{})
	release := make(chan struct{})
	observedBlocked := false
	overrideRefreshByPlatform(t, func(ctx context.Context, platform string, cred model.PoolCredential) (model.PoolCredential, int64, error) {
		// Refresh in flight: simulate a concurrent scheduling scan — the account
		// must be held back by the temporary unschedulable flag.
		candidates, err := pool.ListSchedulableAccounts(poolID)
		if err != nil {
			t.Errorf("list schedulable during refresh: %v", err)
		}
		for i := range candidates {
			if candidates[i].ID == accountID {
				t.Errorf("account %d should be temp-unsched while refresh is in flight", accountID)
			}
		}
		observedBlocked = true
		close(inFlight)
		<-release
		newCred := cred
		newCred.AccessToken = "at-new"
		return newCred, time.Now().Add(time.Hour).Unix(), nil
	})

	errCh := make(chan error, 1)
	go func() { errCh <- refreshAccountImpl(context.Background(), poolID, accountID) }()
	select {
	case <-inFlight:
	case <-time.After(5 * time.Second):
		t.Fatalf("refresh never reached the platform stub")
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatalf("refreshAccountImpl: %v", err)
	}
	if !observedBlocked {
		t.Fatalf("stub did not observe the in-flight window")
	}

	// Schedulable again within 1s after completion.
	deadline := time.Now().Add(time.Second)
	for {
		candidates, err := pool.ListSchedulableAccounts(poolID)
		if err != nil {
			t.Fatalf("list schedulable after refresh: %v", err)
		}
		found := false
		for i := range candidates {
			if candidates[i].ID == accountID {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("account %d not schedulable within 1s after refresh", accountID)
		}
		time.Sleep(10 * time.Millisecond)
	}

	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.IsTempUnsched() || acct.TempUnschedReason != "" {
		t.Fatalf("block should be cleared after success, until=%d reason=%q", acct.TempUnschedUntil, acct.TempUnschedReason)
	}
}

// TestRefreshFailureClearsBlock: the failure path clears the in-flight block
// too (retry pacing is next_refresh_allowed_at's job, independent of the block).
func TestRefreshFailureClearsBlock(t *testing.T) {
	poolID, accountID := createRefreshTestAccount(t, testRefreshCredJSON)

	overrideRefreshByPlatform(t, func(ctx context.Context, platform string, cred model.PoolCredential) (model.PoolCredential, int64, error) {
		return cred, 0, errors.New("upstream refresh rejected")
	})

	if err := refreshAccountImpl(context.Background(), poolID, accountID); err == nil {
		t.Fatalf("expected refresh failure error")
	}

	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.IsTempUnsched() || acct.TempUnschedReason != "" {
		t.Fatalf("block should be cleared after failure, until=%d reason=%q", acct.TempUnschedUntil, acct.TempUnschedReason)
	}
	if acct.GetExtra().RefreshFailureCount != 1 {
		t.Fatalf("failure should record backoff count=1, got %d", acct.GetExtra().RefreshFailureCount)
	}
}

// TestRefreshNoRefreshTokenLeavesNoBlock: the no-refresh_token early return
// happens before any block is set — no block left, no platform refresh run.
func TestRefreshNoRefreshTokenLeavesNoBlock(t *testing.T) {
	poolID, accountID := createRefreshTestAccount(t, `{"type":"oauth","access_token":"at-only"}`)

	called := false
	overrideRefreshByPlatform(t, func(ctx context.Context, platform string, cred model.PoolCredential) (model.PoolCredential, int64, error) {
		called = true
		return cred, 0, nil
	})

	if err := refreshAccountImpl(context.Background(), poolID, accountID); err == nil {
		t.Fatalf("expected no-refresh_token error")
	}
	if called {
		t.Fatalf("platform refresh must not run without refresh_token")
	}
	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.IsTempUnsched() || acct.TempUnschedReason != "" {
		t.Fatalf("no block should be left by the early return")
	}
}

// TestClearTempUnschedIfTrigger_OnlyClearsOwnBlock: the conditional clear never
// erases concurrent 401/403/manual blocks; a block carrying its own trigger is
// cleared.
func TestClearTempUnschedIfTrigger_OnlyClearsOwnBlock(t *testing.T) {
	ensureRefreshTestDB(t)
	seq := atomic.AddInt64(&refreshAccountSeq, 1)
	p := &model.AccountPool{Name: fmt.Sprintf("clear-trigger-%d", seq), Strategy: "ewma", CooldownBaseSec: 300, Enabled: true}
	if err := pool.CreatePool(p); err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(func() { _ = pool.DeletePool(p.ID) })
	a := &model.PoolAccount{PoolID: p.ID, Name: "acct", Platform: model.PoolPlatformOpenAI, Type: model.PoolTypeOAuth}
	if err := pool.CreateAccount(a); err != nil {
		t.Fatalf("create account: %v", err)
	}

	// A 403-shaped block (same shape as handlePoolAuthError's
	// setTempUnschedWithReason).
	blockedUntil := time.Now().Add(10 * time.Minute)
	reason403 := `{"status_code":403,"trigger":"http_403_counter","at":1759500000}`
	poolscheduler.SetTempUnsched(p.ID, a.ID, blockedUntil, reason403)

	cleared, err := poolscheduler.ClearTempUnschedIfTrigger(p.ID, a.ID, refreshUnschedTrigger)
	if err != nil {
		t.Fatalf("conditional clear: %v", err)
	}
	if cleared {
		t.Fatalf("must not clear a 403-owned block")
	}
	acct, err := pool.GetAccount(p.ID, a.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.TempUnschedReason != reason403 || !acct.IsTempUnsched() {
		t.Fatalf("403 block must survive, got reason=%q until=%d", acct.TempUnschedReason, acct.TempUnschedUntil)
	}

	// A block with our trigger -> cleared successfully.
	poolscheduler.SetTempUnsched(p.ID, a.ID, time.Now().Add(6*time.Minute), `{"trigger":"token_refresh_inflight","at":1}`)
	cleared, err = poolscheduler.ClearTempUnschedIfTrigger(p.ID, a.ID, refreshUnschedTrigger)
	if err != nil {
		t.Fatalf("conditional clear own block: %v", err)
	}
	if !cleared {
		t.Fatalf("own block should be cleared")
	}
	acct, err = pool.GetAccount(p.ID, a.ID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.IsTempUnsched() || acct.TempUnschedReason != "" {
		t.Fatalf("own block should be gone, got reason=%q until=%d", acct.TempUnschedReason, acct.TempUnschedUntil)
	}

	// Unblocked account: the conditional clear is a harmless no-op.
	if cleared, err := poolscheduler.ClearTempUnschedIfTrigger(p.ID, a.ID, refreshUnschedTrigger); err != nil || cleared {
		t.Fatalf("clear on unblocked account should be a no-op, cleared=%v err=%v", cleared, err)
	}
}

// TestRefreshAcquireDoesNotOverwriteActiveForeignBlock: when an active block
// from another source (403 cooldown shape) exists, the DB-conditional acquire
// must lose — the refresh proceeds without overwriting the block, and the
// foreign block (reason + until) survives the whole flow including cleanup.
func TestRefreshAcquireDoesNotOverwriteActiveForeignBlock(t *testing.T) {
	poolID, accountID := createRefreshTestAccount(t, testRefreshCredJSON)

	reason403 := `{"status_code":403,"trigger":"http_403_counter","at":1759500000}`
	blockedUntil := time.Now().Add(10 * time.Minute)
	poolscheduler.SetTempUnsched(poolID, accountID, blockedUntil, reason403)

	overrideRefreshByPlatform(t, func(ctx context.Context, platform string, cred model.PoolCredential) (model.PoolCredential, int64, error) {
		acct, err := pool.GetAccount(poolID, accountID)
		if err != nil {
			t.Errorf("get account during refresh: %v", err)
			return cred, 0, nil
		}
		if acct.TempUnschedReason != reason403 {
			t.Errorf("in-flight refresh must not overwrite the foreign block, got reason=%q", acct.TempUnschedReason)
		}
		newCred := cred
		newCred.AccessToken = "at-new"
		return newCred, time.Now().Add(time.Hour).Unix(), nil
	})

	if err := refreshAccountImpl(context.Background(), poolID, accountID); err != nil {
		t.Fatalf("refreshAccountImpl: %v", err)
	}

	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.TempUnschedReason != reason403 || !acct.IsTempUnsched() {
		t.Fatalf("foreign block must survive the refresh flow, got reason=%q until=%d", acct.TempUnschedReason, acct.TempUnschedUntil)
	}
}

// TestRefreshAcquiresOverExpiredForeignBlock: an expired block is scheduling
// space, so the conditional acquire takes it over (overwrites the stale
// reason) and the trigger-conditional cleanup removes it after completion.
func TestRefreshAcquiresOverExpiredForeignBlock(t *testing.T) {
	poolID, accountID := createRefreshTestAccount(t, testRefreshCredJSON)

	expiredReason := `{"status_code":401,"trigger":"oauth_401_refresh_window","at":1759500000}`
	poolscheduler.SetTempUnsched(poolID, accountID, time.Now().Add(-time.Minute), expiredReason)

	overrideRefreshByPlatform(t, func(ctx context.Context, platform string, cred model.PoolCredential) (model.PoolCredential, int64, error) {
		newCred := cred
		newCred.AccessToken = "at-new"
		return newCred, time.Now().Add(time.Hour).Unix(), nil
	})

	if err := refreshAccountImpl(context.Background(), poolID, accountID); err != nil {
		t.Fatalf("refreshAccountImpl: %v", err)
	}

	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.IsTempUnsched() || acct.TempUnschedReason != "" {
		t.Fatalf("expired block should be acquired and cleared, got reason=%q until=%d", acct.TempUnschedReason, acct.TempUnschedUntil)
	}
}
