package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	pluginevent "github.com/lingyuins/octopus/internal/plugin/event"
	"github.com/lingyuins/octopus/internal/utils/log"
	"gorm.io/gorm"
)

type PluginRecord = model.Plugin

type Manager struct {
	root      string
	active    atomic.Pointer[[]*generation]
	retiredMu sync.Mutex
	retired   []*generation
	lifecycle sync.Mutex
	queue     chan queuedEvent
	queueCtx  context.Context
	queueStop context.CancelFunc
	queueWG   sync.WaitGroup
	seen      sync.Map
}

type queuedEvent struct {
	ID, Event string
	Payload   json.RawMessage
}

var DefaultManager *Manager

func NewManager(root string) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{root: root, queue: make(chan queuedEvent, 256), queueCtx: ctx, queueStop: cancel}
}

func Init(root string) (*Manager, error) {
	m := NewManager(root)
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	if err := m.Load(context.Background()); err != nil {
		m.Close()
		return nil, err
	}
	m.queueWG.Add(1)
	go m.eventLoop()
	DefaultManager = m
	pluginevent.SetPublisher(m.Publish)
	return m, nil
}

func (m *Manager) Close() {
	m.queueStop()
	m.queueWG.Wait()
	if current := m.active.Swap(nil); current != nil {
		for _, g := range *current {
			g.Retire()
		}
	}
	m.retiredMu.Lock()
	old := m.retired
	m.retired = nil
	m.retiredMu.Unlock()
	for _, g := range old {
		g.Close()
	}
}

func (m *Manager) Load(ctx context.Context) error {
	records, err := ListRecords(ctx)
	if err != nil {
		return err
	}
	var next []*generation
	for _, record := range records {
		if !record.Enabled || record.Status != model.PluginStatusEnabled {
			continue
		}
		g, err := m.loadGeneration(ctx, record)
		if err != nil {
			m.setError(record.ID, err)
			continue
		}
		next = append(next, g)
	}
	m.swap(next)
	return nil
}

func (m *Manager) loadGeneration(ctx context.Context, record PluginRecord) (*generation, error) {
	dir, err := PluginDir(m.root, record.ID)
	if err != nil {
		return nil, err
	}
	wasm, err := os.ReadFile(filepath.Join(dir, "plugin.wasm"))
	if err != nil {
		m.markNeedsReinstall(record.ID, err)
		return nil, fmt.Errorf("plugin package is unavailable")
	}
	hash := sha256.Sum256(wasm)
	if !strings.EqualFold(hex.EncodeToString(hash[:]), record.SHA256) {
		m.markNeedsReinstall(record.ID, fmt.Errorf("checksum mismatch"))
		return nil, fmt.Errorf("plugin checksum mismatch")
	}
	g, err := newGeneration(ctx, record, wasm)
	if err != nil {
		return nil, fmt.Errorf("compile plugin: %w", err)
	}
	return g, nil
}

func (m *Manager) swap(next []*generation) {
	sort.Slice(next, func(i, j int) bool {
		if next[i].manifest.Priority != next[j].manifest.Priority {
			return next[i].manifest.Priority < next[j].manifest.Priority
		}
		return next[i].manifest.ID < next[j].manifest.ID
	})
	retained := make(map[*generation]struct{}, len(next))
	for _, g := range next {
		retained[g] = struct{}{}
	}
	old := m.active.Swap(&next)
	var removed []*generation
	if old != nil {
		for _, g := range *old {
			if _, ok := retained[g]; !ok {
				removed = append(removed, g)
			}
		}
	}
	m.retiredMu.Lock()
	m.retired = append(m.retired, removed...)
	m.retiredMu.Unlock()
	for _, g := range removed {
		g.Retire()
	}
}

func (m *Manager) Install(ctx context.Context, r io.Reader) (PluginRecord, error) {
	pkg, err := ReadPackage(r)
	if err != nil {
		return PluginRecord{}, err
	}
	return m.InstallPackage(ctx, pkg)
}

func (m *Manager) InstallPackage(ctx context.Context, pkg *Package) (PluginRecord, error) {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	existing, err := Record(ctx, pkg.Manifest.ID)
	exists := err == nil
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return PluginRecord{}, err
	}
	if exists && existing.Status == model.PluginStatusUninstalling {
		return PluginRecord{}, fmt.Errorf("plugin uninstall requires a restart")
	}
	if exists && CompareVersions(pkg.Manifest.Version, existing.Version) < 0 {
		return PluginRecord{}, fmt.Errorf("plugin downgrade is not allowed")
	}
	dir, err := PluginDir(m.root, pkg.Manifest.ID)
	if err != nil {
		return PluginRecord{}, err
	}
	staging := dir + ".installing-" + uuid.NewString()
	if err := os.MkdirAll(staging, 0700); err != nil {
		return PluginRecord{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := os.WriteFile(filepath.Join(staging, "plugin.wasm"), pkg.WASM, 0600); err != nil {
		return PluginRecord{}, err
	}
	if err := os.WriteFile(filepath.Join(staging, "manifest.json"), pkg.ManifestRaw, 0600); err != nil {
		return PluginRecord{}, err
	}
	if len(pkg.Assets) > 0 {
		if err := os.MkdirAll(filepath.Join(staging, "ui"), 0700); err != nil {
			return PluginRecord{}, err
		}
		for name, content := range pkg.Assets {
			if !safePackagePath(name) {
				return PluginRecord{}, fmt.Errorf("unsafe plugin asset path")
			}
			target := filepath.Join(staging, "ui", filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return PluginRecord{}, err
			}
			if err := os.WriteFile(target, content, 0600); err != nil {
				return PluginRecord{}, err
			}
		}
	}
	record := model.Plugin{ID: pkg.Manifest.ID, Name: pkg.Manifest.Name, Version: pkg.Manifest.Version, ManifestJSON: string(pkg.ManifestRaw), SHA256: WASMDigest(pkg.WASM), Status: model.PluginStatusDisabled, SchemaVersion: pkg.Manifest.SchemaVersion}
	if exists {
		record.ConfigJSON, record.Enabled, record.CreatedAt = existing.ConfigJSON, existing.Enabled, existing.CreatedAt
		if existing.SchemaVersion > pkg.Manifest.SchemaVersion {
			return PluginRecord{}, fmt.Errorf("plugin schema downgrade is not allowed")
		}
	}
	if record.Enabled {
		record.Status = model.PluginStatusEnabled
	}
	if pkg.Manifest.Handles("plugin.migrate") && record.Enabled {
		err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			g, err := newGeneration(ctx, record, pkg.WASM)
			if err != nil {
				return err
			}
			defer g.Close()
			callCtx, cancel := withTimeout(ctx, pkg.Manifest.TimeoutMS)
			defer cancel()
			_, err = g.call(callCtx, CallRequest{APIVersion: HostAPIVersion, RequestID: uuid.NewString(), PluginID: record.ID, Event: "plugin.migrate", Payload: json.RawMessage(fmt.Sprintf(`{"from":%d,"to":%d}`, existing.SchemaVersion, pkg.Manifest.SchemaVersion))})
			if err != nil {
				return fmt.Errorf("plugin migration failed; previous version retained: %w", err)
			}
			return tx.Save(&record).Error
		})
		if err != nil {
			return PluginRecord{}, err
		}
	} else if err := db.GetDB().WithContext(ctx).Save(&record).Error; err != nil {
		return PluginRecord{}, err
	}
	backup := dir + ".previous-" + uuid.NewString()
	if _, err := os.Stat(dir); err == nil {
		if err := os.Rename(dir, backup); err != nil {
			return PluginRecord{}, err
		}
	}
	if err := os.Rename(staging, dir); err != nil {
		return PluginRecord{}, err
	}
	committed = true
	_ = os.RemoveAll(backup)
	if record.Enabled {
		g, err := m.loadGeneration(ctx, record)
		if err != nil {
			m.setError(record.ID, err)
			return record, fmt.Errorf("plugin installed but could not be activated: %w", err)
		}
		m.replace(g)
	} else {
		m.remove(record.ID)
	}
	return record, nil
}

func (m *Manager) replace(g *generation) {
	var next []*generation
	if current := m.active.Load(); current != nil {
		for _, item := range *current {
			if item.manifest.ID != g.manifest.ID {
				next = append(next, item)
			}
		}
	}
	m.swap(append(next, g))
}

func (m *Manager) remove(id string) {
	var next []*generation
	if current := m.active.Load(); current != nil {
		for _, item := range *current {
			if item.manifest.ID != id {
				next = append(next, item)
			}
		}
	}
	m.swap(next)
}

func (m *Manager) SetEnabled(ctx context.Context, id string, enabled bool) (PluginRecord, error) {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	record, err := Record(ctx, id)
	if err != nil {
		return record, err
	}
	if record.Status == model.PluginStatusUninstalling {
		return record, fmt.Errorf("plugin uninstall requires a restart")
	}
	record.Enabled = enabled
	record.LastError = ""
	if enabled {
		g, err := m.loadGeneration(ctx, record)
		if err != nil {
			m.setError(id, err)
			return Record(ctx, id)
		}
		record.Status = model.PluginStatusEnabled
		if err := db.GetDB().WithContext(ctx).Save(&record).Error; err != nil {
			g.Close()
			return record, err
		}
		m.replace(g)
	} else {
		record.Status = model.PluginStatusDisabled
		if err := db.GetDB().WithContext(ctx).Save(&record).Error; err != nil {
			return record, err
		}
		m.remove(id)
	}
	return record, nil
}

func (m *Manager) Uninstall(ctx context.Context, id string, deleteData bool) error {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	record, err := Record(ctx, id)
	if err != nil {
		return err
	}
	if !deleteData {
		if err := db.GetDB().WithContext(ctx).Model(&model.Plugin{}).Where("id = ?", id).Updates(map[string]any{"enabled": false, "status": model.PluginStatusUninstalling}).Error; err != nil {
			return err
		}
	}
	m.remove(id)
	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if deleteData {
			for _, table := range []any{&model.PluginKV{}, &model.PluginSecret{}} {
				if err := tx.Delete(table, "plugin_id = ?", id).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Delete(&model.Plugin{}, "id = ?", id).Error; err != nil {
			return err
		}
		return tx.Create(&model.Plugin{ID: id, Name: record.Name, Version: record.Version, ManifestJSON: record.ManifestJSON, ConfigJSON: record.ConfigJSON, SHA256: record.SHA256, Status: model.PluginStatusUninstalling, UninstallAfter: true}).Error
	})
}

func (m *Manager) FinalizeUninstalls(ctx context.Context) error {
	var records []model.Plugin
	if err := db.GetDB().WithContext(ctx).Where("status = ?", model.PluginStatusUninstalling).Find(&records).Error; err != nil {
		return err
	}
	for _, record := range records {
		dir, err := PluginDir(m.root, record.ID)
		if err != nil {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		if err := db.GetDB().WithContext(ctx).Delete(&model.Plugin{}, "id = ? AND status = ?", record.ID, model.PluginStatusUninstalling).Error; err != nil {
			return err
		}
	}
	return nil
}

// Handles reports whether the current active snapshot subscribes to event.
// A later invocation may observe a newer snapshot after a lifecycle change.
func (m *Manager) Handles(event string) bool {
	if m == nil {
		return false
	}
	return handlesEvent(m.active.Load(), event)
}

func handlesEvent(current *[]*generation, event string) bool {
	if current != nil {
		for _, g := range *current {
			if g.manifest.Handles(event) {
				return true
			}
		}
	}
	return false
}

func (m *Manager) Invoke(ctx context.Context, event, attemptID string, payload json.RawMessage) (json.RawMessage, error) {
	current := m.active.Load()
	if !handlesEvent(current, event) {
		return payload, nil
	}
	if len(payload) > MaxPayloadBytes {
		return nil, ErrPayloadTooLarge
	}
	result := payload
	for _, g := range *current {
		if !g.manifest.Handles(event) {
			continue
		}
		if !g.beginRequest() {
			continue
		}
		err := func() error {
			defer g.endRequest()
			config, configErr := Config(ctx, g.manifest.ID)
			if configErr != nil {
				return configErr
			}
			callCtx, cancel := withTimeout(ctx, g.manifest.TimeoutMS)
			defer cancel()
			response, err := g.call(callCtx, CallRequest{APIVersion: HostAPIVersion, RequestID: newRequestID(g.manifest.ID, attemptID), PluginID: g.manifest.ID, Event: event, AttemptID: attemptID, Payload: result, Config: config})
			m.recordCall(g.manifest.ID, err != nil, errors.Is(err, context.DeadlineExceeded))
			if err != nil {
				return err
			}
			if response.Error != "" {
				return errors.New(response.Error)
			}
			if len(response.Payload) > 0 {
				result = response.Payload
			}
			return nil
		}()
		if err != nil {
			hostLogPluginError(g.manifest.ID, err)
			if g.manifest.Required {
				return nil, &RequiredError{ID: g.manifest.ID, Err: err}
			}
		}
	}
	return result, nil
}

func (m *Manager) RPC(ctx context.Context, id string, payload json.RawMessage) (json.RawMessage, error) {
	current := m.active.Load()
	if current == nil {
		return nil, fmt.Errorf("plugin is not active")
	}
	for _, g := range *current {
		if g.manifest.ID != id {
			continue
		}
		if !g.beginRequest() {
			return nil, fmt.Errorf("plugin is not active")
		}
		defer g.endRequest()
		if !g.manifest.Handles("http.rpc") {
			return nil, fmt.Errorf("plugin does not expose RPC")
		}
		config, err := Config(ctx, id)
		if err != nil {
			return nil, err
		}
		callCtx, cancel := withTimeout(ctx, g.manifest.TimeoutMS)
		defer cancel()
		response, err := g.call(callCtx, CallRequest{APIVersion: HostAPIVersion, RequestID: uuid.NewString(), PluginID: id, Event: "http.rpc", Payload: payload, Config: config})
		if err != nil {
			return nil, err
		}
		if response.Error != "" {
			return nil, errors.New(response.Error)
		}
		return response.Payload, nil
	}
	return nil, fmt.Errorf("plugin is not active")
}

func (m *Manager) Publish(event string, payload any) bool {
	data, err := json.Marshal(payload)
	if err != nil || len(data) > MaxPayloadBytes {
		return false
	}
	item := queuedEvent{ID: uuid.NewString(), Event: event, Payload: data}
	if _, loaded := m.seen.LoadOrStore(event+":"+item.ID, time.Now()); loaded {
		return true
	}
	select {
	case m.queue <- item:
		return true
	default:
		log.Warnf("plugin event queue full, dropping %s", event)
		return false
	}
}

func (m *Manager) eventLoop() {
	defer m.queueWG.Done()
	for {
		select {
		case <-m.queueCtx.Done():
			return
		case item := <-m.queue:
			for attempt := 0; attempt < 3; attempt++ {
				ctx, cancel := context.WithTimeout(m.queueCtx, 3*time.Second)
				_, err := m.Invoke(ctx, item.Event, item.ID, item.Payload)
				cancel()
				if err == nil {
					break
				}
				select {
				case <-m.queueCtx.Done():
					return
				case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
				}
			}
			m.seen.Delete(item.Event + ":" + item.ID)
		}
	}
}

func (m *Manager) recordCall(id string, failed, timeout bool) {
	updates := map[string]any{"calls": gorm.Expr("calls + 1")}
	if failed {
		updates["failures"] = gorm.Expr("failures + 1")
	}
	if timeout {
		updates["timeout_count"] = gorm.Expr("timeout_count + 1")
	}
	if err := db.GetDB().Model(&model.Plugin{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		log.Warnf("plugin metrics: %v", err)
	}
}

func (m *Manager) setError(id string, err error) {
	hostLogPluginError(id, err)
	_ = db.GetDB().Model(&model.Plugin{}).Where("id = ?", id).Updates(map[string]any{"status": model.PluginStatusError, "enabled": false, "last_error": err.Error()}).Error
}

func (m *Manager) markNeedsReinstall(id string, err error) {
	_ = db.GetDB().Model(&model.Plugin{}).Where("id = ?", id).Updates(map[string]any{"status": model.PluginStatusNeedsReinstall, "enabled": false, "last_error": err.Error()}).Error
}
