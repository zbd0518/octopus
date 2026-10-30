package relay

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestMediaFirstByteStopsDeadlineAfterData(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	guard := &mediaFirstByteTimeout{cancel: cancel}
	body := watchMediaFirstByte(context.WithValue(ctx, mediaFirstByteKey{}, guard), io.NopCloser(strings.NewReader("hello")))
	data := make([]byte, 1)
	if n, err := body.Read(data); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	guard.expire()
	if ctx.Err() != nil {
		t.Fatal("first byte did not disarm timeout")
	}
}

func TestMediaFirstByteCancelsStalledAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	guard := &mediaFirstByteTimeout{cancel: cancel}
	guard.mu.Lock()
	guard.timer = time.AfterFunc(time.Millisecond, guard.expire)
	guard.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("first byte timeout did not cancel")
	}
	guard.stop()
}
