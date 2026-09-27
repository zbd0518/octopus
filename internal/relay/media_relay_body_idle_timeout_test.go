package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	dbmodel "github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/utils/xurl"
)

// 本文件回归的是「媒体转发路径的响应体读取没有任何时间上限」这一缺陷：
// default 档 http.Client.Timeout 被改为 0（不限时）后，Transport.ResponseHeaderTimeout
// 只覆盖「等响应头」，operationCtx 在未设 OCTOPUS_RELAY_UPSTREAM_TIMEOUT_SECONDS 时
// 也无 deadline（context.go），而 media_relay.go 原本一个 watchdog 都没有
// （SetReadDeadline / Ticker / time.After / ReadTimeout 在该文件 0 命中）。
// 于是上游发完响应头再 hang 住 body 时，请求 goroutine 会永久挂住。
//
// 修复采用「空闲超时」而非「总时长上限」：媒体端点包含视频 / 音乐生成，
// 响应体可能又大又慢，总时长上限会误杀合法的慢速下载；空闲超时只杀
// 「上游彻底 stall」，对持续产出的慢速下载完全放行（见 TestForwardMediaRequestSlowTrickleIsNotKilled）。
//
// 一律不加 t.Parallel()：mediaBodyIdleTimeout 是包级变量，且 balancer 全局 sync.Map、
// setting 全局 cache、client 包级缓存都不可并行。

// setMediaBodyIdleTimeoutForTest 临时改小包级空闲超时并在测试结束时还原，
// 沿用 context_test.go 覆盖 relayUpstreamTimeout 的既有模式
// （不为可测性改造生产代码签名，也不依赖 t.Setenv 与 init() 的一次性读取时机）。
func setMediaBodyIdleTimeoutForTest(t *testing.T, d time.Duration) {
	t.Helper()
	original := mediaBodyIdleTimeout
	mediaBodyIdleTimeout = d
	t.Cleanup(func() { mediaBodyIdleTimeout = original })
}

// waitForGoroutinesToSettle 轮询等待 goroutine 数回落到基线附近，
// 用于证明 watchdog 没有泄漏。留 +2 余量吸收测试运行时自身的 goroutine。
func waitForGoroutinesToSettle(t *testing.T, baseline int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runtime.Gosched()
		if runtime.NumGoroutine() <= baseline+2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("goroutine 未回落到基线：before=%d after=%d（watchdog 泄漏）",
		baseline, runtime.NumGoroutine())
}

// fakeStallBody 复刻「上游发完响应头后把 body 永久 hang 住」的真实网络语义：
// Read 阻塞直到 Close 被调用，随后返回与 net 包同风格的读错误。
// 只有这样，watchdog 的 Close 才能真实地打断阻塞中的 Read
// （生产里 http.Response.Body 是 *bodyEOFSignal，其 Read 在阻塞前已释放自身锁，
// 因此并发 Close 不会死锁——这一点已在实施前实测确认）。
type fakeStallBody struct {
	closeOnce sync.Once
	closed    chan struct{}
}

func newFakeStallBody() *fakeStallBody {
	return &fakeStallBody{closed: make(chan struct{})}
}

func (b *fakeStallBody) Read(p []byte) (int, error) {
	<-b.closed
	return 0, errors.New("read tcp 127.0.0.1:1->127.0.0.1:2: use of closed network connection")
}

func (b *fakeStallBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

// newMediaStallUpstream 起一个 httptest 上游，并保证 handler 与 server 都能干净退出：
// release 由 t.Cleanup 关闭，handler 解除阻塞之后 server 才 Close
// （httptest.Server.Close 会等所有 handler 返回，顺序反了会卡住并打印 blocked in Close）。
func newMediaStallUpstream(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, release <-chan struct{})) *httptest.Server {
	t.Helper()
	xurl.SetSSRFAllowPrivateForTest(true)

	release := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(w, r, release)
	}))
	t.Cleanup(func() {
		once.Do(func() { close(release) })
		srv.Close()
		xurl.SetSSRFAllowPrivateForTest(false)
	})
	return srv
}

// newMediaTestGinContext 构造一个 gin 测试上下文（照抄 media_relay_raw_test.go / media_relay_disconnect_test.go 的先例）。
func newMediaTestGinContext(t *testing.T, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

// ---------------------------------------------------------------------------
// 包装器单元测试：精确控制时序，验证 watchdog 的触发、放行与退出。
// ---------------------------------------------------------------------------

// 缺陷主回归：上游永久 stall 时，阻塞中的 Read 必须被 watchdog 打断并返回超时错误。
// 未修复代码上（裸 io.Copy(response.Body)）此断言会以「测试超时 panic」的形式失败。
func TestMediaIdleTimeoutBodyFiresAndUnblocksBlockedRead(t *testing.T) {
	src := newFakeStallBody()
	b := newMediaIdleTimeoutBody(src, 80*time.Millisecond)

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, b)
		done <- err
	}()

	select {
	case err := <-done:
		elapsed := time.Since(start)
		if err == nil {
			t.Fatal("空闲超时后 Read 应返回错误，got nil")
		}
		if elapsed > 5*time.Second {
			t.Fatalf("watchdog 未在合理时间内收尾：elapsed=%v", elapsed)
		}
		// 错误文案必须被 isTimeoutError 命中，否则 ClassifyRelayError 的归类会漂移。
		if !isTimeoutError(err) {
			t.Fatalf("错误未被 isTimeoutError 命中，会被误分类：err=%v", err)
		}
		if !strings.Contains(err.Error(), "idle timeout") {
			t.Fatalf("错误文案应说明是空闲超时，got %q", err.Error())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Read 仍阻塞：watchdog 没能打断它（未修复代码上正是这个永久挂死）")
	}

	// 触发后再次 Read 必须继续报错，不能复活；Close 也必须可安全重复调用。
	if _, err := b.Read(make([]byte, 8)); err == nil {
		t.Fatal("watchdog 触发后 Read 应持续返回错误")
	}
	if err := b.Close(); err != nil {
		t.Fatalf("触发后 Close 应可安全调用：%v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close 应可重入：%v", err)
	}

	waitForGoroutinesToSettle(t, runtime.NumGoroutine()-1)
}

// 对照组：正常完成 / 读失败 / 客户端断连 / panic 展开都走 Close，
// watchdog 必须靠 Close 退出且不泄漏（idleTimeout 设为 1h，保证测试期内绝不触发）。
func TestMediaIdleTimeoutBodyCloseStopsWatchdogWithoutFiring(t *testing.T) {
	before := runtime.NumGoroutine()

	for i := 0; i < 20; i++ {
		b := newMediaIdleTimeoutBody(io.NopCloser(strings.NewReader("payload")), time.Hour)

		buf := make([]byte, 32)
		n, err := b.Read(buf)
		if err != nil || string(buf[:n]) != "payload" {
			t.Fatalf("第 %d 轮：正常读被 watchdog 干扰，n=%d err=%v", i, n, err)
		}
		if b.fired.Load() {
			t.Fatalf("第 %d 轮：空闲阈值 1h，watchdog 不应触发", i)
		}
		if err := b.Close(); err != nil {
			t.Fatalf("第 %d 轮 Close：%v", i, err)
		}
		if err := b.Close(); err != nil {
			t.Fatalf("第 %d 轮重复 Close：%v", i, err)
		}
	}

	waitForGoroutinesToSettle(t, before)
}

// 触发路径同样不得泄漏：watchdog 关闭底层 body 后必须自行退出。
func TestMediaIdleTimeoutBodyWatchdogExitsAfterFiring(t *testing.T) {
	before := runtime.NumGoroutine()

	for i := 0; i < 10; i++ {
		b := newMediaIdleTimeoutBody(newFakeStallBody(), 40*time.Millisecond)
		if _, err := b.Read(make([]byte, 16)); err == nil {
			t.Fatalf("第 %d 轮：阻塞读应被 watchdog 打断", i)
		}
		if err := b.Close(); err != nil {
			t.Fatalf("第 %d 轮 Close：%v", i, err)
		}
	}

	waitForGoroutinesToSettle(t, before)
}

// watchdog 必须「准时」触发，不得显著超调：轮询实现最坏会晚到空闲阈值的
// 25%，使实际上限比声称的宽；改成按需重新武装的 timer 后应接近阈值本身。
func TestMediaIdleTimeoutBodyFiresPromptlyWithoutOvershoot(t *testing.T) {
	const idleTimeout = 400 * time.Millisecond
	b := newMediaIdleTimeoutBody(newFakeStallBody(), idleTimeout)

	start := time.Now()
	_, err := b.Read(make([]byte, 16))
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("阻塞读应被 watchdog 打断")
	}
	// 下限：不得早于阈值触发，否则会误杀合法的慢速下载。
	if elapsed < idleTimeout {
		t.Fatalf("watchdog 触发过早：elapsed=%v < idleTimeout=%v", elapsed, idleTimeout)
	}
	// 上限：idleTimeout + 200ms 宽限（吸收 CI 调度抖动）。
	if elapsed > idleTimeout+200*time.Millisecond {
		t.Fatalf("watchdog 触发过晚：elapsed=%v, idleTimeout=%v", elapsed, idleTimeout)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close：%v", err)
	}
}

// 「间歇 stall 后恢复产出」不得被杀，但最后那次真 stall 必须被抓。
//
// 这条专门盖住 watch() 重新武装 timer 的分支：上游产出 → 停顿（小于阈值）→
// 继续产出 → 停顿（小于阈值）→ 永久 stall。只有最后一段该触发 watchdog；
// 若计时错用了「从构造时刻起算」而不是「从最后一次进展起算」，
// 中间那段就会被误杀（总时长已超过 idleTimeout）。
func TestMediaIdleTimeoutBodyRearmsAfterResumedProgress(t *testing.T) {
	const (
		idleTimeout = 240 * time.Millisecond
		pause       = 80 * time.Millisecond // 小于阈值，不得触发
	)

	pr, pw := io.Pipe()

	// stall 关闭即通知 writer 退出，避免遗留空转 goroutine。
	// 用 sync.Once 包住：测试体会主动关一次，t.Cleanup 还要兜底再关一次
	//（断言提前 t.Fatalf 时测试体那次关不到），裸 close 会 panic: close of closed channel。
	stall := make(chan struct{})
	var stallOnce sync.Once
	releaseWriter := func() { stallOnce.Do(func() { close(stall) }) }
	t.Cleanup(releaseWriter)

	b := newMediaIdleTimeoutBody(pr, idleTimeout)

	writerDone := make(chan error, 1)
	go func() {
		var wErr error
		defer func() { writerDone <- wErr }()
		for i := 0; i < 3; i++ {
			if _, err := pw.Write([]byte(fmt.Sprintf("seg-%d;", i))); err != nil {
				wErr = err
				return
			}
			time.Sleep(pause)
		}
		// 最后一段：真正的 stall —— 一个字节都不再写。
		// 注意不能用 pw.Write 来「阻塞」：io.Pipe 的 Write 只在读端消费时才阻塞，
		// 而读端正在 io.ReadAll 里循环消费，所以 Write 会立刻成功并交付字节，
		// 反而重置了空闲计时（实测：内容会变成 "...seg-2;never-delivered"）。
		// 改为阻塞在 stall 上：既不产出，又能由 t.Cleanup 确定性唤醒。
		<-stall
		_ = pw.Close()
	}()

	start := time.Now()
	got, err := io.ReadAll(b)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("最后的永久 stall 应触发 watchdog 并让 ReadAll 报错")
	}
	if !isTimeoutError(err) {
		t.Fatalf("错误未被 isTimeoutError 命中：%v", err)
	}
	// 三段都应在 stall 前完整读到，且不多不少（writer 已不再产出）。
	if string(got) != "seg-0;seg-1;seg-2;" {
		t.Fatalf("中间间歇被误杀或内容被污染：got=%q", string(got))
	}
	// 触发时刻应落在「最后一次产出起算 idleTimeout」附近。
	// 三段分别在 t≈0 / pause / 2*pause 产出，最后一次进展在 2*pause=160ms，
	// 故期望触发时刻 ≈ 2*pause + idleTimeout = 400ms。
	// 若计时基准错用「构造时刻」，触发会落在 idleTimeout=240ms 附近，明显早于下界。
	if elapsed < 2*pause+idleTimeout {
		t.Fatalf("触发过早，说明计时基准不是「最后一次进展」：elapsed=%v", elapsed)
	}
	if elapsed > 2*pause+idleTimeout+500*time.Millisecond {
		t.Fatalf("触发过晚：elapsed=%v", elapsed)
	}
	// 唤醒 writer（它此刻正阻塞在 <-stall，一个字节都没再写），并确认它干净退出。
	releaseWriter()
	if wErr := <-writerDone; wErr != nil {
		t.Fatalf("writer 不应报错（它只是被通知退出）：%v", wErr)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close：%v", err)
	}
	_ = pr.Close()
}

// 「缓慢但持续产出」不得被杀：每次 Read 拿到字节就重置计时，
// 因此总时长可以远超空闲阈值。这正是空闲超时优于总时长上限的地方。
func TestMediaIdleTimeoutBodySlowTrickleKeepsProgressing(t *testing.T) {
	const (
		idleTimeout = 300 * time.Millisecond
		chunkDelay  = 30 * time.Millisecond
		chunks      = 20
	)

	pr, pw := io.Pipe()
	b := newMediaIdleTimeoutBody(pr, idleTimeout)

	// 上游：每 chunkDelay 滴一片，总时长 chunks*chunkDelay = 600ms > idleTimeout = 300ms。
	writerDone := make(chan error, 1)
	go func() {
		var wErr error
		for i := 0; i < chunks; i++ {
			time.Sleep(chunkDelay)
			if _, err := pw.Write([]byte(fmt.Sprintf("chunk-%02d;", i))); err != nil {
				wErr = err
				return
			}
		}
		writerDone <- wErr
		_ = pw.Close()
	}()

	got, err := io.ReadAll(b)
	if err != nil {
		t.Fatalf("持续产出的上游被误杀（空闲超时退化成总时长上限？）：%v", err)
	}
	if b.fired.Load() {
		t.Fatal("watchdog 不应在持续产出时触发")
	}
	var want strings.Builder
	for i := 0; i < chunks; i++ {
		want.WriteString(fmt.Sprintf("chunk-%02d;", i))
	}
	if string(got) != want.String() {
		t.Fatalf("内容不完整：\n got=%q\nwant=%q", string(got), want.String())
	}
	if err := <-writerDone; err != nil {
		t.Fatalf("上游写入失败：%v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close：%v", err)
	}
}

// 开关与解析：idleTimeout<=0 或 nil body 时必须原样返回，不引入包装与 goroutine。
func TestWrapMediaResponseBodyDisabled(t *testing.T) {
	src := io.NopCloser(strings.NewReader("x"))
	if got := wrapMediaResponseBody(src, 0); got != src {
		t.Fatal("idleTimeout=0 时应原样返回，不引入包装")
	}
	if got := wrapMediaResponseBody(src, -time.Second); got != src {
		t.Fatal("idleTimeout<0 时应原样返回，不引入包装")
	}
	if got := wrapMediaResponseBody(nil, time.Second); got != nil {
		t.Fatal("nil body 应原样返回")
	}
	if _, ok := wrapMediaResponseBody(src, time.Second).(*mediaIdleTimeoutBody); !ok {
		t.Fatal("idleTimeout>0 时应返回 mediaIdleTimeoutBody 包装")
	}
}

// 环境变量解析：空值 / 非法 / <=0 一律回退，绝不产生「无上限」的退化值。
func TestResolveMediaBodyIdleTimeout(t *testing.T) {
	const fallback = 600 * time.Second

	for _, tt := range []struct {
		raw  string
		want time.Duration
	}{
		{"", fallback},
		{"   ", fallback},
		{"abc", fallback},
		{"0", fallback},
		{"-5", fallback},
		{"30", 30 * time.Second},
		{" 45 ", 45 * time.Second},
	} {
		if got := resolveMediaBodyIdleTimeout(tt.raw, fallback); got != tt.want {
			t.Fatalf("resolveMediaBodyIdleTimeout(%q) = %v, want %v", tt.raw, got, tt.want)
		}
	}

	// 默认值必须有限：这是「不再永久挂住」这一性质的根。
	if defaultMediaBodyIdleTimeout <= 0 {
		t.Fatalf("defaultMediaBodyIdleTimeout 必须为正，got %v", defaultMediaBodyIdleTimeout)
	}
	if mediaBodyIdleTimeout <= 0 {
		t.Fatalf("mediaBodyIdleTimeout 必须为正，got %v", mediaBodyIdleTimeout)
	}
}

// ---------------------------------------------------------------------------
// 端到端：经 forwardMediaRequestJSON / forwardMediaRequestMultipart 的真实转发路径。
// ---------------------------------------------------------------------------

// 二进制（TTS）路径的上游 stall：io.Copy 必须被 watchdog 打断，
// 错误必须流入既有 ClassifyRelayError，并被判为上游故障（记熔断 + 换渠道）。
func TestForwardMediaRequestJSONStalledBinaryBodyTimesOut(t *testing.T) {
	setMediaBodyIdleTimeoutForTest(t, 200*time.Millisecond)

	upstream := newMediaStallUpstream(t, func(w http.ResponseWriter, r *http.Request, release <-chan struct{}) {
		// 立刻发响应头（上游已接受请求），随后永久不写 body。
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release
	})

	const body = `{"model":"tts-model","input":"hello"}`
	c, recorder := newMediaTestGinContext(t, "/v1/audio/speech", body)
	cfg := mediaEndpointConfig{UpstreamPath: "/v1/audio/speech", BinaryResponse: true}

	start := time.Now()
	statusCode, fwdErr := forwardMediaRequestJSON(
		c, cfg, dbmodel.Group{},
		&dbmodel.Channel{BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}}},
		"sk-test", []byte(body),
		"tts-model", "tts-model", false, context.Background(),
	)
	elapsed := time.Since(start)

	if fwdErr == nil {
		t.Fatal("上游 body 永久 stall 时应返回错误（未修复代码上这里会永久阻塞，测试以超时失败）")
	}
	if statusCode != 0 {
		t.Fatalf("statusCode = %d, want 0（媒体路径读失败约定置 0）", statusCode)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("watchdog 未及时收尾：elapsed=%v", elapsed)
	}
	if !isTimeoutError(fwdErr) {
		t.Fatalf("错误未被 isTimeoutError 命中，会被误分类：%v", fwdErr)
	}

	// 一个字节都没写给下游 → written=false → classifyNonHTTPError → 超时 → ScopeNextChannel。
	// 这是期望行为：换渠道重试，且 shouldRecordChannelFailure 判定为真实上游故障、记熔断。
	if c.Writer.Written() {
		t.Fatal("上游未写 body，不应已向客户端写出字节")
	}
	decision := ClassifyRelayError(statusCode, fwdErr, c.Writer.Written())
	if decision.Scope != ScopeNextChannel {
		t.Fatalf("Scope = %v, want ScopeNextChannel", decision.Scope)
	}
	if !shouldRecordChannelFailure(0, decision) {
		t.Fatal("上游 stall 是真实渠道故障，必须记熔断（不得被 SkipFailureAccounting 豁免）")
	}
	if decision.SkipFailureAccounting {
		t.Fatal("上游 stall 不是客户端断连，不得标记 SkipFailureAccounting")
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("不应向客户端写出任何字节，got %q", recorder.Body.String())
	}
}

// SSE 路径同样无界（getReader + ReadSlice），必须一并被 watchdog 覆盖。
// 这里上游已滴出半条事件 → written=true → ClassifyRelayError 归为 ScopeAbortAll：
// 已向下游流式写出部分内容，不能再换渠道重放，但仍记熔断（上游故障）。
func TestForwardMediaRequestJSONStalledSSEBodyTimesOut(t *testing.T) {
	setMediaBodyIdleTimeoutForTest(t, 200*time.Millisecond)

	upstream := newMediaStallUpstream(t, func(w http.ResponseWriter, r *http.Request, release <-chan struct{}) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: partial-chunk\n"))
		w.(http.Flusher).Flush()
		<-release
	})

	const body = `{"model":"chat-model","stream":true}`
	c, recorder := newMediaTestGinContext(t, "/v1/chat/completions", body)
	cfg := mediaEndpointConfig{UpstreamPath: "/v1/chat/completions"}

	statusCode, fwdErr := forwardMediaRequestJSON(
		c, cfg, dbmodel.Group{},
		&dbmodel.Channel{BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}}},
		"sk-test", []byte(body),
		"chat-model", "chat-model", true, context.Background(),
	)

	if fwdErr == nil {
		t.Fatal("SSE 上游 stall 时应返回错误（未修复代码上 ReadSlice 会永久阻塞）")
	}
	if statusCode != 0 {
		t.Fatalf("statusCode = %d, want 0", statusCode)
	}
	if !isTimeoutError(fwdErr) {
		t.Fatalf("错误未被 isTimeoutError 命中：%v", fwdErr)
	}
	if !c.Writer.Written() {
		t.Fatal("上游已滴出半条事件，应已写出字节")
	}
	decision := ClassifyRelayError(statusCode, fwdErr, c.Writer.Written())
	if decision.Scope != ScopeAbortAll {
		t.Fatalf("已写出部分内容，Scope = %v, want ScopeAbortAll", decision.Scope)
	}
	if !shouldRecordChannelFailure(0, decision) {
		t.Fatal("上游 stall 仍应记熔断")
	}
	if !strings.Contains(recorder.Body.String(), "partial-chunk") {
		t.Fatalf("已滴出的事件应原样透传，got %q", recorder.Body.String())
	}
}

// multipart 转发路径是第二个接线点（forwardMediaRequestMultipart 有自己独立的
// response.Body 与 defer），必须同样被覆盖。
func TestForwardMediaRequestMultipartStalledBodyTimesOut(t *testing.T) {
	setMediaBodyIdleTimeoutForTest(t, 200*time.Millisecond)

	upstream := newMediaStallUpstream(t, func(w http.ResponseWriter, r *http.Request, release <-chan struct{}) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release
	})

	c, _ := newMediaTestGinContext(t, "/v1/audio/transcriptions", "")
	// 直接构造已解析的 multipart 表单，避免依赖真实 multipart 解析
	// （forwardMediaRequestMultipart 只读 c.Request.MultipartForm）。
	c.Request.MultipartForm = &multipart.Form{
		Value: map[string][]string{"model": {"whisper-1"}},
	}
	cfg := mediaEndpointConfig{UpstreamPath: "/v1/audio/transcriptions", MultipartInput: true}

	statusCode, fwdErr := forwardMediaRequestMultipart(
		c, cfg,
		&dbmodel.Channel{BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}}},
		"sk-test", "whisper-1", "whisper-1", false, context.Background(),
	)

	if fwdErr == nil {
		t.Fatal("multipart 路径上游 stall 时应返回错误（第二个接线点未生效？）")
	}
	if statusCode != 0 {
		t.Fatalf("statusCode = %d, want 0", statusCode)
	}
	if !isTimeoutError(fwdErr) {
		t.Fatalf("错误未被 isTimeoutError 命中：%v", fwdErr)
	}
}

// 对照组：正常大小、正常速度的响应必须完整拷贝成功，完全不受 watchdog 影响。
func TestForwardMediaRequestJSONNormalResponseIsUnaffected(t *testing.T) {
	// 空闲阈值刻意压到 300ms：正常响应一次性写完，Read 立刻拿到全部字节，
	// watchdog 没有触发的机会。
	setMediaBodyIdleTimeoutForTest(t, 300*time.Millisecond)

	const payload = `{"created":1713167890,"data":[{"b64_json":"aGk="}]}`
	upstream := newMediaStallUpstream(t, func(w http.ResponseWriter, r *http.Request, release <-chan struct{}) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(payload))
	})

	const body = `{"model":"image-model","prompt":"海獭"}`
	c, recorder := newMediaTestGinContext(t, "/v1/images/generations", body)
	cfg := mediaEndpointConfig{UpstreamPath: "/v1/images/generations"}

	statusCode, fwdErr := forwardMediaRequestJSON(
		c, cfg, dbmodel.Group{},
		&dbmodel.Channel{BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}}},
		"sk-test", []byte(body),
		"image-model", "image-model", false, context.Background(),
	)

	if fwdErr != nil {
		t.Fatalf("正常响应不应出错：%v", fwdErr)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want 200", statusCode)
	}
	if recorder.Body.String() != payload {
		t.Fatalf("响应体不完整：\n got=%q\nwant=%q", recorder.Body.String(), payload)
	}
	if recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("Content-Type 未透传：%q", recorder.Header().Get("Content-Type"))
	}
}

// 关键对照组：合法的大文件慢速下载（视频 / 音乐生成）不得被误杀。
// 上游每 30ms 滴一片、共 20 片，总时长 600ms 远超 300ms 的空闲阈值——
// 若实现退化成「总时长上限」，这个测试必然失败。
func TestForwardMediaRequestSlowTrickleIsNotKilled(t *testing.T) {
	setMediaBodyIdleTimeoutForTest(t, 300*time.Millisecond)

	const (
		chunkDelay = 30 * time.Millisecond
		chunks     = 20
	)
	var want strings.Builder
	pieces := make([]string, chunks)
	for i := range pieces {
		pieces[i] = fmt.Sprintf("chunk-%02d;", i)
		want.WriteString(pieces[i])
	}

	upstream := newMediaStallUpstream(t, func(w http.ResponseWriter, r *http.Request, release <-chan struct{}) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		for _, piece := range pieces {
			select {
			case <-release:
				return
			case <-time.After(chunkDelay):
			}
			if _, err := w.Write([]byte(piece)); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	})

	const body = `{"model":"video-model","prompt":"雪山延时"}`
	c, recorder := newMediaTestGinContext(t, "/v1/videos/generations", body)
	cfg := mediaEndpointConfig{UpstreamPath: "/v1/videos/generations", BinaryResponse: true}

	statusCode, fwdErr := forwardMediaRequestJSON(
		c, cfg, dbmodel.Group{},
		&dbmodel.Channel{BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}}},
		"sk-test", []byte(body),
		"video-model", "video-model", false, context.Background(),
	)

	if fwdErr != nil {
		t.Fatalf("缓慢但持续产出的上游被误杀（空闲超时退化成了总时长上限）：%v", fwdErr)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want 200", statusCode)
	}
	if recorder.Body.String() != want.String() {
		t.Fatalf("响应体不完整：\n got=%q\nwant=%q", recorder.Body.String(), want.String())
	}
}
