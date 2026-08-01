package errorlog

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	internaldb "github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
)

// setupDB 初始化 SQLite 主库（错误日志存主库，不依赖独立日志库）。
func setupDB(t *testing.T) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "errorlog-test.db")
	if err := internaldb.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = internaldb.Close() })
}

func TestAddAndList(t *testing.T) {
	setupDB(t)
	ctx := context.Background()

	now := time.Now().Unix()
	if err := Add(ctx, model.ErrorLog{
		Source:        "backend",
		Level:         "panic",
		Message:       "boom: nil pointer",
		Stack:         "goroutine 1 ...",
		RequestMethod: "POST",
		RequestPath:   "/v1/chat/completions",
		ClientIP:      "127.0.0.1",
		Version:       "v2.5.1",
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := Add(ctx, model.ErrorLog{
		Source:  "frontend",
		Level:   "unhandledrejection",
		Message: "TypeError: Cannot read properties of undefined",
		Stack:   "at render (app.tsx:1:1)",
		PageURL: "/hub",
		RouteID: "hub",
	}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	// 全部
	all, err := List(ctx, Filter{}, 1, 20)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("List() len = %d, want 2", len(all))
	}
	// 按时间倒序：frontend 后写入应在最前
	if all[0].Source != "frontend" || all[1].Source != "backend" {
		t.Fatalf("List() order = %s, %s; want frontend, backend", all[0].Source, all[1].Source)
	}
	if all[0].ID == 0 || all[0].Time == 0 {
		t.Fatalf("List() entry missing id/time: %+v", all[0])
	}

	// 按 source 过滤
	frontend, err := List(ctx, Filter{Source: "frontend"}, 1, 20)
	if err != nil {
		t.Fatalf("List(source=frontend) error = %v", err)
	}
	if len(frontend) != 1 || frontend[0].Message != "TypeError: Cannot read properties of undefined" {
		t.Fatalf("List(source=frontend) = %+v, want 1 entry", frontend)
	}

	// 时间范围过滤
	start := now - 1
	end := now + 1
	window, err := List(ctx, Filter{StartTime: &start, EndTime: &end}, 1, 20)
	if err != nil {
		t.Fatalf("List(time window) error = %v", err)
	}
	if len(window) != 2 {
		t.Fatalf("List(time window) len = %d, want 2", len(window))
	}

	// 分页：page_size=1 时第一页 1 条
	page1, err := List(ctx, Filter{}, 1, 1)
	if err != nil {
		t.Fatalf("List(page=1,size=1) error = %v", err)
	}
	if len(page1) != 1 {
		t.Fatalf("List(page=1,size=1) len = %d, want 1", len(page1))
	}
}

func TestGetByIDAndClear(t *testing.T) {
	setupDB(t)
	ctx := context.Background()

	if err := Add(ctx, model.ErrorLog{Source: "backend", Level: "error", Message: "test"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	all, err := List(ctx, Filter{}, 1, 20)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("List() len = %d, want 1", len(all))
	}
	id := all[0].ID

	got, err := GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.Message != "test" || got.Source != "backend" {
		t.Fatalf("GetByID() = %+v", got)
	}

	if err := Clear(ctx); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	after, err := List(ctx, Filter{}, 1, 20)
	if err != nil {
		t.Fatalf("List() after clear error = %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("List() after clear len = %d, want 0", len(after))
	}
}
