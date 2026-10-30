package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	extism "github.com/extism/go-sdk"
	"github.com/tetratelabs/wazero"
)

type instance struct{ plugin *extism.Plugin }

type generation struct {
	record         PluginRecord
	manifest       Manifest
	compiled       *extism.CompiledPlugin
	pool           chan *instance
	closeOnce      sync.Once
	refs           sync.WaitGroup
	refsMu         sync.Mutex
	refsDone       chan struct{}
	refsDoneClosed bool
}

func newGeneration(ctx context.Context, record PluginRecord, wasm []byte) (*generation, error) {
	m, err := ParseManifest([]byte(record.ManifestJSON))
	if err != nil {
		return nil, err
	}
	g := &generation{record: record, manifest: m}
	g.refsDone = make(chan struct{})
	var httpMax int64 = MaxPayloadBytes
	compiled, err := extism.NewCompiledPlugin(ctx, extism.Manifest{
		Wasm:    []extism.Wasm{extism.WasmData{Data: wasm}},
		Memory:  &extism.ManifestMemory{MaxPages: uint32(m.MemoryMiB * 16), MaxHttpResponseBytes: httpMax, MaxVarBytes: 64 << 10},
		Timeout: uint64(m.TimeoutMS),
	}, extism.PluginConfig{EnableWasi: true, RuntimeConfig: wazero.NewRuntimeConfig().WithCloseOnContextDone(true)}, g.hostFunctions())
	if err != nil {
		return nil, err
	}
	g.compiled = compiled
	g.pool = make(chan *instance, m.Instances)
	for range m.Instances {
		p, err := compiled.Instance(ctx, extism.PluginInstanceConfig{ModuleConfig: wazero.NewModuleConfig()})
		if err != nil {
			g.Close()
			return nil, err
		}
		g.pool <- &instance{plugin: p}
	}
	return g, nil
}

func (g *generation) acquire(ctx context.Context) (*instance, error) {
	select {
	case item := <-g.pool:
		return item, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (g *generation) release(item *instance, broken bool) {
	if !broken {
		g.pool <- item
		return
	}
	_ = item.plugin.Close(context.Background())
	p, err := g.compiled.Instance(context.Background(), extism.PluginInstanceConfig{ModuleConfig: wazero.NewModuleConfig()})
	if err == nil {
		g.pool <- &instance{plugin: p}
		return
	}
}

func (g *generation) beginRequest() bool {
	g.refsMu.Lock()
	defer g.refsMu.Unlock()
	if g.refsDoneClosed {
		return false
	}
	g.refs.Add(1)
	return true
}

func (g *generation) endRequest() { g.refs.Done() }

func (g *generation) call(ctx context.Context, in CallRequest) (CallResponse, error) {
	var out CallResponse
	item, err := g.acquire(ctx)
	if err != nil {
		return out, err
	}
	broken := false
	defer func() { g.release(item, broken) }()
	data, err := json.Marshal(in)
	if err != nil {
		return out, err
	}
	if len(data) > MaxPayloadBytes {
		return out, ErrPayloadTooLarge
	}
	_, result, err := item.plugin.CallWithContext(ctx, g.manifest.Entry, data)
	if err != nil {
		broken = true
		return out, err
	}
	if len(result) > MaxPayloadBytes {
		broken = true
		return out, ErrPayloadTooLarge
	}
	if err := json.Unmarshal(result, &out); err != nil {
		return out, fmt.Errorf("invalid plugin response: %w", err)
	}
	if out.APIVersion != 0 && out.APIVersion != HostAPIVersion {
		return out, fmt.Errorf("incompatible plugin response API")
	}
	if len(out.Payload) > 0 && !json.Valid(out.Payload) {
		return out, fmt.Errorf("invalid plugin response payload")
	}
	return out, validateDecision(out)
}

func (g *generation) Close() {
	g.closeOnce.Do(func() {
		close(g.pool)
		for item := range g.pool {
			if item.plugin != nil {
				_ = item.plugin.Close(context.Background())
			}
		}
		if g.compiled != nil {
			_ = g.compiled.Close(context.Background())
		}
	})
}

func (g *generation) Retire() {
	g.refsMu.Lock()
	if !g.refsDoneClosed {
		g.refsDoneClosed = true
		close(g.refsDone)
	}
	g.refsMu.Unlock()
	go func() { g.refs.Wait(); <-g.refsDone; g.Close() }()
}

func withTimeout(ctx context.Context, milliseconds int) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, time.Duration(milliseconds)*time.Millisecond)
}
