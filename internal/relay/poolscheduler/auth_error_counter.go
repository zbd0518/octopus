package poolscheduler

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lingyuins/octopus/internal/op/pool"
)

// auth_error_counter.go — 进程内 OpenAI 403（并复用 401-非 OAuth）错误计数器。
// 与 balancer/key_cooldown.go 同模板：sync.Map + 容量阈值窗口。
// 阈值满 3 次（180 分钟窗口）触发外部调用方 SetError；否则由调用方设置 TempUnsched 冷却。
//
// B1-#7 persistence (lazy load): on first cache miss the counter is seeded
// from the account's DB mirror columns (auth_error_count /
// auth_error_window_start) so the in-window count survives process restarts;
// the report and purge paths write the mirror back both ways. The mirror is
// best-effort (DB write failures do not affect in-memory counting).
//
// Concurrency model (B1-#7 review fix): every state transition of an
// account's evidence — increment, DB mirror write, success snapshot+reset,
// delayed-success clear decision, purge eviction — runs under the per-account
// evidence lock (authErrorLock). The lock closes the ordering-inversion hole
// where a delayed success report could erase 401/403 evidence created after
// its snapshot: at Unix-second resolution a reset and a same-second 403 share
// the same window_start, so (count, window_start) alone cannot order them.
// With the lock, the clear decision can safely consult the in-memory counter
// (see clearAuthErrorMirror): any post-reset increment is observed before the
// delayed clear runs, and the DB writes of both paths are serialized in
// decision order.
const (
	// authErrorWindow = sub2api openAI403CounterWindowMinutes
	authErrorWindow = 180 * time.Minute
	// authErrorThreshold = sub2api openAI403DisableThreshold
	authErrorThreshold = 3
	// AuthErrorCooldownDefault = sub2api openAI403CooldownMinutesDefault
	AuthErrorCooldownDefault = 10 * time.Minute
)

type authErrorEntry struct {
	count       int64
	windowStart int64 // unix 秒
}

// globalAuthErrors key: "poolID:accountID" -> *authErrorEntry
var globalAuthErrors sync.Map

// authErrorNow is the clock used for all window arithmetic. Production points
// at time.Now; tests pin it to make same-second / cross-second ordering cases
// deterministic (Unix-second resolution makes those cases timing-sensitive).
var authErrorNow = time.Now

// globalAuthErrorLocks key: "poolID:accountID" -> *sync.Mutex.
// Entries are never deleted: RemoveAccount may run concurrently with a worker
// holding the lock, and swapping in a fresh mutex would break mutual
// exclusion. Growth is bounded by the number of distinct (pool, account)
// pairs ever touched, which is bounded by the pool_accounts table.
var globalAuthErrorLocks sync.Map

func authErrorKey(poolID, accountID int) string {
	return fmt.Sprintf("%d:%d", poolID, accountID)
}

func authErrorLock(poolID, accountID int) *sync.Mutex {
	actual, _ := globalAuthErrorLocks.LoadOrStore(authErrorKey(poolID, accountID), &sync.Mutex{})
	return actual.(*sync.Mutex)
}

// loadAuthErrorSeed builds the in-memory entry for a first cache miss: it
// inherits the in-window count from the account's DB mirror (so 403 evidence
// survives process restarts). Returns a fresh entry (count 0, window starting
// now) when the DB is unavailable / the account does not exist / the mirror is
// invalid or its window has expired.
func loadAuthErrorSeed(poolID, accountID int) *authErrorEntry {
	fresh := func() *authErrorEntry { return &authErrorEntry{windowStart: authErrorNow().Unix()} }
	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil || acct == nil {
		return fresh()
	}
	if acct.AuthErrorCount <= 0 || acct.AuthErrorWindowStart <= 0 {
		return fresh()
	}
	if authErrorNow().Unix()-acct.AuthErrorWindowStart > int64(authErrorWindow.Seconds()) {
		return fresh()
	}
	return &authErrorEntry{count: int64(acct.AuthErrorCount), windowStart: acct.AuthErrorWindowStart}
}

// IncrementAuthError 计数一次鉴权类错误。返回当前计数与是否超过阈值。
// 窗口从首次错误开始计时，超过 authErrorWindow 就重置计数。
// On a first miss the counter is seeded from the DB mirror: LoadOrStore of a
// fully constructed entry guarantees atomicity — the losing caller reuses the
// winner's entry, so there is no double seeding or read-check-clear race.
func IncrementAuthError(poolID, accountID int) (count int, exceeded bool) {
	mu := authErrorLock(poolID, accountID)
	mu.Lock()
	defer mu.Unlock()
	return incrementAuthErrorLocked(poolID, accountID)
}

// incrementAuthErrorLocked is IncrementAuthError without the lock acquisition;
// the caller must hold the account's evidence lock.
func incrementAuthErrorLocked(poolID, accountID int) (count int, exceeded bool) {
	key := authErrorKey(poolID, accountID)
	val, loaded := globalAuthErrors.Load(key)
	if !loaded {
		val, _ = globalAuthErrors.LoadOrStore(key, loadAuthErrorSeed(poolID, accountID))
	}
	entry := val.(*authErrorEntry)
	now := authErrorNow().Unix()
	// 窗口外：重置（原子读，避免与 ResetAuthError/PurgeStaleAuthErrors 并发读到
	// 半更新的 windowStart 造成数据竞争）。
	if now-atomic.LoadInt64(&entry.windowStart) > int64(authErrorWindow.Seconds()) {
		atomic.StoreInt64(&entry.windowStart, now)
		atomic.StoreInt64(&entry.count, 1)
		return 1, false
	}
	c := atomic.AddInt64(&entry.count, 1)
	return int(c), int(c) >= authErrorThreshold
}

// ResetAuthError 请求成功后清零该账号计数。
func ResetAuthError(poolID, accountID int) {
	mu := authErrorLock(poolID, accountID)
	mu.Lock()
	defer mu.Unlock()
	resetAuthErrorLocked(poolID, accountID)
}

// resetAuthErrorLocked is ResetAuthError without the lock acquisition; the
// caller must hold the account's evidence lock.
func resetAuthErrorLocked(poolID, accountID int) {
	if val, ok := globalAuthErrors.Load(authErrorKey(poolID, accountID)); ok {
		entry := val.(*authErrorEntry)
		atomic.StoreInt64(&entry.count, 0)
		atomic.StoreInt64(&entry.windowStart, authErrorNow().Unix())
	}
}

// snapshotAndResetAuthError captures the in-memory (count, windowStart) and
// resets the counter as one critical section. ReportResult uses this so a
// concurrent 401/403 is either fully included in the snapshot (the success
// legitimately accounts it) or fully preserved after it (the delayed success
// worker must not erase it) — never half-accounted. Returns the snapshot.
func snapshotAndResetAuthError(poolID, accountID int) (count int, windowStart int64) {
	mu := authErrorLock(poolID, accountID)
	mu.Lock()
	defer mu.Unlock()
	// A successful request after restart must account for persisted evidence
	// before resetting it, just like the first post-restart auth error does.
	key := authErrorKey(poolID, accountID)
	if _, ok := globalAuthErrors.Load(key); !ok {
		entry := &authErrorEntry{windowStart: authErrorNow().Unix()}
		if acct, err := pool.GetAccount(poolID, accountID); err == nil {
			entry.count = int64(acct.AuthErrorCount)
			entry.windowStart = acct.AuthErrorWindowStart
		}
		globalAuthErrors.Store(key, entry)
	}
	count, windowStart = authErrorSnapshot(poolID, accountID)
	resetAuthErrorLocked(poolID, accountID)
	return count, windowStart
}

// authErrorSnapshot atomically reads the in-memory counter's (count,
// windowStart) snapshot; returns (0, 0) when the entry does not exist.
func authErrorSnapshot(poolID, accountID int) (int, int64) {
	val, ok := globalAuthErrors.Load(authErrorKey(poolID, accountID))
	if !ok {
		return 0, 0
	}
	entry := val.(*authErrorEntry)
	return int(atomic.LoadInt64(&entry.count)), atomic.LoadInt64(&entry.windowStart)
}

// RemoveAuthError 删除账户内存条目（删除账号时清理）。
func RemoveAuthError(poolID, accountID int) {
	globalAuthErrors.Delete(authErrorKey(poolID, accountID))
}

// parseAuthErrorKey reverse-parses "poolID:accountID" (used only for purge
// write-back; returns -1 on parse failure).
func parseAuthErrorKey(key string) (int, int) {
	poolPart, accountPart, ok := strings.Cut(key, ":")
	if !ok {
		return -1, -1
	}
	poolID, err := strconv.Atoi(poolPart)
	if err != nil {
		return -1, -1
	}
	accountID, err := strconv.Atoi(accountPart)
	if err != nil {
		return -1, -1
	}
	return poolID, accountID
}

// PurgeStaleAuthErrors 后台任务清理窗口已过期的条目（无论计数是否为 0）。
// 窗口过期即删：IncrementAuthError 在窗口外会重置 windowStart 并重新计数，
// 因此过期条目保留与否不影响正确性，直接删除可避免 globalAuthErrors 长期驻留
// （ResetAuthError 会刷新 windowStart，仅靠 count==0 判定会让活跃账号条目永不回收）。
// On eviction the DB mirror columns are zeroed best-effort so the recovery
// panel keeps no zombie count (B1-#7).
func PurgeStaleAuthErrors() {
	now := authErrorNow().Unix()
	deadline := now - int64(authErrorWindow.Seconds())
	globalAuthErrors.Range(func(k, v interface{}) bool {
		entry := v.(*authErrorEntry)
		// 覆盖完窗口后仍无新增：窗口已过期，删除。
		if atomic.LoadInt64(&entry.windowStart) < deadline {
			purgeAuthErrorEntry(k, v, deadline)
		}
		return true
	})
}

// purgeAuthErrorEntry re-checks the window under the account's evidence lock
// before evicting: the entry can be renewed in place by a concurrent increment
// (window reset) between the unlocked read in PurgeStaleAuthErrors and this
// point, and a CompareAndDelete on the stored pointer alone does not prove the
// window is still expired. Holding the lock also orders the mirror zeroing
// against a concurrent post-renewal mirror write, which must survive the purge.
func purgeAuthErrorEntry(k, v interface{}, deadline int64) {
	entry := v.(*authErrorEntry)
	poolID, accountID := parseAuthErrorKey(k.(string))
	if poolID < 0 {
		// Unparseable key: evict without a DB mirror write.
		globalAuthErrors.CompareAndDelete(k, v)
		return
	}
	mu := authErrorLock(poolID, accountID)
	mu.Lock()
	defer mu.Unlock()
	if atomic.LoadInt64(&entry.windowStart) >= deadline {
		// Renewed concurrently; keep the entry and its mirror evidence.
		return
	}
	// CompareAndDelete：仅在条目未被并发重建时驱逐，避免误删新条目。
	if globalAuthErrors.CompareAndDelete(k, v) {
		_ = pool.UpdateAccount(poolID, accountID, map[string]interface{}{
			"auth_error_count":        0,
			"auth_error_window_start": int64(0),
		})
	}
}
