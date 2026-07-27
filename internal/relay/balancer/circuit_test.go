package balancer

import (
	"testing"
	"time"

	ch "github.com/lingyuins/octopus/internal/op/channel"
)

// enterHalfOpen 把指定 (channelID, keyID, modelName) 的熔断器置为 HalfOpen 状态，
// 并把 HalfOpenSince 倒拨到过去，模拟"试探已丢失且超过探测超时"的场景。
// 这是 issue #162 的核心病根：试探请求被中途放弃（客户端断连、响应关键词拦截等
// 未记录结果的路径）后，HalfOpen 状态无人转换，IsTripped 永远返回 true → 渠道被
// 永久跳过，即使上游已恢复也无法重新探测。
func enterHalfOpen(channelID, keyID int, modelName string, enteredAt time.Time) {
	key := circuitKey(channelID, keyID, modelName)
	entry := &circuitEntry{
		State:           StateHalfOpen,
		LastFailureTime: enteredAt,
		HalfOpenSince:   enteredAt,
		TripCount:       1,
	}
	globalBreaker.Store(key, entry)
}

// withProbeTimeout 临时覆盖探测超时函数变量，测试结束后恢复。
func withProbeTimeout(d time.Duration, fn func()) {
	orig := halfOpenProbeTimeoutFunc
	halfOpenProbeTimeoutFunc = func() time.Duration { return d }
	defer func() { halfOpenProbeTimeoutFunc = orig }()
	fn()
}

// TestHalfOpenStallPreFixReproducesIssue162 复现 issue #162 的病根：
// HalfOpen 状态在探测超时机制不存在时会被永久跳过。
//
// 这里直接断言旧行为（无探测超时）：HalfOpen 一直返回 tripped，
// 证明没有修复前渠道会被永久跳过。配合下面的 TestHalfOpenProbeTimeoutAllowsRecovery
// 一并构成回归保护。
func TestHalfOpenStallPreFixReproducesIssue162(t *testing.T) {
	clearCircuitBreakerForTest()
	modelName := "issue162-stall"
	seedChannelWithKeys(1, []int{11})
	defer ch.GetCache().Del(1)

	// 禁用探测超时（模拟修复前的旧行为）。
	withProbeTimeout(0, func() {
		// 把 key 置为 HalfOpen 且 HalfOpenSince 在很久以前（本应已超时）。
		enterHalfOpen(1, 11, modelName, time.Now().Add(-10*time.Minute))

		// 旧行为：无论进入 HalfOpen 多久，IsTripped 都返回 true（永久跳过）。
		tripped, _ := IsTripped(1, 11, modelName)
		if !tripped {
			t.Fatalf("pre-fix behavior: HalfOpen without probe timeout should stay tripped (got not tripped), reproducing issue #162 stall")
		}
	})
}

// TestHalfOpenProbeTimeoutAllowsRecovery 验证 issue #162 的修复：
// HalfOpen 进入超过探测超时后，IsTripped 视为"试探已丢失"并允许一次新的试探
// （返回 tripped=false），同时刷新 HalfOpenSince 重置超时窗口，渠道不再被永久跳过。
func TestHalfOpenProbeTimeoutAllowsRecovery(t *testing.T) {
	clearCircuitBreakerForTest()
	modelName := "issue162-recover"
	seedChannelWithKeys(1, []int{11})
	defer ch.GetCache().Del(1)

	withProbeTimeout(60*time.Second, func() {
		// HalfOpen 进入时间在 2 分钟前，已超过 60s 探测超时。
		enterHalfOpen(1, 11, modelName, time.Now().Add(-2*time.Minute))

		tripped, remaining := IsTripped(1, 11, modelName)
		if tripped {
			t.Fatalf("IsTripped() = tripped, want not tripped after probe timeout (remaining=%v)", remaining)
		}
		if remaining != 0 {
			t.Fatalf("IsTripped() remaining = %v, want 0 on probe recovery", remaining)
		}

		// 进入 HalfOpen 后被刷新为 now，紧接着的第二次调用应返回 tripped=true
		// （窗口内的并发试探仍被拒绝，保持"单个试探"语义）。
		tripped2, _ := IsTripped(1, 11, modelName)
		if !tripped2 {
			t.Fatalf("IsTripped() after refresh should be tripped (probe window active, reject concurrent probes)")
		}
	})
}

// TestHalfOpenProbeTimeoutReadOnlyDoesNotDeprioritize 验证 issue #162 的修复在
// Auto 策略只读判定路径（isKeyTrippedReadOnly）上同样生效：探测超时后的 HalfOpen
// 不再被计入"全熔断"，渠道不会被 Auto 降权排末尾而无法被探测。
func TestHalfOpenProbeTimeoutReadOnlyDoesNotDeprioritize(t *testing.T) {
	clearCircuitBreakerForTest()
	modelName := "issue162-readonly"
	seedChannelWithKeys(1, []int{11})
	defer ch.GetCache().Del(1)

	withProbeTimeout(60*time.Second, func() {
		// HalfOpen 进入时间在 2 分钟前，已超过探测超时。
		enterHalfOpen(1, 11, modelName, time.Now().Add(-2*time.Minute))

		// 只读判定不应把超时的 HalfOpen 计为 tripped，从而 IsChannelAllKeysTripped
		// 返回 false（未全熔断），Auto 不降权该渠道。
		if isKeyTrippedReadOnly(1, 11, modelName) {
			t.Fatalf("isKeyTrippedReadOnly() = true, want false (timed-out HalfOpen should not deprioritize)")
		}
		if IsChannelAllKeysTripped(1, modelName) {
			t.Fatalf("IsChannelAllKeysTripped() = true, want false (timed-out HalfOpen must not deprioritize channel in Auto)")
		}
	})
}

// TestHalfOpenWithinProbeTimeoutStillTripped 验证探测超时窗口内的 HalfOpen
// 行为不变：仍被 IsTripped 视为 tripped（拒绝并发试探），isKeyTrippedReadOnly
// 也仍计为 tripped。确保修复没有破坏"半开只允许单个试探"的核心语义。
func TestHalfOpenWithinProbeTimeoutStillTripped(t *testing.T) {
	clearCircuitBreakerForTest()
	modelName := "issue162-within"
	seedChannelWithKeys(1, []int{11})
	defer ch.GetCache().Del(1)

	withProbeTimeout(60*time.Second, func() {
		// HalfOpen 刚刚进入（5 秒前），尚未超过 60s 探测超时。
		enterHalfOpen(1, 11, modelName, time.Now().Add(-5*time.Second))

		tripped, _ := IsTripped(1, 11, modelName)
		if !tripped {
			t.Fatalf("IsTripped() within probe window = not tripped, want tripped (reject concurrent probes)")
		}
		if !isKeyTrippedReadOnly(1, 11, modelName) {
			t.Fatalf("isKeyTrippedReadOnly() within probe window = false, want true")
		}
		if !IsChannelAllKeysTripped(1, modelName) {
			t.Fatalf("IsChannelAllKeysTripped() within probe window = false, want true (channel fully tripped)")
		}
	})
}

// TestRecordSuccessClearsHalfOpenSince 验证试探成功重置状态时 HalfOpenSince 被清空，
// 避免残留值在下一次 HalfOpen 周期误判超时。
func TestRecordSuccessClearsHalfOpenSince(t *testing.T) {
	clearCircuitBreakerForTest()
	modelName := "issue162-record-success"
	seedChannelWithKeys(1, []int{11})
	defer ch.GetCache().Del(1)

	enterHalfOpen(1, 11, modelName, time.Now().Add(-2*time.Minute))
	RecordSuccess(1, 11, modelName)

	key := circuitKey(1, 11, modelName)
	v, ok := globalBreaker.Load(key)
	if !ok {
		t.Fatalf("entry should exist after RecordSuccess")
	}
	entry := v.(*circuitEntry)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.State != StateClosed {
		t.Fatalf("State = %v, want StateClosed", entry.State)
	}
	if !entry.HalfOpenSince.IsZero() {
		t.Fatalf("HalfOpenSince = %v, want zero (cleared on success)", entry.HalfOpenSince)
	}
	if entry.TripCount != 0 {
		t.Fatalf("TripCount = %d, want 0 (reset on success)", entry.TripCount)
	}
}

// TestRecordFailureHalfOpenToOpenClearsSince 验证 HalfOpen 试探失败回到 Open 时
// HalfOpenSince 被清空，TripCount 递增（冷却翻倍）。
func TestRecordFailureHalfOpenToOpenClearsSince(t *testing.T) {
	clearCircuitBreakerForTest()
	modelName := "issue162-record-failure"
	seedChannelWithKeys(1, []int{11})
	defer ch.GetCache().Del(1)

	enterHalfOpen(1, 11, modelName, time.Now().Add(-30*time.Second))
	RecordFailure(1, 11, modelName)

	key := circuitKey(1, 11, modelName)
	v, ok := globalBreaker.Load(key)
	if !ok {
		t.Fatalf("entry should exist after RecordFailure")
	}
	entry := v.(*circuitEntry)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.State != StateOpen {
		t.Fatalf("State = %v, want StateOpen", entry.State)
	}
	if !entry.HalfOpenSince.IsZero() {
		t.Fatalf("HalfOpenSince = %v, want zero (cleared on HalfOpen->Open)", entry.HalfOpenSince)
	}
	if entry.TripCount != 2 {
		t.Fatalf("TripCount = %d, want 2 (incremented on probe failure)", entry.TripCount)
	}
}
