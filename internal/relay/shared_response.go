package relay

import (
	"bytes"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	dm "github.com/lingyuins/octopus/internal/model"
)

type sharedRelayResponse struct {
	body     []byte
	header   http.Header
	status   int
	outcome  *inflightRelayResult
	err      error
	overflow bool
}

type sharedRelayFlight struct {
	done   chan struct{}
	result *sharedRelayResponse
}

var sharedRelayFlights = struct {
	sync.Mutex
	entries map[string]*sharedRelayFlight
}{entries: make(map[string]*sharedRelayFlight)}

const maxSharedRelayBodyBytes = 8 << 20

func serveSharedRelay(req *relayRequest, group dm.Group, model, key string, keyRetries, routeRetries, cooldown, maxAttempts int) (bool, error) {
	sharedRelayFlights.Lock()
	flight, follower := sharedRelayFlights.entries[key]
	if !follower && len(sharedRelayFlights.entries) >= 256 {
		sharedRelayFlights.Unlock()
		return false, nil
	}
	if !follower {
		flight = &sharedRelayFlight{done: make(chan struct{})}
		sharedRelayFlights.entries[key] = flight
	}
	sharedRelayFlights.Unlock()
	if follower {
		select {
		case <-req.clientCtx.Done():
			return true, errClientDisconnected
		case <-flight.done:
		}
	} else {
		func() {
			defer func() {
				sharedRelayFlights.Lock()
				delete(sharedRelayFlights.entries, key)
				close(flight.done)
				sharedRelayFlights.Unlock()
			}()
			recorder := newSharedResponseRecorder(maxSharedRelayBodyBytes)
			isolated, _ := gin.CreateTestContext(recorder)
			isolated.Request = req.c.Request.Clone(req.operationCtx)
			for name, value := range req.c.Keys {
				isolated.Set(name, value)
			}
			copy := *req
			copy.c = isolated
			copy.clientCtx = req.operationCtx
			copy.streamSession = nil
			outcome, err := executeRelay(&copy, group, model, keyRetries, routeRetries, cooldown, maxAttempts)
			flight.result = &sharedRelayResponse{
				body:     recorder.Body(),
				header:   recorder.Header().Clone(),
				status:   recorder.Code(),
				outcome:  outcome,
				err:      err,
				overflow: recorder.Overflow(),
			}
		}()
	}
	result := flight.result
	if result == nil || result.overflow || len(bytes.TrimSpace(result.body)) == 0 {
		return false, nil
	}
	for name, values := range result.header {
		if name == "X-Ratelimit-Remaining" || name == "Retry-After" {
			continue
		}
		req.c.Writer.Header()[name] = append([]string(nil), values...)
	}
	req.c.Data(result.status, req.c.Writer.Header().Get("Content-Type"), result.body)
	if follower {
		if result.outcome != nil {
			req.metrics.SetInternalResponse(cloneInternalResponse(result.outcome.internalResp), result.outcome.actualModel)
		}
		req.metrics.Stats.InputCost, req.metrics.Stats.OutputCost = 0, 0
		req.metrics.Save(result.err == nil, result.err, nil)
	}
	return true, result.err
}

type sharedResponseRecorder struct {
	header   http.Header
	body     bytes.Buffer
	status   int
	limit    int
	overflow bool
}

func newSharedResponseRecorder(limit int) *sharedResponseRecorder {
	return &sharedResponseRecorder{header: make(http.Header), status: http.StatusOK, limit: max(0, limit)}
}

func (r *sharedResponseRecorder) Header() http.Header {
	return r.header
}

func (r *sharedResponseRecorder) WriteHeader(status int) {
	if r.status != http.StatusOK {
		return
	}
	r.status = status
}

func (r *sharedResponseRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if r.limit <= 0 {
		r.overflow = len(data) > 0
		return len(data), nil
	}
	remaining := r.limit - r.body.Len()
	if remaining > 0 {
		if len(data) <= remaining {
			_, _ = r.body.Write(data)
		} else {
			_, _ = r.body.Write(data[:remaining])
			r.overflow = true
		}
	} else if len(data) > 0 {
		r.overflow = true
	}
	return len(data), nil
}

func (r *sharedResponseRecorder) Flush() {
	if r.status == 0 {
		r.status = http.StatusOK
	}
}

func (r *sharedResponseRecorder) Body() []byte {
	return append([]byte(nil), r.body.Bytes()...)
}

func (r *sharedResponseRecorder) Code() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

func (r *sharedResponseRecorder) Overflow() bool {
	return r.overflow
}
