package poolscheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

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
