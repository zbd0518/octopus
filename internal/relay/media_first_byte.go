package relay

import (
	"context"
	"io"
	"sync"
	"time"
)

type mediaFirstByteKey struct{}

type mediaFirstByteTimeout struct {
	mu     sync.Mutex
	timer  *time.Timer
	cancel context.CancelFunc
	fired  bool
	seen   bool
}

func (guard *mediaFirstByteTimeout) stop() {
	guard.mu.Lock()
	guard.seen = true
	if guard.timer != nil {
		guard.timer.Stop()
	}
	guard.mu.Unlock()
}

func (guard *mediaFirstByteTimeout) expire() {
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if !guard.seen {
		guard.fired = true
		guard.cancel()
	}
}

type mediaFirstByteBody struct {
	io.ReadCloser
	guard *mediaFirstByteTimeout
}

func (body *mediaFirstByteBody) Read(data []byte) (int, error) {
	n, err := body.ReadCloser.Read(data)
	if n > 0 {
		body.guard.stop()
	}
	return n, err
}

func watchMediaFirstByte(ctx context.Context, body io.ReadCloser) io.ReadCloser {
	guard, _ := ctx.Value(mediaFirstByteKey{}).(*mediaFirstByteTimeout)
	if guard == nil {
		return body
	}
	return &mediaFirstByteBody{ReadCloser: body, guard: guard}
}
