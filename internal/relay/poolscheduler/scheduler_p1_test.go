package poolscheduler

import (
	"context"
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
	"github.com/lingyuins/octopus/internal/op/setting"
)

var (
	sharedSchedulerDBOnce sync.Once
	sharedSchedulerDBErr  error
)

func ensureSchedulerSharedDB(t *testing.T) {
	t.Helper()
	sharedSchedulerDBOnce.Do(func() {
		// 不用 t.TempDir：共享 DB 句柄跨多个测试保持打开，在 Windows 上 RemoveAll 会因文件锁失败。
		dir, err := os.MkdirTemp("", "poolscheduler-test-*")
		if err != nil {
			sharedSchedulerDBErr = err
			return
		}
		dsn := filepath.Join(dir, "scheduler-test.db")
		sharedSchedulerDBErr = db.InitDB("sqlite", dsn, false)
		if sharedSchedulerDBErr != nil {
			return
		}
		sharedSchedulerDBErr = setting.RefreshCache(context.Background())
	})
	if sharedSchedulerDBErr != nil {
		t.Fatalf("init shared scheduler db: %v", sharedSchedulerDBErr)
	}
}

var schedulerPoolSeq int64

// 通过共享 DB 建一个独立池（名称唯一避免冲突），供调度策略测试使用。
func setupSchedulerPoolDB(t *testing.T) (poolID int, cleanup func()) {
	t.Helper()
	ensureSchedulerSharedDB(t)
	seq := atomic.AddInt64(&schedulerPoolSeq, 1)
	p := &model.AccountPool{
		Name:               fmt.Sprintf("pool-%d", seq),
		Strategy:           "ewma",
		DefaultConcurrency: 1,
		CooldownBaseSec:    300,
		Enabled:            true,
	}
	if err := pool.CreatePool(p); err != nil {
		t.Fatalf("create pool: %v", err)
	}
	poolID = p.ID
	cleanup = func() {
		RemovePool(poolID)
		_ = pool.DeletePool(poolID)
	}
	t.Cleanup(cleanup)
	return poolID, cleanup
}

func addAccount(t *testing.T, poolID int, a *model.PoolAccount) int {
	t.Helper()
	a.PoolID = poolID
	if a.Status == "" {
		a.Status = "active"
	}
	a.Schedulable = true
	if a.Platform == "" {
		a.Platform = model.PoolPlatformCustom
	}
	if a.Type == "" {
		a.Type = model.PoolTypeAPIKey
	}
	if a.Credentials == "" {
		a.Credentials = `{"type":"apikey","api_key":"sk-test"}`
	}
	if err := pool.CreateAccount(a); err != nil {
		t.Fatalf("create account: %v", err)
	}
	return a.ID
}

func TestSelectByLeastLoaded_PicksLowestRatio(t *testing.T) {
	poolID, _ := setupSchedulerPoolDB(t)
	// 两个账号同样 loadfactor=1，其中 acct1 已经有 3 个槽位，acct2 0 个。
	a1 := addAccount(t, poolID, &model.PoolAccount{Name: "a1", LoadFactor: 1})
	a2 := addAccount(t, poolID, &model.PoolAccount{Name: "a2", LoadFactor: 1})

	key1 := statsKey(poolID, a1)
	slot1, _ := globalPoolSlots.LoadOrStore(key1, new(int64))
	atomic.StoreInt64(slot1.(*int64), 3)
	key2 := statsKey(poolID, a2)
	slot2, _ := globalPoolSlots.LoadOrStore(key2, new(int64))
	atomic.StoreInt64(slot2.(*int64), 0)

	got := selectByLeastLoaded([]model.PoolAccount{
		{ID: a1, LoadFactor: 1},
		{ID: a2, LoadFactor: 1},
	}, poolID)
	if got.ID != a2 {
		t.Fatalf("least_loaded should pick account %d, got %d", a2, got.ID)
	}
}

func TestSelectByLeastLoaded_LoadFactorInfluences(t *testing.T) {
	poolID, _ := setupSchedulerPoolDB(t)
	// acct1: load=2 factor=1 → ratio 2.0；acct2: load=3 factor=4 → ratio 0.75 → 选 acct2。
	a1 := addAccount(t, poolID, &model.PoolAccount{Name: "a1", LoadFactor: 1})
	a2 := addAccount(t, poolID, &model.PoolAccount{Name: "a2", LoadFactor: 4})

	slot1, _ := globalPoolSlots.LoadOrStore(statsKey(poolID, a1), new(int64))
	atomic.StoreInt64(slot1.(*int64), 2)
	slot2, _ := globalPoolSlots.LoadOrStore(statsKey(poolID, a2), new(int64))
	atomic.StoreInt64(slot2.(*int64), 3)

	got := selectByLeastLoaded([]model.PoolAccount{
		{ID: a1, LoadFactor: 1},
		{ID: a2, LoadFactor: 4},
	}, poolID)
	if got.ID != a2 {
		t.Fatalf("expected account with lower load/factor ratio, got %d", got.ID)
	}
}

func TestSelectByEWMA_WeightTiltZeroStats(t *testing.T) {
	poolID, _ := setupSchedulerPoolDB(t)
	// 无历史统计时分数完全由 weight/priority 决定；weight 高者胜出。
	a1 := addAccount(t, poolID, &model.PoolAccount{Name: "a1", Weight: 0})
	a2 := addAccount(t, poolID, &model.PoolAccount{Name: "a2", Weight: 5})

	got := selectByEWMA([]model.PoolAccount{
		{ID: a1, Weight: 0},
		{ID: a2, Weight: 5},
	}, poolID)
	if got.ID != a2 {
		t.Fatalf("ewma with zero stats should prefer higher weight, got %d", got.ID)
	}
}

func TestFilterLayeredByPriority_WhenEnabledFilters(t *testing.T) {
	ensureSchedulerSharedDB(t)
	// 打开分层过滤 + 阈值 5
	if err := setting.SetString(model.SettingKeyPoolLayeredFilterEnabled, "true"); err != nil {
		t.Fatalf("set layered enabled: %v", err)
	}
	if err := setting.SetString(model.SettingKeyPoolMinPriority, "5"); err != nil {
		t.Fatalf("set min_priority: %v", err)
	}
	defer func() {
		_ = setting.SetString(model.SettingKeyPoolLayeredFilterEnabled, "false")
		_ = setting.SetString(model.SettingKeyPoolMinPriority, "-9999")
	}()

	candidates := []model.PoolAccount{
		{ID: 1, Priority: 1},
		{ID: 2, Priority: 5},
		{ID: 3, Priority: 10},
	}
	got := filterLayeredByPriority(candidates)
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates after filter, got %d", len(got))
	}
	for _, c := range got {
		if c.Priority < 5 {
			t.Fatalf("candidate %d should be filtered out", c.ID)
		}
	}
}

func TestPoolStrategyWhitelist_RejectsUnknown(t *testing.T) {
	_, _ = setupSchedulerPoolDB(t)
	p := &model.AccountPool{Name: "bad", Strategy: "nonsense"}
	if err := pool.CreatePool(p); err == nil {
		t.Fatalf("expected unsupported pool strategy error")
	}
}

// setStickyEscapeForTest 设置粘性逃逸三键并在测试后还原默认值（默认关闭）。
func setStickyEscapeForTest(t *testing.T, enabled, errorRate, ttftMs string) {
	t.Helper()
	set := func(key model.SettingKey, value string) {
		if err := setting.SetString(key, value); err != nil {
			t.Fatalf("set %s=%s: %v", key, value, err)
		}
	}
	set(model.SettingKeyPoolStickyEscapeEnabled, enabled)
	set(model.SettingKeyPoolStickyEscapeErrorRate, errorRate)
	set(model.SettingKeyPoolStickyEscapeTTFTMs, ttftMs)
	t.Cleanup(func() {
		_ = setting.SetString(model.SettingKeyPoolStickyEscapeEnabled, "false")
		_ = setting.SetString(model.SettingKeyPoolStickyEscapeErrorRate, "0.5")
		_ = setting.SetString(model.SettingKeyPoolStickyEscapeTTFTMs, "15000")
	})
}

// seedStickyForTest 直接向 globalPoolSticky 写入粘性条目并注册清理。
func seedStickyForTest(t *testing.T, poolID, accountID int, sessionHash string) {
	t.Helper()
	globalPoolSticky.Store(stickyKey(poolID, sessionHash), &stickyEntry{AccountID: accountID, LastActivity: time.Now()})
	t.Cleanup(func() { globalPoolSticky.Delete(stickyKey(poolID, sessionHash)) })
}

// seedStatsForTest 直接向 globalPoolStats 写入 EWMA 统计并注册清理。
func seedStatsForTest(t *testing.T, poolID, accountID int, errorRate, ttftMs float64) {
	t.Helper()
	globalPoolStats.Store(statsKey(poolID, accountID), &accountStats{errorRate: errorRate, ttftMs: ttftMs, lastActivity: time.Now()})
	t.Cleanup(func() { globalPoolStats.Delete(statsKey(poolID, accountID)) })
}

// TestStickyEscape_EscapeExcludesButPreservesEntry（B1-#9 核心验收）：
// 开启逃逸后，本次选号排除劣化账号（round_robin 也不会再选中它），
// 原粘性条目保留、不改绑。
func TestStickyEscape_EscapeExcludesButPreservesEntry(t *testing.T) {
	poolID, _ := setupSchedulerPoolDB(t)
	setStickyEscapeForTest(t, "true", "0.5", "15000")

	a1 := addAccount(t, poolID, &model.PoolAccount{Name: "sticky-bad"})
	a2 := addAccount(t, poolID, &model.PoolAccount{Name: "spare"})
	seedStickyForTest(t, poolID, a1, "sess-escape")
	seedStatsForTest(t, poolID, a1, 0.6, 0)

	// round_robin 策略下验证"排除"语义（否则 RR 可能立刻转回劣化账号）。
	if err := pool.UpdatePool(poolID, map[string]interface{}{"strategy": "round_robin"}); err != nil {
		t.Fatalf("set strategy: %v", err)
	}

	got, err := SelectAccount(poolID, "sess-escape", nil, 1, "")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.ID == a1 {
		t.Fatalf("escaped account %d must not be selected", a1)
	}
	if got.ID != a2 {
		t.Fatalf("expected spare account %d, got %d", a2, got.ID)
	}

	// 原粘性条目保留（未被改绑到 a2，也未被删除）。
	val, ok := globalPoolSticky.Load(stickyKey(poolID, "sess-escape"))
	if !ok {
		t.Fatalf("sticky entry must survive escape")
	}
	if entry := val.(*stickyEntry); entry.AccountID != a1 {
		t.Fatalf("sticky entry must keep original binding %d, got %d", a1, entry.AccountID)
	}

	_ = a2
}

// TestStickyEscape_RecoveredStatsReconverge：统计恢复后，会话回归原粘性绑定。
func TestStickyEscape_RecoveredStatsReconverge(t *testing.T) {
	poolID, _ := setupSchedulerPoolDB(t)
	setStickyEscapeForTest(t, "true", "0.5", "15000")

	a1 := addAccount(t, poolID, &model.PoolAccount{Name: "sticky-recover"})
	addAccount(t, poolID, &model.PoolAccount{Name: "spare-recover"})
	seedStickyForTest(t, poolID, a1, "sess-recover")
	seedStatsForTest(t, poolID, a1, 0.6, 0)

	if _, err := SelectAccount(poolID, "sess-recover", nil, 1, ""); err != nil {
		t.Fatalf("select during degrade: %v", err)
	}

	// 统计恢复：errorRate 归零。
	globalPoolStats.Store(statsKey(poolID, a1), &accountStats{errorRate: 0, ttftMs: 0, lastActivity: time.Now()})

	got, err := SelectAccount(poolID, "sess-recover", nil, 1, "")
	if err != nil {
		t.Fatalf("select after recovery: %v", err)
	}
	if got.ID != a1 {
		t.Fatalf("session should re-converge to original binding %d, got %d", a1, got.ID)
	}
}

// TestStickyEscape_DisabledKeepsOldBehavior（golden）：默认关闭时，即使
// 错误率超阈值，粘性命中行为与旧逻辑逐字节一致。
func TestStickyEscape_DisabledKeepsOldBehavior(t *testing.T) {
	poolID, _ := setupSchedulerPoolDB(t)
	// 不设置任何键：使用默认值 enabled=false。
	setStickyEscapeForTest(t, "false", "0.5", "15000")

	a1 := addAccount(t, poolID, &model.PoolAccount{Name: "sticky-default"})
	seedStickyForTest(t, poolID, a1, "sess-disabled")
	seedStatsForTest(t, poolID, a1, 0.9, 0)

	got, err := SelectAccount(poolID, "sess-disabled", nil, 1, "")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got.ID != a1 {
		t.Fatalf("disabled escape must keep sticky hit on %d, got %d", a1, got.ID)
	}
}

// TestStickyEscape_TTFTDimension：TTFT 超阈值触发逃逸；阈值 <=0 禁用该维度。
func TestStickyEscape_TTFTDimension(t *testing.T) {
	poolID, _ := setupSchedulerPoolDB(t)

	a1 := addAccount(t, poolID, &model.PoolAccount{Name: "sticky-ttft"})
	addAccount(t, poolID, &model.PoolAccount{Name: "spare-ttft"})
	seedStickyForTest(t, poolID, a1, "sess-ttft")
	seedStatsForTest(t, poolID, a1, 0, 20000)

	setStickyEscapeForTest(t, "true", "0.5", "15000")
	if got, err := SelectAccount(poolID, "sess-ttft", nil, 1, ""); err != nil || got.ID == a1 {
		t.Fatalf("high TTFT should escape sticky, got %v err=%v", got, err)
	}

	// 阈值 0 = 禁用 TTFT 维度（错误率未超阈值 → 不逃逸，回归粘性命中）。
	globalPoolSticky.Store(stickyKey(poolID, "sess-ttft"), &stickyEntry{AccountID: a1, LastActivity: time.Now()})
	globalPoolSlots.Delete(statsKey(poolID, a1))
	setStickyEscapeForTest(t, "true", "0.5", "0")
	if got, err := SelectAccount(poolID, "sess-ttft", nil, 1, ""); err != nil || got.ID != a1 {
		t.Fatalf("ttft threshold 0 must disable ttft escape, got %v err=%v", got, err)
	}
}
