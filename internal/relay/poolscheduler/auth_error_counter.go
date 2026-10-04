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
// B1-#7 持久化（lazy load）：计数器首次未命中时从账号 DB 镜像列
//（auth_error_count / auth_error_window_start）播种，进程重启后继承窗口内计数；
// 上报与清理路径双向回写镜像。镜像为 best-effort（DB 写失败不影响内存计数）。
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

// loadAuthErrorSeed 构造首次未命中时的内存条目：从账号 DB 镜像继承窗口内计数
//（进程重启后 403 证据不丢）。DB 不可用 / 账号不存在 / 镜像无效或窗口已过期时
// 返回全新条目（0 计数、窗口从现在起算）。
func loadAuthErrorSeed(poolID, accountID int) *authErrorEntry {
	fresh := func() *authErrorEntry { return &authErrorEntry{windowStart: time.Now().Unix()} }
	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil || acct == nil {
		return fresh()
	}
	if acct.AuthErrorCount <= 0 || acct.AuthErrorWindowStart <= 0 {
		return fresh()
	}
	if time.Now().Unix()-acct.AuthErrorWindowStart > int64(authErrorWindow.Seconds()) {
		return fresh()
	}
	return &authErrorEntry{count: int64(acct.AuthErrorCount), windowStart: acct.AuthErrorWindowStart}
}

// IncrementAuthError 计数一次鉴权类错误。返回当前计数与是否超过阈值。
// 窗口从首次错误开始计时，超过 authErrorWindow 就重置计数。
// 首次未命中时从 DB 镜像播种：LoadOrStore 一个完整构造的条目保证原子性，
// 输掉的调用方沿用赢者的条目，不存在重复播种或 read-check-clear 竞态。
func IncrementAuthError(poolID, accountID int) (count int, exceeded bool) {
	key := authErrorKey(poolID, accountID)
	val, loaded := globalAuthErrors.Load(key)
	if !loaded {
		val, _ = globalAuthErrors.LoadOrStore(key, loadAuthErrorSeed(poolID, accountID))
	}
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

// authErrorSnapshot 原子读取内存计数器的 (count, windowStart) 快照；
// 条目不存在时返回 (0, 0)。
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

// parseAuthErrorKey 反解 "poolID:accountID"（仅用于清理回写，解析失败返回 -1）。
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
// 驱逐时 best-effort 清零 DB 镜像列，避免恢复面板残留僵尸计数（B1-#7）。
func PurgeStaleAuthErrors() {
	now := time.Now().Unix()
	deadline := now - int64(authErrorWindow.Seconds())
	globalAuthErrors.Range(func(k, v interface{}) bool {
		entry := v.(*authErrorEntry)
		// 覆盖完窗口后仍无新增：窗口已过期，删除。
		if atomic.LoadInt64(&entry.windowStart) < deadline {
			// CompareAndDelete：仅在条目未被并发重建时驱逐，避免误删新条目。
			if globalAuthErrors.CompareAndDelete(k, v) {
				if poolID, accountID := parseAuthErrorKey(k.(string)); poolID >= 0 {
					_ = pool.UpdateAccount(poolID, accountID, map[string]interface{}{
						"auth_error_count":        0,
						"auth_error_window_start": int64(0),
					})
				}
			}
		}
		return true
	})
}
