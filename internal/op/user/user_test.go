package user

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
)

// initMemDB initializes an in-memory SQLite database for the user package tests.
// Each test gets an isolated DB via a unique DSN derived from the test name.
func initMemDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
}

// resetAdminCache simulates a cold start (container restart): the in-memory
// adminCache is empty and has not yet been loaded from the DB, so Ready()
// returns false on the next Init() call.
func resetAdminCache(t *testing.T) {
	t.Helper()
	old := GetAdminCache()
	SetCache(model.User{})
	t.Cleanup(func() { SetCache(old) })
}

// TestBootstrapFromEnvIdempotentWhenAdminExists reproduces issue #198 part two:
// OCTOPUS_INITIAL_ADMIN_USERNAME/PASSWORD are set and the DB already has an admin
// (every restart after the first run). Previously this crashed with
// "user init error: bootstrap admin from env: initial admin account is already
// set up" and caused the Docker container to loop on restart. Init() must now
// treat this as a benign no-op.
func TestBootstrapFromEnvIdempotentWhenAdminExists(t *testing.T) {
	initMemDB(t)

	// Seed an admin as if a previous first-run bootstrap had succeeded.
	if err := BootstrapCreate("admin", "super-secret-123"); err != nil {
		t.Fatalf("seed initial admin: %v", err)
	}

	// Emulate the env the operator set once for first-run bootstrap.
	t.Setenv("OCTOPUS_INITIAL_ADMIN_USERNAME", "admin")
	t.Setenv("OCTOPUS_INITIAL_ADMIN_PASSWORD", "super-secret-123")

	// Cold start: cache empty, simulating a fresh container restart.
	resetAdminCache(t)

	// This used to return ErrBootstrapAlreadySetUp and crash the process.
	if err := Init(); err != nil {
		t.Fatalf("Init() after admin exists: %v", err)
	}

	// The existing admin must not be overwritten or duplicated by the env hint.
	var count int64
	if err := db.GetDB().Model(&model.User{}).Count(&count).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Fatalf("user count = %d, want 1 (existing admin must be preserved)", count)
	}

	var loaded model.User
	if err := db.GetDB().First(&loaded).Error; err != nil {
		t.Fatalf("load admin: %v", err)
	}
	if loaded.Username != "admin" {
		t.Fatalf("admin username = %q, want admin", loaded.Username)
	}
}

// TestBootstrapFromEnvCreatesWhenDBEmpty ensures the env bootstrap path still
// creates the admin on a fresh DB (the original first-run use case), so the
// idempotency change did not break first-run initialization.
func TestBootstrapFromEnvCreatesWhenDBEmpty(t *testing.T) {
	initMemDB(t)

	t.Setenv("OCTOPUS_INITIAL_ADMIN_USERNAME", "root")
	t.Setenv("OCTOPUS_INITIAL_ADMIN_PASSWORD", "super-secret-123")
	resetAdminCache(t)

	if err := Init(); err != nil {
		t.Fatalf("Init() on empty DB with env: %v", err)
	}

	if !Ready() {
		t.Fatal("Ready() = false, want true after env bootstrap created admin")
	}
	if got := GetAdminCache(); got.Username != "root" {
		t.Fatalf("admin username = %q, want root", got.Username)
	}

	var count int64
	if err := db.GetDB().Model(&model.User{}).Count(&count).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Fatalf("user count = %d, want 1", count)
	}
}

// TestBootstrapFromEnvRejectsPartialEnv guards the "both must be set together"
// rule: setting only one of OCTOPUS_INITIAL_ADMIN_USERNAME / PASSWORD must
// still error, not silently no-op.
func TestBootstrapFromEnvRejectsPartialEnv(t *testing.T) {
	initMemDB(t)

	t.Setenv("OCTOPUS_INITIAL_ADMIN_USERNAME", "root")
	// password intentionally unset
	resetAdminCache(t)

	err := Init()
	if err == nil {
		t.Fatal("Init() with only username set: error = nil, want error")
	}
	if !strings.Contains(err.Error(), "must be set together") {
		t.Fatalf("Init() error = %q, want 'must be set together'", err.Error())
	}
}
