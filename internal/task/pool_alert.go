package task

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/alert"
	"github.com/lingyuins/octopus/internal/relay/poolscheduler"
	"github.com/lingyuins/octopus/internal/utils/log"
)

const poolAlertDedupWindow = 5 * time.Minute
const poolAlertDedupLimit = 8192

type poolStateEvent struct {
	PoolID    int
	AccountID int
	Kind      string
	Detail    string
}

type poolAlertKey struct {
	RuleID    int
	AccountID int
}

var (
	poolAlertEvents    = make(chan poolStateEvent, 256)
	poolAlertDropped   atomic.Int64
	poolAlertStartOnce sync.Once
	poolAlertStop      = make(chan struct{})
	poolAlertDone      = make(chan struct{})
	poolAlertStarted   atomic.Bool
	poolAlertLastFired = make(map[poolAlertKey]time.Time)
	poolAlertNow       = time.Now
)

// DroppedPoolAlertCount exposes bounded-queue degradation without blocking relay.
func DroppedPoolAlertCount() int64 { return poolAlertDropped.Load() }

func enqueuePoolStateChange(poolID, accountID int, kind, detail string) {
	// Other scheduler transitions do not satisfy pool_account_error rules.
	if kind != "error" {
		return
	}
	select {
	case poolAlertEvents <- poolStateEvent{PoolID: poolID, AccountID: accountID, Kind: kind, Detail: detail}:
	default:
		poolAlertDropped.Add(1)
	}
}

func startPoolAlertWorker() {
	poolAlertStartOnce.Do(func() {
		poolAlertStarted.Store(true)
		poolscheduler.NotifyPoolStateChange = enqueuePoolStateChange
		go func() {
			defer close(poolAlertDone)
			for {
				select {
				case event := <-poolAlertEvents:
					deliverPoolStateEvent(event)
				case <-poolAlertStop:
					for {
						select {
						case event := <-poolAlertEvents:
							deliverPoolStateEvent(event)
						default:
							return
						}
					}
				}
			}
		}()
	})
}

func deliverPoolStateEvent(event poolStateEvent) {
	if event.Kind != "error" {
		return
	}
	alertEvaluationMu.Lock()
	defer alertEvaluationMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rules, err := alert.RuleList(ctx)
	if err != nil {
		log.Warnf("pool alert: list rules: %v", err)
		return
	}
	channels, err := alert.NotifChannelList(ctx)
	if err != nil {
		log.Warnf("pool alert: list notification channels: %v", err)
	}
	channelMap := make(map[int]*model.AlertNotifChannel, len(channels))
	for i := range channels {
		channelMap[channels[i].ID] = &channels[i]
	}
	for _, rule := range rules {
		if !rule.Enabled || rule.ConditionType != model.AlertConditionPoolAccountError ||
			(rule.ScopePoolAccountID > 0 && rule.ScopePoolAccountID != event.AccountID) {
			continue
		}
		if !claimPoolAccountAlerts(rule, []int{event.AccountID}, poolAlertNow()) {
			continue
		}
		eval := alertEvaluation{
			Firing: true, CurrentValue: 1, AccountIDs: []int{event.AccountID},
			Detail: fmt.Sprintf("pool %d account %d entered error: %.512s", event.PoolID, event.AccountID, event.Detail),
		}
		// Do not re-read status: recovery may have completed before this queued
		// event is consumed, but the error transition still deserves delivery.
		firePoolAccountAlert(&rule, channelMap, eval)
	}
}

// Caller holds alertEvaluationMu, shared by event delivery and periodic scans.
func claimPoolAccountAlerts(rule model.AlertRule, accountIDs []int, now time.Time) bool {
	for key, expiresAt := range poolAlertLastFired {
		if !now.Before(expiresAt) {
			delete(poolAlertLastFired, key)
		}
	}
	window := poolAlertDedupWindow
	if rule.CooldownSec > int(window.Seconds()) {
		window = time.Duration(rule.CooldownSec) * time.Second
		if window < poolAlertDedupWindow {
			window = poolAlertDedupWindow
		}
	}
	claimed := false
	for _, accountID := range accountIDs {
		key := poolAlertKey{RuleID: rule.ID, AccountID: accountID}
		if expiresAt, ok := poolAlertLastFired[key]; ok && now.Before(expiresAt) {
			continue
		}
		if len(poolAlertLastFired) >= poolAlertDedupLimit {
			var oldestKey poolAlertKey
			var oldest time.Time
			for candidate, expiry := range poolAlertLastFired {
				if oldest.IsZero() || expiry.Before(oldest) {
					oldestKey, oldest = candidate, expiry
				}
			}
			delete(poolAlertLastFired, oldestKey)
		}
		poolAlertLastFired[key] = now.Add(window)
		claimed = true
	}
	return claimed
}

func firePoolAccountAlert(rule *model.AlertRule, channels map[int]*model.AlertNotifChannel, eval alertEvaluation) {
	alert.StateSet(rule.ID, model.AlertStateFiring)
	current := alert.StateGet(rule.ID)
	notify := notifyAlert(rule, channels, alertStateFiring, current, eval)
	recordHistory(rule, model.AlertStateFiring, "pool account error", notify, eval)
}
