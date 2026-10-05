package middleware

import (
	"net/http"
	"testing"
)

// TestShouldAuditManagementWrite_PoolAccountExportRoute locks in the audit
// coverage of the pool account export route (B2-#2 step 7, audit-2 P0-1/P2-3).
//
// Regression context: export was originally registered as GET. Adding a GET
// entry to auditedManagementWriteRoutes has no effect at all, because
// isPotentialAuditRequest short-circuits every non-POST/PUT/PATCH/DELETE
// method before the whitelist is consulted — the route was silently unaudited.
// Export is therefore a POST now; this test fails if the whitelist entry is
// dropped, or if the route is reverted to a non-auditable method.
func TestShouldAuditManagementWrite_PoolAccountExportRoute(t *testing.T) {
	const exportFullPath = "/api/v1/pool/:id/account/export"

	if !ShouldAuditManagementWrite(http.MethodPost, exportFullPath) {
		t.Fatalf("POST %s must be covered by the audit whitelist", exportFullPath)
	}

	// The GET form must stay unauditable: isPotentialAuditRequest rejects
	// every non-writing method before the whitelist lookup. If this ever
	// returns true, the short-circuit was removed and read endpoints would
	// start flooding the audit log.
	if isPotentialAuditRequest(http.MethodGet, "/api/v1/pool/1/account/export") {
		t.Fatalf("GET requests must be short-circuited by isPotentialAuditRequest before the whitelist lookup")
	}
	if ShouldAuditManagementWrite(http.MethodGet, exportFullPath) {
		t.Fatalf("GET must never match the audit whitelist")
	}
}

// TestShouldAuditManagementWrite_PoolScheduledTestRoutes locks in the audit
// coverage of the pool scheduled-test write routes (B4-#11). All three are
// POST/DELETE, so plain whitelist entries are sufficient (the method filter
// passes them through to the list lookup, unlike the GET export case).
func TestShouldAuditManagementWrite_PoolScheduledTestRoutes(t *testing.T) {
	fullPaths := []string{
		"/api/v1/pool/:id/scheduled-test/create",
		"/api/v1/pool/:id/scheduled-test/update/:tid",
		"/api/v1/pool/:id/scheduled-test/delete/:tid",
	}
	for _, fullPath := range fullPaths {
		method := http.MethodPost
		if fullPath == "/api/v1/pool/:id/scheduled-test/delete/:tid" {
			method = http.MethodDelete
		}
		if !ShouldAuditManagementWrite(method, fullPath) {
			t.Fatalf("%s %s must be covered by the audit whitelist", method, fullPath)
		}
	}
	// The read routes must stay out of the audit whitelist.
	if ShouldAuditManagementWrite(http.MethodGet, "/api/v1/pool/:id/scheduled-test/list") {
		t.Fatalf("GET list route must not be audited")
	}
	if ShouldAuditManagementWrite(http.MethodGet, "/api/v1/pool/:id/scheduled-test/results/:tid") {
		t.Fatalf("GET results route must not be audited")
	}
}

// TestShouldAuditManagementWrite_PoolUnschedRuleRoutes locks in the audit
// coverage of the temp-unsched rule CRUD write routes (B4-#12).
func TestShouldAuditManagementWrite_PoolUnschedRuleRoutes(t *testing.T) {
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/pool/unsched-rules/create"},
		{http.MethodPost, "/api/v1/pool/unsched-rules/update/:id"},
		{http.MethodDelete, "/api/v1/pool/unsched-rules/delete/:id"},
	}
	for _, tc := range cases {
		if !ShouldAuditManagementWrite(tc.method, tc.path) {
			t.Fatalf("%s %s must be covered by the audit whitelist", tc.method, tc.path)
		}
	}
	if ShouldAuditManagementWrite(http.MethodGet, "/api/v1/pool/unsched-rules/list") {
		t.Fatalf("GET list route must not be audited")
	}
}

// TestShouldAuditManagementWrite_PoolRestoreProxyRoute locks in the audit
// coverage of the B4-#13 proxy-fallback restore endpoint.
func TestShouldAuditManagementWrite_PoolRestoreProxyRoute(t *testing.T) {
	const fullPath = "/api/v1/pool/:id/account/restore-proxy/:aid"
	if !ShouldAuditManagementWrite(http.MethodPost, fullPath) {
		t.Fatalf("POST %s must be covered by the audit whitelist", fullPath)
	}
}
