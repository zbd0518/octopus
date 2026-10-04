package poolscheduler

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
)

// resetAuthErrorsForTest 清空全局计数器（测试隔离）。
// B1-#7 起 IncrementAuthError 首次未命中会从 DB 镜像懒加载播种，
// 因此测试需要共享 DB 先就绪。
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

// TestAuthErrorCounter_LazyLoadSeedsFromMirror：进程重启（内存清空）后，
// 首次计数从 DB 镜像继承窗口内计数（B1-#7）。
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

	// 首次未命中 → 播种 2 + 本次 1 = 3（恰好达到阈值 3）。
	count, exceeded := IncrementAuthError(poolID, accountID)
	if count != 3 || !exceeded {
		t.Fatalf("lazy-load count=%d exceeded=%v want 3/true", count, exceeded)
	}
	// 真实调用方（handlePoolAuthError）在增量后同步回写镜像。
	if err := ReportAuthErrorCount(poolID, accountID, count); err != nil {
		t.Fatalf("report mirror: %v", err)
	}

	// 模拟进程重启：清空内存后再次计数应继续继承（3+1=4）。
	globalAuthErrors.Delete(authErrorKey(poolID, accountID))
	count, _ = IncrementAuthError(poolID, accountID)
	if count != 4 {
		t.Fatalf("post-restart count=%d want 4", count)
	}
}

// TestAuthErrorCounter_LazyLoadExpiredMirrorRestarts：镜像窗口已过期 →
// 不继承，全新窗口从 1 起算。
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

// TestReportAuthErrorCount_WritesWindowMirror：镜像写入同时携带计数与窗口起点。
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

// TestPurgeStaleAuthErrors_ZeroesMirror：驱逐过期内存条目时 best-effort
// 清零 DB 镜像，避免面板残留僵尸计数（B1-#7）。
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

// TestApplyReportToDB_DelayedSuccessKeepsNewEvidence：延迟到达的成功任务
// 不得擦掉快照之后新产生的镜像证据（B1-#7 防擦除措施）。
func TestApplyReportToDB_DelayedSuccessKeepsNewEvidence(t *testing.T) {
	resetAuthErrorsForTest(t)
	poolID, _ := setupSchedulerPoolDB(t)
	accountID := addAccount(t, poolID, &model.PoolAccount{Name: "auth-evidence"})

	oldWindow := time.Now().Add(-10 * time.Minute).Unix()
	newEvidenceStart := time.Now().Unix() // ResetAuthError 之后的镜像写入携带更新的 window_start

	// 场景 1：成功后（快照 window=oldWindow）新 403 写入镜像 {1, newEvidenceStart}。
	// 延迟成功任务按快照 (1, oldWindow) 清除 → 新证据更"新"，必须保留。
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

	// 场景 2：同窗口内新 403 使计数增长（{3, oldWindow}），快照 count=1 → 保留。
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

	// 场景 3：无新证据（镜像 == 快照）→ 正常清零。
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
