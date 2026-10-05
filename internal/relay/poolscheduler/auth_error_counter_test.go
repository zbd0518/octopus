package poolscheduler

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
)

// resetAuthErrorsForTest clears the global counter (test isolation).
// Since B1-#7, IncrementAuthError lazy-seeds from the DB mirror on first miss,
// so tests need the shared DB to be ready first.
func resetAuthErrorsForTest(t *testing.T) {
	t.Helper()
	ensureSchedulerSharedDB(t)
	globalAuthErrors.Range(func(k, _ interface{}) bool {
		globalAuthErrors.Delete(k)
		return true
	})
	t.Cleanup(func() {
		globalAuthErrors.Range(func(k, _ interface{}) bool {
			globalAuthErrors.Delete(k)
			return true
		})
	})
}

func TestAuthErrorCounter_ThresholdExceeded(t *testing.T) {
	resetAuthErrorsForTest(t)
	poolID, accountID := 1, 42

	var exceeded bool
	for i := 1; i <= 2; i++ {
		count, e := IncrementAuthError(poolID, accountID)
		if e {
			t.Fatalf("attempt %d unexpected exceeded", i)
		}
		if count != i {
			t.Fatalf("attempt %d count=%d want %d", i, count, i)
		}
	}
	_, exceeded = IncrementAuthError(poolID, accountID)
	if !exceeded {
		t.Fatalf("3rd attempt should exceed threshold")
	}
}

func TestAuthErrorCounter_WindowResets(t *testing.T) {
	resetAuthErrorsForTest(t)
	poolID, accountID := 1, 7

	// 手动构造过期窗口：写入一个 windowStart 远超 180 分钟前。
	key := authErrorKey(poolID, accountID)
	globalAuthErrors.Store(key, &authErrorEntry{
		count:       2,
		windowStart: time.Now().Unix() - int64(authErrorWindow.Seconds()) - 10,
	})

	count, exceeded := IncrementAuthError(poolID, accountID)
	if exceeded {
		t.Fatalf("expired window should reset, not exceed")
	}
	if count != 1 {
		t.Fatalf("expired window count=%d want 1", count)
	}
}

func TestAuthErrorCounter_ResetClears(t *testing.T) {
	resetAuthErrorsForTest(t)
	poolID, accountID := 1, 99

	for range 3 {
		IncrementAuthError(poolID, accountID)
	}
	ResetAuthError(poolID, accountID)

	count, exceeded := IncrementAuthError(poolID, accountID)
	if exceeded {
		t.Fatalf("post-reset first failure should not exceed")
	}
	if count != 1 {
		t.Fatalf("post-reset count=%d want 1", count)
	}
}

func TestAuthErrorCounter_RemoveAndPurge(t *testing.T) {
	resetAuthErrorsForTest(t)
	poolID, accountID := 1, 300

	IncrementAuthError(poolID, accountID)
	RemoveAuthError(poolID, accountID)
	if _, ok := globalAuthErrors.Load(authErrorKey(poolID, accountID)); ok {
		t.Fatalf("RemoveAuthError should delete entry")
	}

	// 添一条窗口过期的记录（含 count），应被 Purge 清理——窗口过期即删，
	// 因 IncrementAuthError 在窗口外会重置 windowStart 并重新计数。
	key := authErrorKey(poolID, accountID)
	globalAuthErrors.Store(key, &authErrorEntry{
		count:       2,
		windowStart: time.Now().Unix() - int64(authErrorWindow.Seconds()) - 10,
	})
	PurgeStaleAuthErrors()
	if _, ok := globalAuthErrors.Load(key); ok {
		t.Fatalf("PurgeStale should remove window-expired entry (regardless of count)")
	}

	// 窗口未过期的记录（无论计数）应被保留。
	globalAuthErrors.Store(key, &authErrorEntry{
		count:       1,
		windowStart: time.Now().Unix(),
	})
	PurgeStaleAuthErrors()
	if _, ok := globalAuthErrors.Load(key); !ok {
		t.Fatalf("PurgeStale should keep non-expired entry")
	}
}

func TestAuthErrorCounter_Concurrent(t *testing.T) {
	resetAuthErrorsForTest(t)
	poolID, accountID := 2, 500

	const N = 30
	done := make(chan struct{}, N)
	for range N {
		go func() {
			IncrementAuthError(poolID, accountID)
			done <- struct{}{}
		}()
	}
	for range N {
		<-done
	}
	val, _ := globalAuthErrors.Load(authErrorKey(poolID, accountID))
	entry := val.(*authErrorEntry)
	if c := atomic.LoadInt64(&entry.count); c != N {
		t.Fatalf("concurrent count=%d want %d", c, N)
	}
}

// TestAuthErrorCounter_LazyLoadSeedsFromMirror: after a process restart
// (memory cleared), the first count inherits the in-window count from the DB
// mirror (B1-#7).
func TestAuthErrorCounter_LazyLoadSeedsFromMirror(t *testing.T) {
	resetAuthErrorsForTest(t)
	poolID, _ := setupSchedulerPoolDB(t)
	a := addAccount(t, poolID, &model.PoolAccount{Name: "auth-mirror"})
	accountID := a

	now := time.Now().Unix()
	if err := pool.UpdateAccount(poolID, accountID, map[string]interface{}{
		"auth_error_count":        2,
		"auth_error_window_start": now - 60,
	}); err != nil {
		t.Fatalf("seed mirror: %v", err)
	}

	// First miss -> seeded 2 + this one 1 = 3 (exactly the threshold).
	count, exceeded := IncrementAuthError(poolID, accountID)
	if count != 3 || !exceeded {
		t.Fatalf("lazy-load count=%d exceeded=%v want 3/true", count, exceeded)
	}
	// The real caller (handlePoolAuthError) mirrors back synchronously after the increment.
	if err := ReportAuthErrorCount(poolID, accountID, count); err != nil {
		t.Fatalf("report mirror: %v", err)
	}

	// Simulate a process restart: after clearing memory, counting again should keep
	// inheriting (3+1=4).
	globalAuthErrors.Delete(authErrorKey(poolID, accountID))
	count, _ = IncrementAuthError(poolID, accountID)
	if count != 4 {
		t.Fatalf("post-restart count=%d want 4", count)
	}
}

// TestAuthErrorCounter_LazyLoadExpiredMirrorRestarts: an expired mirror window
// is not inherited; a fresh window starts from 1.
func TestAuthErrorCounter_LazyLoadExpiredMirrorRestarts(t *testing.T) {
	resetAuthErrorsForTest(t)
	poolID, _ := setupSchedulerPoolDB(t)
	accountID := addAccount(t, poolID, &model.PoolAccount{Name: "auth-mirror-expired"})

	now := time.Now().Unix()
	if err := pool.UpdateAccount(poolID, accountID, map[string]interface{}{
		"auth_error_count":        2,
		"auth_error_window_start": now - int64(authErrorWindow.Seconds()) - 10,
	}); err != nil {
		t.Fatalf("seed expired mirror: %v", err)
	}

	count, exceeded := IncrementAuthError(poolID, accountID)
	if count != 1 || exceeded {
		t.Fatalf("expired mirror count=%d exceeded=%v want 1/false", count, exceeded)
	}
}

// TestReportAuthErrorCount_WritesWindowMirror: the mirror write carries both the
// count and the window start.
func TestReportAuthErrorCount_WritesWindowMirror(t *testing.T) {
	resetAuthErrorsForTest(t)
	poolID, _ := setupSchedulerPoolDB(t)
	accountID := addAccount(t, poolID, &model.PoolAccount{Name: "auth-report"})

	windowStart := time.Now().Add(-time.Minute).Unix()
	globalAuthErrors.Store(authErrorKey(poolID, accountID), &authErrorEntry{count: 2, windowStart: windowStart})

	if err := ReportAuthErrorCount(poolID, accountID, 2); err != nil {
		t.Fatalf("report: %v", err)
	}
	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.AuthErrorCount != 2 || acct.AuthErrorWindowStart != windowStart {
		t.Fatalf("mirror=%d/%d want 2/%d", acct.AuthErrorCount, acct.AuthErrorWindowStart, windowStart)
	}
}

// TestPurgeStaleAuthErrors_ZeroesMirror: evicting an expired in-memory entry
// zeroes the DB mirror best-effort so the panel keeps no zombie count (B1-#7).
func TestPurgeStaleAuthErrors_ZeroesMirror(t *testing.T) {
	resetAuthErrorsForTest(t)
	poolID, _ := setupSchedulerPoolDB(t)
	accountID := addAccount(t, poolID, &model.PoolAccount{Name: "auth-purge"})

	expiredStart := time.Now().Unix() - int64(authErrorWindow.Seconds()) - 10
	key := authErrorKey(poolID, accountID)
	globalAuthErrors.Store(key, &authErrorEntry{count: 2, windowStart: expiredStart})
	if err := pool.UpdateAccount(poolID, accountID, map[string]interface{}{
		"auth_error_count":        2,
		"auth_error_window_start": expiredStart,
	}); err != nil {
		t.Fatalf("seed mirror: %v", err)
	}

	PurgeStaleAuthErrors()
	if _, ok := globalAuthErrors.Load(key); ok {
		t.Fatalf("expired entry should be purged")
	}
	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.AuthErrorCount != 0 || acct.AuthErrorWindowStart != 0 {
		t.Fatalf("mirror should be zeroed on purge, got %d/%d", acct.AuthErrorCount, acct.AuthErrorWindowStart)
	}
}

// TestApplyReportToDB_DelayedSuccessKeepsNewEvidence: a delayed success job must
// not erase mirror evidence produced after its snapshot (B1-#7 anti-erasure).
func TestApplyReportToDB_DelayedSuccessKeepsNewEvidence(t *testing.T) {
	resetAuthErrorsForTest(t)
	poolID, _ := setupSchedulerPoolDB(t)
	accountID := addAccount(t, poolID, &model.PoolAccount{Name: "auth-evidence"})

	oldWindow := time.Now().Add(-10 * time.Minute).Unix()
	newEvidenceStart := time.Now().Unix() // mirror writes after ResetAuthError carry the updated window_start

	// Scenario 1: after the success (snapshot window=oldWindow) a new 403 mirrors
	// {1, newEvidenceStart}. The delayed success job clears per snapshot (1,
	// oldWindow) -> the newer evidence must survive.
	if err := pool.UpdateAccount(poolID, accountID, map[string]interface{}{
		"auth_error_count":        1,
		"auth_error_window_start": newEvidenceStart,
	}); err != nil {
		t.Fatalf("seed new evidence: %v", err)
	}
	applyReportToDB(poolReportJob{poolID: poolID, accountID: accountID, success: true, authErrorCount: 1, authErrorWindowStart: oldWindow})
	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.AuthErrorCount != 1 || acct.AuthErrorWindowStart != newEvidenceStart {
		t.Fatalf("newer evidence must survive delayed success, got %d/%d", acct.AuthErrorCount, acct.AuthErrorWindowStart)
	}

	// Scenario 2: a new 403 in the same window grows the count ({3, oldWindow});
	// snapshot count=1 -> keep.
	if err := pool.UpdateAccount(poolID, accountID, map[string]interface{}{
		"auth_error_count":        3,
		"auth_error_window_start": oldWindow,
	}); err != nil {
		t.Fatalf("seed grown evidence: %v", err)
	}
	applyReportToDB(poolReportJob{poolID: poolID, accountID: accountID, success: true, authErrorCount: 1, authErrorWindowStart: oldWindow})
	acct, err = pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.AuthErrorCount != 3 {
		t.Fatalf("grown evidence must survive, got %d", acct.AuthErrorCount)
	}

	// Scenario 3: no new evidence (mirror == snapshot) -> cleared normally.
	if err := pool.UpdateAccount(poolID, accountID, map[string]interface{}{
		"auth_error_count":        1,
		"auth_error_window_start": oldWindow,
	}); err != nil {
		t.Fatalf("seed stale evidence: %v", err)
	}
	applyReportToDB(poolReportJob{poolID: poolID, accountID: accountID, success: true, authErrorCount: 1, authErrorWindowStart: oldWindow})
	acct, err = pool.GetAccount(poolID, accountID)
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	if acct.AuthErrorCount != 0 || acct.AuthErrorWindowStart != 0 {
		t.Fatalf("stale evidence should be cleared, got %d/%d", acct.AuthErrorCount, acct.AuthErrorWindowStart)
	}
}
