package relay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// attempt_time_out 到期掐断流式响应时，错误必须报出真实原因（attempt 超时），
// 而不是伪装成上游提前断流（io.ErrUnexpectedEOF）。
//
// 生产现场：分组配了 20s 转发超时，推理模型的长回答被自身超时掐断，日志却记成
// "upstream stream ended before completion: unexpected EOF"，把排障引向协议转换/
// 上游稳定性。这里用 io.Pipe 复刻「上游发过一段内容后挂住，attempt 到期后传输层
// 关闭响应体」的现场。
func TestStreamAttemptTimeoutReportsTimeoutNotUpstreamEOF(t *testing.T) {
	clientCtx, cancelClient := context.WithCancel(context.Background())
	t.Cleanup(cancelClient)

	ra, recorder := newStreamTestAttempt(t, clientCtx)
	ra.attemptTimeOutSec = 1

	ctx, cancelAttempt := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancelAttempt)

	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close() })

	go func() {
		// 上游先正常发一段可见内容（无 finish_reason），随后挂住等 attempt 超时。
		_, _ = writer.Write([]byte("data: partial\n\n"))
		<-ctx.Done()
		// 到期后传输层关闭响应体：等价于 net/http 在请求上下文取消后收尾。
		_ = writer.Close()
	}()

	done := make(chan error, 1)
	go func() {
		done <- ra.handleStreamResponse(ctx, &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       reader,
		})
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("attempt timeout must surface an error")
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("attempt timeout misreported as upstream EOF: %v", err)
		}
		if !strings.Contains(err.Error(), "attempt timeout") {
			t.Fatalf("error should name the attempt timeout, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handleStreamResponse did not return after the attempt deadline")
	}

	// 已写出的部分内容保持原样，且不得伪造终止帧；错误事件应把真实原因带给客户端。
	body := recorder.Body.String()
	if !strings.Contains(body, "partial") {
		t.Fatalf("partial content lost: %q", body)
	}
	if strings.Contains(body, "data: [DONE]") {
		t.Fatalf("timed-out stream must not forge a [DONE] terminal: %q", body)
	}
	if !strings.Contains(body, "attempt timeout") {
		t.Fatalf("client-facing error event should carry the timeout reason: %q", body)
	}
}

// 对照组：ctx 未到期时，上游提前断流仍必须报 io.ErrUnexpectedEOF——
// 修复不得把真实的上游断流也归因成超时。
func TestStreamUpstreamEOFWithoutDeadlineStillReportsEOF(t *testing.T) {
	clientCtx, cancelClient := context.WithCancel(context.Background())
	t.Cleanup(cancelClient)

	ra, _ := newStreamTestAttempt(t, clientCtx)
	ra.attemptTimeOutSec = 1 // 配置了转发超时，但远未到期

	ctx, cancelAttempt := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancelAttempt)

	reader, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close() })

	go func() {
		_, _ = writer.Write([]byte("data: partial\n\n"))
		_ = writer.Close()
	}()

	done := make(chan error, 1)
	go func() {
		done <- ra.handleStreamResponse(ctx, &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       reader,
		})
	}()

	select {
	case err := <-done:
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("upstream EOF must stay io.ErrUnexpectedEOF, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handleStreamResponse did not return after upstream EOF")
	}
}
