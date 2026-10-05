package poolscheduler

import (
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/model"
)

func TestPoolStateWritersEmitOnlyAfterSuccessfulWrites(t *testing.T) {
	poolID, _ := setupSchedulerPoolDB(t)
	accountID := addAccount(t, poolID, &model.PoolAccount{Name: "state-events"})
	previous := NotifyPoolStateChange
	var kinds []string
	NotifyPoolStateChange = func(pid, aid int, kind, detail string) {
		if pid != poolID || aid != accountID {
			t.Fatalf("unexpected event account: %d/%d", pid, aid)
		}
		kinds = append(kinds, kind)
	}
	t.Cleanup(func() { NotifyPoolStateChange = previous })
	until := time.Now().Add(time.Minute)
	SetRateLimitCooldown(poolID, accountID, until)
	SetOverload(poolID, accountID, until)
	SetTempUnsched(poolID, accountID, until, `{"trigger":"rule"}`)
	SetError(poolID, accountID)
	ClearTempUnsched(poolID, accountID)
	SetError(poolID, -1)
	want := []string{"rate_limit", "overload", "temp_unsched", "error"}
	if len(kinds) != len(want) {
		t.Fatalf("events = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("events = %v, want %v", kinds, want)
		}
	}
}
