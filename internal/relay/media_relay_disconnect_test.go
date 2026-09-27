package relay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	dbmodel "github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/relay/balancer"
	"github.com/lingyuins/octopus/internal/utils/xurl"
)

// errBrokenPipe 模拟客户端已断开时的写失败（EPIPE）。
var errBrokenPipe = errors.New("write: broken pipe")

// failingMediaWriter 包装 gin.ResponseWriter，让写入在中途失败。
//
// httptest.ResponseRecorder 的写永远成功，无法复刻媒体路径「向客户端回写时
// 连接已断」的现场。媒体侧四处 io.Copy（二进制 / SSE / JSON / TTS）失败时会把
// statusCode 置 0 并返回错误，这正是缺陷2 的触发点。
//
// written 可配置，用于覆盖媒体路径写失败的两种归类结果：
//   - written=true  → ClassifyRelayError 走 ScopeAbortAll（type.go 的 written 分支）
//   - written=false → 走 classifyNonHTTPError，EPIPE 被判为网络错误 → ScopeNextChannel
//
// 两种归类在未修复代码里都会记熔断，后者还会额外触发无谓的换渠道重试。
type failingMediaWriter struct {
	gin.ResponseWriter

	mu        sync.Mutex
	written   bool
	onWrite   func() // 在返回错误前调用，用于确定性地标记「客户端此刻已断开」
	writeErrs int
}

func (w *failingMediaWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.written = true
	cb := w.onWrite
	w.writeErrs++
	w.mu.Unlock()
	if cb != nil {
		cb()
	}
	return 0, errBrokenPipe
}

func (w *failingMediaWriter) Written() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.written
}

// newMediaTestContext 构造一个带可失败 writer 的 gin 测试上下文。
func newMediaTestContext(t *testing.T, clientCtx context.Context, onWrite func()) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech",
		strings.NewReader(`{"model":"tts-model","input":"hello"}`)).WithContext(clientCtx)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Writer = &failingMediaWriter{ResponseWriter: c.Writer, onWrite: onWrite}
	return c, recorder
}

// 缺陷2 主回归：媒体路径向客户端回写失败 + 客户端已断开 → 必须豁免熔断计数。
//
// media_relay.go 原本完全没有断连豁免（errClientDisconnected / context.Canceled
// 在该文件 grep 0 命中），写失败被归类成上游故障：既记熔断，又在
// ScopeNextChannel 分支上换渠道重试一个客户端早已不再等待的请求。
//
// onWrite 回调在写失败的同一时刻取消 clientCtx，确定性地复刻「客户端在我方
// 回写时刚好断开」，避免与真实网络时序竞争。
func TestMediaRelayWriteFailureWithGoneClientSkipsAccounting(t *testing.T) {
	xurl.SetSSRFAllowPrivateForTest(true)
	t.Cleanup(func() { xurl.SetSSRFAllowPrivateForTest(false) })

	// 媒体路径下客户端断连的两条真实现场。两者在未修复代码里都会记熔断，
	// 后者还额外触发一次无谓的换渠道重试（客户端早已不在等）。
	//
	// 注意 written 的取值不可自由组合：gin 的 responseWriter 在任何 Write
	// 尝试时就会把 written 置 true（WriteHeaderNow），failingMediaWriter 原样
	// 复刻了这个语义。所以「写失败」必然是 written=true；written=false 只会在
	// 根本没向客户端写过字节就失败时出现，即上游连接层错误。
	for _, tt := range []struct {
		name      string
		binary    bool // 上游是否返回二进制（走 handleBinaryResponse）
		writeFail bool // 是否复刻「回写客户端时失败」
		wantScope RetryScope
	}{
		{
			name:      "binary write failure aborts all",
			binary:    true,
			writeFail: true,
			wantScope: ScopeAbortAll,
		},
		{
			name:      "json write failure aborts all",
			binary:    false,
			writeFail: true,
			wantScope: ScopeAbortAll,
		},
		{
			// 上游连接层失败（一个已关闭的端口）：从未写过客户端字节，
			// written=false 且错误文本被 isNetworkError 命中 → ScopeNextChannel。
			name:      "upstream connection failure switches channel",
			binary:    false,
			writeFail: false,
			wantScope: ScopeNextChannel,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			clientCtx, cancelClient := context.WithCancel(context.Background())
			t.Cleanup(cancelClient)

			var wroteOnce bool
			c, _ := newMediaTestContext(t, clientCtx, func() {
				// 第一次写失败即视为客户端断开（生产里 EPIPE 正是这个含义）。
				if !wroteOnce {
					wroteOnce = true
					cancelClient()
				}
			})
			if tt.writeFail {
				// 写失败分支：先让 header 写入成功，再让 body 写入失败。
				c.Writer.WriteHeader(http.StatusOK)
				if w, ok := c.Writer.(*failingMediaWriter); ok {
					w.mu.Lock()
					w.written = true
					w.mu.Unlock()
				}
			}

			// 上游地址：writeFail=false 时指向一个已关闭的服务器，制造连接层错误。
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.binary {
					w.Header().Set("Content-Type", "audio/mpeg")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("partial-audio-bytes"))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"created":1,"data":[{"url":"https://cdn.example.com/x"}]}`))
			}))
			upstreamURL := upstream.URL
			if !tt.writeFail {
				upstream.Close() // 端口已关闭 → dial 阶段失败，不会写任何客户端字节
				cancelClient()   // 客户端在同一时刻断开
			} else {
				defer upstream.Close()
			}

			cfg := mediaEndpointConfig{UpstreamPath: "/v1/audio/speech", BinaryResponse: tt.binary}
			group := dbmodel.Group{EndpointProvider: "raw"}

			statusCode, fwdErr := forwardMediaRequestJSON(
				c, cfg, group,
				&dbmodel.Channel{BaseUrls: []dbmodel.BaseUrl{{URL: upstreamURL}}},
				"sk-test",
				[]byte(`{"model":"tts-model","input":"hello"}`),
				"tts-model", "tts-model", false,
				context.Background(), // operationCtx 与 clientCtx 解耦（context.go），正是缺陷2 的根因
			)

			if fwdErr == nil {
				t.Fatal("forwardMediaRequestJSON() err = nil, want a write failure")
			}
			if statusCode != 0 {
				t.Fatalf("statusCode = %d, want 0 (媒体侧写失败会把状态码置 0)", statusCode)
			}
			// 前提断言：客户端确实已断开。这是豁免的唯一判据，
			// 且必须查 c.Request.Context()（operationCtx 基于 context.Background()）。
			if c.Request.Context().Err() == nil {
				t.Fatal("c.Request.Context().Err() = nil, want non-nil; client must be gone for this scenario")
			}

			// 复刻 media_relay.go:279-280 的归一化顺序：先分类，再按客户端状态豁免。
			//
			// 覆盖范围（诚实声明）：本用例真正跑的是生产函数
			// forwardMediaRequestJSON（它确实会因写失败把 statusCode 置 0），
			// 以及真实的 ClassifyRelayError；但豁免调用本身是测试代码自己调的，
			// 因为 MediaHandler 的候选循环需要完整 DB 渠道/key/Iterator。
			// 「生产代码确实调了它且传的是 c.Request.Context()」由源码文本守卫
			// TestMediaRelayGoHasDisconnectExemption 校验，该守卫已被实测证伪：
			// 注释掉 media_relay.go 的 markClientCancelIfGone 调用后它立即 FAIL。
			written := c.Writer.Written()
			decision := ClassifyRelayError(statusCode, fwdErr, written)

			// 对照组（必要）：不调 markClientCancelIfGone 时，本次失败必须会被计入
			// 熔断器。少了这一对，下面的断言无法区分「豁免生效」与
			// 「该 Scope 本来就不计数」——即无法证伪缺陷 2 本身。
			unmarked := decision
			if !shouldRecordChannelFailure(0, unmarked) {
				t.Fatalf("未豁免时必须计入渠道失败，否则本场景不构成缺陷 2；decision = %+v", unmarked)
			}

			markClientCancelIfGone(&decision, c.Request.Context(), fwdErr)
			if !decision.SkipFailureAccounting {
				t.Fatalf("SkipFailureAccounting = false, want true; decision = %+v", decision)
			}
			if shouldRecordChannelFailure(0, decision) {
				t.Fatalf("shouldRecordChannelFailure() = true for a disconnected client; decision = %+v", decision)
			}
			// Scope 必须保持分类原值：豁免只改「是否计数」，不改「是否重试」。
			if decision.Scope != tt.wantScope {
				t.Fatalf("Scope = %v, want %v (豁免不得改变重试语义); decision = %+v",
					decision.Scope, tt.wantScope, decision)
			}
		})
	}
}

// 对照组：媒体路径写失败但客户端仍在连接 —— 这是真实的上游/网络故障，
// 必须照旧计入熔断，否则豁免会退化成「媒体路径永不熔断」。
func TestMediaRelayWriteFailureWithLiveClientStillCounts(t *testing.T) {
	xurl.SetSSRFAllowPrivateForTest(true)
	t.Cleanup(func() { xurl.SetSSRFAllowPrivateForTest(false) })

	clientCtx, cancelClient := context.WithCancel(context.Background())
	t.Cleanup(cancelClient) // 全程不取消：客户端仍在连接

	c, _ := newMediaTestContext(t, clientCtx, nil)
	c.Writer.WriteHeader(http.StatusOK)
	if w, ok := c.Writer.(*failingMediaWriter); ok {
		w.mu.Lock()
		w.written = true
		w.mu.Unlock()
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial-audio-bytes"))
	}))
	defer upstream.Close()

	cfg := mediaEndpointConfig{UpstreamPath: "/v1/audio/speech", BinaryResponse: true}
	statusCode, fwdErr := forwardMediaRequestJSON(
		c, cfg, dbmodel.Group{EndpointProvider: "raw"},
		&dbmodel.Channel{BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}}},
		"sk-test",
		[]byte(`{"model":"tts-model","input":"hello"}`),
		"tts-model", "tts-model", false,
		context.Background(),
	)

	if fwdErr == nil {
		t.Fatal("forwardMediaRequestJSON() err = nil, want a write failure")
	}
	if statusCode != 0 {
		t.Fatalf("statusCode = %d, want 0", statusCode)
	}
	if c.Request.Context().Err() != nil {
		t.Fatalf("client context err = %v, want nil (client must still be connected)", c.Request.Context().Err())
	}

	written := c.Writer.Written()
	decision := ClassifyRelayError(statusCode, fwdErr, written)
	markClientCancelIfGone(&decision, c.Request.Context(), fwdErr)

	if decision.SkipFailureAccounting {
		t.Fatalf("SkipFailureAccounting = true, want false while the client is still connected; decision = %+v", decision)
	}
	if !shouldRecordChannelFailure(0, decision) {
		t.Fatalf("shouldRecordChannelFailure() = false, want true for a genuine media write failure; decision = %+v", decision)
	}
}

// 端到端熔断计数（媒体侧）：反复的客户端断连不得熔断健康渠道，
// 对照组（客户端在线）必须在默认阈值 5 处熔断。
//
// 覆盖范围（诚实声明）：这里走的是真实的 balancer 全局熔断器（RecordFailure /
// RecordAutoFailure / IsTripped）与真实的 ClassifyRelayError，并用 modelName
// 做隔离；但豁免调用仍由测试自己发出，同上一个用例。
// 日志里的 `Closed -> Open` 告警均来自对照组（客户端在线），属于预期行为。
func TestMediaRelayClientDisconnectsDoNotTripCircuitBreaker(t *testing.T) {
	const (
		channelID = 904411
		keyID     = 904412
	)
	modelName := fmt.Sprintf("media-cb-%s", t.Name())

	balancer.RemoveChannelEntries(channelID)
	t.Cleanup(func() {
		balancer.RemoveChannelEntries(channelID)
		balancer.RemoveChannelStats(channelID)
		balancer.RemoveChannelKeyAvailability(channelID)
	})

	goneCtx := newCanceledContext(t)
	liveCtx := newLiveContext(t)
	mediaErr := fmt.Errorf("failed to stream binary response: %w", errBrokenPipe)

	// 媒体写失败的两种归类都要覆盖。
	for _, written := range []bool{true, false} {
		label := "written=true(ScopeAbortAll)"
		if !written {
			label = "written=false(ScopeNextChannel)"
		}
		t.Run(label, func(t *testing.T) {
			balancer.RemoveChannelEntries(channelID)
			balancer.RemoveChannelStats(channelID)

			for i := 0; i < 12; i++ {
				decision := ClassifyRelayError(0, mediaErr, written)
				markClientCancelIfGone(&decision, goneCtx, mediaErr)
				if shouldRecordChannelFailure(0, decision) {
					balancer.RecordFailure(channelID, keyID, modelName)
					balancer.RecordAutoFailure(channelID, modelName)
				}
				if tripped, _ := balancer.IsTripped(channelID, keyID, modelName); tripped {
					t.Fatalf("tripped after %d client disconnects; decision = %+v", i+1, decision)
				}
			}

			// 对照组：客户端在线，同样的写失败必须熔断。
			balancer.RemoveChannelEntries(channelID)
			balancer.RemoveChannelStats(channelID)
			var trippedAt int
			for i := 0; i < 12; i++ {
				decision := ClassifyRelayError(0, mediaErr, written)
				markClientCancelIfGone(&decision, liveCtx, mediaErr)
				if shouldRecordChannelFailure(0, decision) {
					balancer.RecordFailure(channelID, keyID, modelName)
					balancer.RecordAutoFailure(channelID, modelName)
				}
				if tripped, _ := balancer.IsTripped(channelID, keyID, modelName); tripped {
					trippedAt = i + 1
					break
				}
			}
			if trippedAt == 0 || trippedAt > 5 {
				t.Fatalf("control group trippedAt = %d, want 1..5 (default threshold); "+
					"豁免不得吞掉真实的媒体写失败", trippedAt)
			}
		})
	}
}
