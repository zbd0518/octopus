package task

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/alert"
	"github.com/lingyuins/octopus/internal/relay/poolscheduler"
)

func resetPoolAlertQueue(t *testing.T) {
	t.Helper()
	previous := poolAlertLastFired
	previousClock := poolAlertNow
	poolAlertLastFired = make(map[poolAlertKey]time.Time)
	t.Cleanup(func() {
		poolAlertLastFired = previous
		poolAlertNow = previousClock
	})
	for {
		select {
		case <-poolAlertEvents:
		default:
			return
		}
	}
}

func TestPoolAlertDedupIsPerRuleAndAccount(t *testing.T) {
	resetPoolAlertQueue(t)
	now := time.Now()
	rule := model.AlertRule{ID: 1, CooldownSec: 0}
	if !claimPoolAccountAlerts(rule, []int{10}, now) {
		t.Fatal("first error must fire")
	}
	if claimPoolAccountAlerts(rule, []int{10}, now.Add(4*time.Minute)) {
		t.Fatal("same account must be deduplicated within five minutes, including after recovery")
	}
	if !claimPoolAccountAlerts(rule, []int{11}, now.Add(4*time.Minute)) {
		t.Fatal("a different account must fire independently")
	}
	if !claimPoolAccountAlerts(model.AlertRule{ID: 2}, []int{10}, now.Add(4*time.Minute)) {
		t.Fatal("a different rule must fire independently")
	}
	if !claimPoolAccountAlerts(rule, []int{10}, now.Add(6*time.Minute)) {
		t.Fatal("another error six minutes later must fire")
	}
}

func TestPoolAlertQueueDropsWithoutBlocking(t *testing.T) {
	resetPoolAlertQueue(t)
	before := DroppedPoolAlertCount()
	for i := 0; i <= cap(poolAlertEvents); i++ {
		enqueuePoolStateChange(1, i+1, "error", "test")
	}
	if DroppedPoolAlertCount() != before+1 {
		t.Fatal("full queue must count the dropped event")
	}
	for len(poolAlertEvents) > 0 {
		<-poolAlertEvents
	}
}

func TestPoolErrorEventSurvivesRecoveryBeforeDelivery(t *testing.T) {
	setupAlertEvalDB(t)
	resetPoolAlertQueue(t)
	if err := db.GetDB().AutoMigrate(&model.AccountPool{}, &model.PoolAccount{}); err != nil {
		t.Fatal(err)
	}
	p := model.AccountPool{Name: "event-pool"}
	if err := db.GetDB().Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	account := model.PoolAccount{PoolID: p.ID, Name: "recovered-before-delivery", Status: "active"}
	if err := db.GetDB().Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	var webhooks atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhooks.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	channel := model.AlertNotifChannel{Name: "pool-event-webhook", Type: string(model.AlertNotifWebhook), URL: server.URL}
	if err := alert.NotifChannelCreate(context.Background(), &channel); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	poolAlertNow = func() time.Time { return now }
	rule := model.AlertRule{Name: "pool-error-event", Enabled: true, ConditionType: model.AlertConditionPoolAccountError, ScopePoolAccountID: account.ID, NotifChannelID: channel.ID}
	if err := alert.RuleCreate(context.Background(), &rule); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = alert.RuleDelete(context.Background(), rule.ID) })
	previous := poolscheduler.NotifyPoolStateChange
	poolscheduler.NotifyPoolStateChange = enqueuePoolStateChange
	t.Cleanup(func() { poolscheduler.NotifyPoolStateChange = previous })
	poolscheduler.SetError(p.ID, account.ID)
	if err := poolscheduler.RecoverAccount(p.ID, account.ID); err != nil {
		t.Fatal(err)
	}
	if eval := evaluatePoolAccountError(&rule); eval.Firing {
		t.Fatal("test account must already be recovered")
	}
	select {
	case event := <-poolAlertEvents:
		deliverPoolStateEvent(event)
	default:
		t.Fatal("SetError must emit an event")
	}
	poolscheduler.SetError(p.ID, account.ID)
	poolscheduler.SetError(p.ID, account.ID)
	for len(poolAlertEvents) > 0 {
		deliverPoolStateEvent(<-poolAlertEvents)
	}
	var count int64
	if err := db.GetDB().Model(&model.AlertHistory{}).Where("rule_id = ? AND state = ?", rule.ID, model.AlertStateFiring).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("firing history count = %d, want one deduplicated event", count)
	}
	EvaluateAlertRules()
	if err := db.GetDB().Model(&model.AlertHistory{}).Where("rule_id = ? AND state = ?", rule.ID, model.AlertStateFiring).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("periodic scan duplicated event: count=%d err=%v", count, err)
	}
	if webhooks.Load() != 1 {
		t.Fatalf("webhooks = %d, want 1", webhooks.Load())
	}
	now = now.Add(6 * time.Minute)
	poolscheduler.SetError(p.ID, account.ID)
	deliverPoolStateEvent(<-poolAlertEvents)
	if webhooks.Load() != 2 {
		t.Fatalf("webhooks after six minutes = %d, want 2", webhooks.Load())
	}
}
