package poolscheduler

import (
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
)

func TestSuccessAfterRestartClearsPersistedAuthErrors(t *testing.T) {
	for _, age := range []time.Duration{time.Minute, authErrorWindow + time.Minute} {
		t.Run(age.String(), func(t *testing.T) {
			resetAuthErrorsForTest(t)
			now := time.Now()
			fixedAuthErrorClock(t, now)
			poolID, _ := setupSchedulerPoolDB(t)
			accountID := addAccount(t, poolID, &model.PoolAccount{Name: "restart-success"})
			if err := pool.UpdateAccount(poolID, accountID, map[string]interface{}{
				"auth_error_count": 2, "auth_error_window_start": now.Add(-age).Unix(),
			}); err != nil {
				t.Fatal(err)
			}
			count, window := snapshotAndResetAuthError(poolID, accountID)
			applyReportToDB(poolReportJob{poolID: poolID, accountID: accountID, success: true, authErrorCount: count, authErrorWindowStart: window})
			acct, err := pool.GetAccount(poolID, accountID)
			if err != nil {
				t.Fatal(err)
			}
			if acct.AuthErrorCount != 0 || acct.AuthErrorWindowStart != 0 {
				t.Fatalf("persisted evidence survived success: %d/%d", acct.AuthErrorCount, acct.AuthErrorWindowStart)
			}
			if count, exceeded := IncrementAuthError(poolID, accountID); count != 1 || exceeded {
				t.Fatalf("post-success error count = %d, exceeded = %v", count, exceeded)
			}
		})
	}
}

// fixedAuthErrorClock pins the auth-error window clock to a single instant so
// same-second and cross-second ordering cases run deterministically instead of
// depending on which side of a Unix-second boundary the test executes on.
func fixedAuthErrorClock(t *testing.T, at time.Time) {
	t.Helper()
	prev := authErrorNow
	authErrorNow = func() time.Time { return at }
	t.Cleanup(func() { authErrorNow = prev })
}

// TestBatch1ReviewDelayedSuccessPreservesSameSecondAuthError: mirror count=2
// at Unix second T; a success snapshots (2,T) and resets the in-memory counter;
// a new 403 in the SAME second mirrors (1,T) — identical window_start, so
// (count, window_start) alone cannot order the writes. The delayed success
// worker must not clear the newer evidence. The clock is pinned to a single
// second, making the case deterministic (no boundary skip).
func TestBatch1ReviewDelayedSuccessPreservesSameSecondAuthError(t *testing.T) {
	resetAuthErrorsForTest(t)
	now := time.Unix(time.Now().Unix(), 0)
	fixedAuthErrorClock(t, now)
	poolID, _ := setupSchedulerPoolDB(t)
	accountID := addAccount(t, poolID, &model.PoolAccount{Name: "same-second-auth-error"})
	globalAuthErrors.Store(authErrorKey(poolID, accountID), &authErrorEntry{count: 2, windowStart: now.Unix()})
	if err := ReportAuthErrorCount(poolID, accountID, 2); err != nil {
		t.Fatal(err)
	}
	oldCount, oldWindow := authErrorSnapshot(poolID, accountID)
	ResetAuthError(poolID, accountID)
	if oldCount != 2 || oldWindow != now.Unix() {
		t.Fatalf("snapshot drifted before reset: count=%d window=%d, want 2/%d", oldCount, oldWindow, now.Unix())
	}
	if _, resetWindow := authErrorSnapshot(poolID, accountID); resetWindow != oldWindow {
		t.Fatalf("pinned clock advanced: reset window=%d want %d", resetWindow, oldWindow)
	}
	count, _ := IncrementAuthError(poolID, accountID)
	if err := ReportAuthErrorCount(poolID, accountID, count); err != nil {
		t.Fatal(err)
	}
	applyReportToDB(poolReportJob{poolID: poolID, accountID: accountID, success: true, authErrorCount: oldCount, authErrorWindowStart: oldWindow})
	account, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.AuthErrorCount != 1 {
		t.Fatalf("new authentication evidence was cleared by a delayed success: count=%d, want 1", account.AuthErrorCount)
	}
}

// TestBatch1ReviewDelayedSuccessClearsWhenNoNewEvidence: with the clock pinned
// to the same second and no 403 after the reset, the delayed success still
// clears the mirror (the pre-existing not-newer contract must keep working).
func TestBatch1ReviewDelayedSuccessClearsWhenNoNewEvidence(t *testing.T) {
	resetAuthErrorsForTest(t)
	now := time.Unix(time.Now().Unix(), 0)
	fixedAuthErrorClock(t, now)
	poolID, _ := setupSchedulerPoolDB(t)
	accountID := addAccount(t, poolID, &model.PoolAccount{Name: "same-second-clear"})
	globalAuthErrors.Store(authErrorKey(poolID, accountID), &authErrorEntry{count: 2, windowStart: now.Unix()})
	if err := ReportAuthErrorCount(poolID, accountID, 2); err != nil {
		t.Fatal(err)
	}
	oldCount, oldWindow := authErrorSnapshot(poolID, accountID)
	ResetAuthError(poolID, accountID)
	applyReportToDB(poolReportJob{poolID: poolID, accountID: accountID, success: true, authErrorCount: oldCount, authErrorWindowStart: oldWindow})
	account, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.AuthErrorCount != 0 || account.AuthErrorWindowStart != 0 {
		t.Fatalf("stale mirror must be cleared by a delayed success, got %d/%d", account.AuthErrorCount, account.AuthErrorWindowStart)
	}
}

// TestBatch1ReviewDelayedSuccessPreservesLaterSecondEvidence: a 403 arriving
// after the success reset (clock advanced well within the window) must also
// survive the delayed success clear.
func TestBatch1ReviewDelayedSuccessPreservesLaterSecondEvidence(t *testing.T) {
	resetAuthErrorsForTest(t)
	now := time.Unix(time.Now().Unix(), 0)
	fixedAuthErrorClock(t, now)
	poolID, _ := setupSchedulerPoolDB(t)
	accountID := addAccount(t, poolID, &model.PoolAccount{Name: "later-second-auth-error"})
	globalAuthErrors.Store(authErrorKey(poolID, accountID), &authErrorEntry{count: 2, windowStart: now.Unix()})
	if err := ReportAuthErrorCount(poolID, accountID, 2); err != nil {
		t.Fatal(err)
	}
	oldCount, oldWindow := authErrorSnapshot(poolID, accountID)
	ResetAuthError(poolID, accountID)

	// Advance the pinned clock past the success moment (still inside the
	// 180-minute window) and record a fresh 403.
	fixedAuthErrorClock(t, now.Add(90*time.Second))
	count, _ := IncrementAuthError(poolID, accountID)
	if count != 1 {
		t.Fatalf("post-reset count=%d want 1", count)
	}
	if err := ReportAuthErrorCount(poolID, accountID, count); err != nil {
		t.Fatal(err)
	}

	applyReportToDB(poolReportJob{poolID: poolID, accountID: accountID, success: true, authErrorCount: oldCount, authErrorWindowStart: oldWindow})
	account, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.AuthErrorCount != 1 {
		t.Fatalf("later-second authentication evidence was cleared by a delayed success: count=%d, want 1", account.AuthErrorCount)
	}
}

// TestBatch1ReviewPurgeKeepsRenewedEntry: an entry whose window is renewed by
// a concurrent increment between the purge's unlocked read and its eviction
// must survive, and its renewed mirror evidence must not be zeroed.
func TestBatch1ReviewPurgeKeepsRenewedEntry(t *testing.T) {
	resetAuthErrorsForTest(t)
	start := time.Unix(time.Now().Unix(), 0)
	fixedAuthErrorClock(t, start)
	poolID, _ := setupSchedulerPoolDB(t)
	accountID := addAccount(t, poolID, &model.PoolAccount{Name: "purge-renew"})

	// Seed an entry whose window is already expired, with matching DB mirror.
	expiredStart := start.Unix() - int64(authErrorWindow.Seconds()) - 10
	deadline := start.Unix() - int64(authErrorWindow.Seconds())
	key := authErrorKey(poolID, accountID)
	entry := &authErrorEntry{count: 2, windowStart: expiredStart}
	globalAuthErrors.Store(key, entry)
	if err := pool.UpdateAccount(poolID, accountID, map[string]interface{}{
		"auth_error_count":        2,
		"auth_error_window_start": expiredStart,
	}); err != nil {
		t.Fatal(err)
	}

	// Renew the window under the account's evidence lock (simulates a 403
	// racing the background purge between its unlocked read and eviction):
	// the expired-window increment restarts the window and the mirror write
	// lands under the same lock.
	mu := authErrorLock(poolID, accountID)
	mu.Lock()
	if count, exceeded := incrementAuthErrorLocked(poolID, accountID); count != 1 || exceeded {
		mu.Unlock()
		t.Fatalf("renewal increment count=%d exceeded=%v, want 1/false", count, exceeded)
	}
	if err := pool.UpdateAccount(poolID, accountID, map[string]interface{}{
		"auth_error_count":        1,
		"auth_error_window_start": start.Unix(),
	}); err != nil {
		mu.Unlock()
		t.Fatal(err)
	}
	mu.Unlock()

	// Drive the eviction path with the pre-renewal deadline: the locked
	// re-check must observe the renewal and keep the entry + mirror.
	purgeAuthErrorEntry(key, entry, deadline)
	if _, ok := globalAuthErrors.Load(key); !ok {
		t.Fatalf("renewed entry must survive the purge")
	}
	account, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatal(err)
	}
	if account.AuthErrorCount != 1 || account.AuthErrorWindowStart != start.Unix() {
		t.Fatalf("renewed mirror evidence must survive the purge, got %d/%d", account.AuthErrorCount, account.AuthErrorWindowStart)
	}
}
