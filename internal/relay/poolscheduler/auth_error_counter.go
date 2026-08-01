package poolscheduler

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// auth_error_counter.go — 进程内 OpenAI 403（并复用 401-非 OAuth）错误计数器。
// 与 balancer/key_cooldown.go 同模板：sync.Map + 容量阈值窗口。
// 阈值满 3 次（180 分钟窗口）触发外部调用方 SetError；否则由调用方设置 TempUnsched 冷却。
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

func authErrorKey(poolID, accountID int) string {
	return fmt.Sprintf("%d:%d", poolID, accountID)
}

// IncrementAuthError 计数一次鉴权类错误。返回当前计数与是否超过阈值。
// 窗口从首次错误开始计时，超过 authErrorWindow 就重置计数。
func IncrementAuthError(poolID, accountID int) (count int, exceeded bool) {
	key := authErrorKey(poolID, accountID)
	val, _ := globalAuthErrors.LoadOrStore(key, &authErrorEntry{windowStart: time.Now().Unix()})
	entry := val.(*authErrorEntry)
	now := time.Now().Unix()
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
	key := authErrorKey(poolID, accountID)
	if val, ok := globalAuthErrors.Load(key); ok {
		entry := val.(*authErrorEntry)
		atomic.StoreInt64(&entry.count, 0)
		atomic.StoreInt64(&entry.windowStart, time.Now().Unix())
	}
}

// RemoveAuthError 删除账户内存条目（删除账号时清理）。
func RemoveAuthError(poolID, accountID int) {
	globalAuthErrors.Delete(authErrorKey(poolID, accountID))
}

// PurgeStaleAuthErrors 后台任务清理窗口已过期的条目（无论计数是否为 0）。
// 窗口过期即删：IncrementAuthError 在窗口外会重置 windowStart 并重新计数，
// 因此过期条目保留与否不影响正确性，直接删除可避免 globalAuthErrors 长期驻留
// （ResetAuthError 会刷新 windowStart，仅靠 count==0 判定会让活跃账号条目永不回收）。
func PurgeStaleAuthErrors() {
	now := time.Now().Unix()
	deadline := now - int64(authErrorWindow.Seconds())
	globalAuthErrors.Range(func(k, v interface{}) bool {
		entry := v.(*authErrorEntry)
		// 覆盖完窗口后仍无新增：窗口已过期，删除。
		if atomic.LoadInt64(&entry.windowStart) < deadline {
			globalAuthErrors.Delete(k)
		}
		return true
	})
}
