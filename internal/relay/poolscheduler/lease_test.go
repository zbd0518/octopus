package poolscheduler

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/model"
)

func TestAccountLeaseConcurrentAdmissionAndRelease(t *testing.T) {
	poolID, _ := setupSchedulerPoolDB(t)
	accountID := addAccount(t, poolID, &model.PoolAccount{Name: t.Name(), Concurrency: 1})
	start := make(chan struct{})
	results := make(chan *AccountLease, 32)
	var workers sync.WaitGroup
	for range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			lease, _ := SelectAccountLease(poolID, "", nil, 1, "")
			results <- lease
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	var acquired []*AccountLease
	for lease := range results {
		if lease != nil {
			acquired = append(acquired, lease)
		}
	}
	if len(acquired) != 1 {
		t.Fatalf("acquired %d leases, want 1", len(acquired))
	}
	lease := acquired[0]
	RemoveAccount(poolID, accountID)
	if next, err := SelectAccountLease(poolID, "", nil, 1, ""); err == nil {
		next.Release()
		t.Fatal("removing runtime state admitted a second active lease")
	}
	lease.Release()
	lease.Release()
	if got := atomic.LoadInt64(lease.slots); got != 0 {
		t.Fatalf("counter after repeated release = %d", got)
	}
	next, err := SelectAccountLease(poolID, "", nil, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	defer next.Release()
	lease.Release()
	if atomic.LoadInt64(next.slots) != 1 {
		t.Fatal("old release changed a new lease")
	}
}

func TestPurgeStaleKeepsActiveAndRemovesOrphanCounters(t *testing.T) {
	account := &model.PoolAccount{ID: 919991, Concurrency: 1}
	lease := acquireAccountSlot(919990, account, 1)
	defer RemoveAccount(919990, account.ID)
	value, _ := globalPoolStats.Load(statsKey(919990, account.ID))
	stats := value.(*accountStats)
	stats.mu.Lock()
	stats.lastActivity = time.Now().Add(-time.Hour)
	stats.mu.Unlock()
	PurgeStale(time.Minute)
	if _, ok := globalPoolSlots.Load(statsKey(919990, account.ID)); !ok {
		t.Fatal("purged an active counter")
	}
	lease.Release()
	globalPoolStats.Delete(statsKey(919990, account.ID))
	PurgeStale(time.Minute)
	if _, ok := globalPoolSlots.Load(statsKey(919990, account.ID)); ok {
		t.Fatal("orphan idle counter retained")
	}
}
