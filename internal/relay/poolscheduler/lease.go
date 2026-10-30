package poolscheduler

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
)

var slotLifecycleLock sync.Mutex

// AccountLease owns the concrete counter acquired by one selection.
type AccountLease struct {
	Account *model.PoolAccount
	slots   *int64
	once    sync.Once
}

func (l *AccountLease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() { releaseSlotCounter(l.slots) })
}

func releaseSlotCounter(counter *int64) {
	if counter == nil {
		return
	}
	for {
		current := atomic.LoadInt64(counter)
		if current <= 0 || atomic.CompareAndSwapInt64(counter, current, current-1) {
			return
		}
	}
}

func accountSlotLimit(account *model.PoolAccount, fallback int) int {
	limit := account.EffectiveLoadFactor()
	if limit <= 0 {
		limit = account.EffectiveConcurrency(fallback)
	}
	return max(1, limit)
}

func acquireAccountSlot(poolID int, account *model.PoolAccount, fallback int) *AccountLease {
	slotLifecycleLock.Lock()
	defer slotLifecycleLock.Unlock()
	key := statsKey(poolID, account.ID)
	value, _ := globalPoolSlots.LoadOrStore(key, new(int64))
	counter := value.(*int64)
	limit := int64(accountSlotLimit(account, fallback))
	for {
		current := atomic.LoadInt64(counter)
		if current >= limit {
			return nil
		}
		if atomic.CompareAndSwapInt64(counter, current, current+1) {
			touchAccountActivity(poolID, account.ID)
			return &AccountLease{Account: account, slots: counter}
		}
	}
}

func touchAccountActivity(poolID, accountID int) {
	value, _ := globalPoolStats.LoadOrStore(statsKey(poolID, accountID), &accountStats{})
	stats := value.(*accountStats)
	stats.mu.Lock()
	stats.lastActivity = time.Now()
	stats.mu.Unlock()
}

func SelectAccountLease(poolID int, sessionHash string, excludeIDs []int, fallback int, modelName string) (*AccountLease, error) {
	escapedID := 0
	if sessionHash != "" {
		key := stickyKey(poolID, sessionHash)
		if value, ok := globalPoolSticky.Load(key); ok {
			if entry, valid := value.(*stickyEntry); valid {
				excluded := false
				for _, id := range excludeIDs {
					excluded = excluded || id == entry.AccountID
				}
				if !excluded {
					account, err := pool.GetAccount(poolID, entry.AccountID)
					if err != nil || !account.IsSchedulable() {
						globalPoolSticky.CompareAndDelete(key, entry)
					} else if model.ModelMatches(account.Models, modelName) {
						if shouldEscapeStickyAccount(poolID, account.ID) {
							escapedID = account.ID
						} else if lease := acquireAccountSlot(poolID, account, fallback); lease != nil {
							globalPoolSticky.CompareAndSwap(key, entry, &stickyEntry{AccountID: account.ID, LastActivity: time.Now()})
							return lease, nil
						}
					}
				}
			}
		}
	}
	candidates, err := pool.ListSchedulableAccounts(poolID)
	if err != nil {
		return nil, err
	}
	candidates = filterLayeredByPriority(filterByModel(filterExcluded(candidates, excludeIDs), modelName))
	if len(candidates) == 0 {
		triggerRefreshForExpired(poolID, modelName)
		return nil, ErrNoAvailableAccount
	}
	candidates = filterBySlot(candidates, poolID, fallback)
	if escapedID > 0 {
		if alternatives := filterExcluded(candidates, []int{escapedID}); len(alternatives) > 0 {
			candidates = alternatives
		}
	}
	for len(candidates) > 0 {
		selected := selectByStrategy(candidates, poolID)
		if lease := acquireAccountSlot(poolID, &selected, fallback); lease != nil {
			if sessionHash != "" && escapedID == 0 {
				globalPoolSticky.Store(stickyKey(poolID, sessionHash), &stickyEntry{AccountID: selected.ID, LastActivity: time.Now()})
			}
			return lease, nil
		}
		candidates = filterExcluded(candidates, []int{selected.ID})
	}
	return nil, ErrNoAvailableAccount
}
