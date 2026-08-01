package planprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
)

const testMiMoCookie = "api-platform_serviceToken=token; userId=user-1; api-platform_slh=slh; api-platform_ph=ph"

func withMiMoPlanServers(t *testing.T) (usageRequests *int, detailRequests *int) {
	t.Helper()

	usageRequests = new(int)
	detailRequests = new(int)

	usageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*usageRequests++
		if r.Method != http.MethodGet {
			t.Errorf("usage method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("Cookie"); got != testMiMoCookie {
			t.Errorf("usage Cookie = %q, want %q", got, testMiMoCookie)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": map[string]any{
				"usage": map[string]any{
					"items": []map[string]any{
						{"name": "plan_total_token", "used": 1000, "limit": 10000},
						{"name": "compensation_total_token", "used": 200, "limit": 3000},
					},
				},
				"monthUsage": map[string]any{
					"items": []map[string]any{
						{"name": "month_total_token", "used": 100, "limit": 1000},
						{"name": "month_extra_token", "used": 25, "limit": 500},
					},
				},
			},
		})
	}))
	detailServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*detailRequests++
		if r.Method != http.MethodGet {
			t.Errorf("detail method = %s, want GET", r.Method)
		}
		if got := r.Header.Get("Cookie"); got != testMiMoCookie {
			t.Errorf("detail Cookie = %q, want %q", got, testMiMoCookie)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    0,
			"message": "ok",
			"data": map[string]any{
				"planCode":         "mimo-pro",
				"planName":         "MiMo Pro",
				"currentPeriodEnd": "2026-08-09 10:11:12",
			},
		})
	}))

	origUsageURL := mimoPlanUsageURL
	origDetailURL := mimoPlanDetailURL
	mimoPlanUsageURL = usageServer.URL
	mimoPlanDetailURL = detailServer.URL
	t.Cleanup(func() {
		mimoPlanUsageURL = origUsageURL
		mimoPlanDetailURL = origDetailURL
		usageServer.Close()
		detailServer.Close()
	})
	return usageRequests, detailRequests
}

func setupPlanProviderDB(t *testing.T) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "planprovider.db")
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
}

func TestQueryMiMoPlanTokenPlanAggregatesAllUsageItems(t *testing.T) {
	usageRequests, detailRequests := withMiMoPlanServers(t)

	result, err := queryMiMoPlanTokenPlan(context.Background(), testMiMoCookie)
	if err != nil {
		t.Fatalf("queryMiMoPlanTokenPlan() error = %v", err)
	}

	if *usageRequests != 1 {
		t.Fatalf("usage requests = %d, want 1", *usageRequests)
	}
	if *detailRequests != 1 {
		t.Fatalf("detail requests = %d, want 1", *detailRequests)
	}
	if result.QuotaTotal != 13000 {
		t.Fatalf("QuotaTotal = %v, want 13000", result.QuotaTotal)
	}
	if result.QuotaUsed != 1200 {
		t.Fatalf("QuotaUsed = %v, want 1200", result.QuotaUsed)
	}
	if result.WeeklyTotal != 1500 {
		t.Fatalf("WeeklyTotal = %v, want 1500", result.WeeklyTotal)
	}
	if result.WeeklyUsed != 125 {
		t.Fatalf("WeeklyUsed = %v, want 125", result.WeeklyUsed)
	}
	wantReset := time.Date(2026, 8, 9, 10, 11, 12, 0, time.Local)
	if result.QuotaResetAt == nil || !result.QuotaResetAt.Equal(wantReset) {
		t.Fatalf("QuotaResetAt = %v, want %v", result.QuotaResetAt, wantReset)
	}
	if len(result.Models) != 1 || result.Models[0].QuotaTotal != 13000 || result.Models[0].QuotaUsed != 1200 {
		t.Fatalf("Models = %+v, want aggregate total/used", result.Models)
	}
}

func TestAddProviderMiMoIgnoresForwardAPIKey(t *testing.T) {
	setupPlanProviderDB(t)
	withMiMoPlanServers(t)

	provider, err := AddProvider(context.Background(), model.PlanProviderMiMoPlan, testMiMoCookie, "sk-should-not-be-saved", "MiMo monitor", 0, model.ProxyUsageModeDirect, nil, "", "", "", "")
	if err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}

	if provider.ChannelID != 0 {
		t.Fatalf("ChannelID = %d, want 0", provider.ChannelID)
	}
	if provider.ForwardAPIKey != "" {
		t.Fatalf("ForwardAPIKey = %q, want empty", provider.ForwardAPIKey)
	}

	var stored model.PlanProvider
	if err := db.GetDB().First(&stored, provider.ID).Error; err != nil {
		t.Fatalf("load stored provider: %v", err)
	}
	if stored.ForwardAPIKey != "" {
		t.Fatalf("stored ForwardAPIKey = %q, want empty", stored.ForwardAPIKey)
	}
}

func TestListProvidersMasksPlanProviderSecrets(t *testing.T) {
	setupPlanProviderDB(t)
	withMiMoPlanServers(t)

	provider, err := AddProvider(context.Background(), model.PlanProviderMiMoPlan, testMiMoCookie, "", "MiMo monitor", 0, model.ProxyUsageModeDirect, nil, "", "", "", "")
	if err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}
	provider.ForwardAPIKey = "sk-legacy-secret"
	if err := db.GetDB().Save(provider).Error; err != nil {
		t.Fatalf("save provider with legacy secret: %v", err)
	}

	items, err := ListProviders(context.Background(), model.PlanProviderTypeTokenPlan)
	if err != nil {
		t.Fatalf("ListProviders() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if items[0].APIKey != "" {
		t.Fatalf("listed APIKey = %q, want empty", items[0].APIKey)
	}
	if items[0].ForwardAPIKey != "" {
		t.Fatalf("listed ForwardAPIKey = %q, want empty", items[0].ForwardAPIKey)
	}
}
