package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op"
)

func setupPriceCategoryTest(t *testing.T) func() {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := "file:" + strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace("pcat"+t.Name()) + "?mode=memory&cache=shared"
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	return func() { _ = db.Close() }
}

func doPriceCatReq(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, path, &buf)
	c.Request = c.Request.WithContext(t.Context())

	switch path {
	case "/api/v1/model/price-category/list":
		listPriceCategories(c)
	case "/api/v1/model/price-category/create":
		createPriceCategory(c)
	case "/api/v1/model/price-category/update":
		updatePriceCategory(c)
	case "/api/v1/model/price-category/delete":
		deletePriceCategory(c)
	default:
		t.Fatalf("unknown path %q", path)
	}
	return rec
}

func TestPriceCategoryHandlers(t *testing.T) {
	cleanup := setupPriceCategoryTest(t)
	defer cleanup()

	// create
	create := model.ModelPriceCategory{
		Name:      "test-chat",
		RuleType:  string(model.ModelPriceCategoryRulePrefix),
		RuleValue: "my-",
		LLMPrice:  model.LLMPrice{Input: 1, Output: 2},
		SortOrder: 3,
		Enabled:   true,
	}
	rec := doPriceCatReq(t, http.MethodPost, "/api/v1/model/price-category/create", create)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var createdRes struct {
		Data model.ModelPriceCategory `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createdRes); err != nil {
		t.Fatalf("unmarshal create resp: %v", err)
	}
	created := createdRes.Data
	if created.ID == 0 {
		t.Fatalf("created category id = 0")
	}

	// list
	rec = doPriceCatReq(t, http.MethodGet, "/api/v1/model/price-category/list", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var listed struct {
		Data []model.ModelPriceCategory `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(listed.Data) != 1 || listed.Data[0].Name != "test-chat" {
		t.Fatalf("list = %+v, want 1 test-chat", listed.Data)
	}

	// update enable -> disabled
	created.Enabled = false
	created.Input = 5
	rec = doPriceCatReq(t, http.MethodPost, "/api/v1/model/price-category/update", created)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var updatedRes struct {
		Data model.ModelPriceCategory `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &updatedRes); err != nil {
		t.Fatalf("unmarshal update: %v", err)
	}
	updated := updatedRes.Data
	if updated.Enabled || updated.Input != 5 {
		t.Fatalf("update not applied: %+v", updated)
	}

	// delete
	rec = doPriceCatReq(t, http.MethodPost, "/api/v1/model/price-category/delete", map[string]uint{"id": created.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body=%s", rec.Code, rec.Body.String())
	}

	rec = doPriceCatReq(t, http.MethodGet, "/api/v1/model/price-category/list", nil)
	json.Unmarshal(rec.Body.Bytes(), &listed)
	if len(listed.Data) != 0 {
		t.Fatalf("after delete list = %d entries, want 0", len(listed.Data))
	}
}

func TestPriceCategoryCreateValidation(t *testing.T) {
	cleanup := setupPriceCategoryTest(t)
	defer cleanup()

	bad := model.ModelPriceCategory{
		Name:      "bad",
		RuleType:  "invalid-rule",
		RuleValue: "x",
		LLMPrice:  model.LLMPrice{Input: 1, Output: 1},
	}
	rec := doPriceCatReq(t, http.MethodPost, "/api/v1/model/price-category/create", bad)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid rule_type status = %d, want 400", rec.Code)
	}
}
