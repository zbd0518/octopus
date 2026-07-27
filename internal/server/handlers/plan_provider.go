package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op"
	"github.com/lingyuins/octopus/internal/planprovider"
	"github.com/lingyuins/octopus/internal/server/auth"
	"github.com/lingyuins/octopus/internal/server/middleware"
	"github.com/lingyuins/octopus/internal/server/resp"
	"github.com/lingyuins/octopus/internal/server/router"
)

func init() {
	// 注入代理池 URL 解析器（planprovider 与 op 双向依赖，无法由 op 注入，在此注入）。
	planprovider.ProxyURLByConfigFunc = op.ProxyURLForConfig

	// 额度管理路由 (读)
	router.NewGroupRouter("/api/v1/plan-provider").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermSitesRead)).
		AddRoute(router.NewRoute("/balance/list", http.MethodGet).Handle(listBalanceProviders)).
		AddRoute(router.NewRoute("/tokenplan/list", http.MethodGet).Handle(listTokenPlanProviders)).
		AddRoute(router.NewRoute("/balance/categories", http.MethodGet).Handle(getBalanceCategories)).
		AddRoute(router.NewRoute("/tokenplan/categories", http.MethodGet).Handle(getTokenPlanCategories))

	// 额度管理路由 (写)
	router.NewGroupRouter("/api/v1/plan-provider").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermSitesWrite)).
		AddRoute(router.NewRoute("/add", http.MethodPost).Handle(addPlanProvider)).
		AddRoute(router.NewRoute("/refresh/:id", http.MethodPost).Handle(refreshPlanProvider)).
		AddRoute(router.NewRoute("/:id", http.MethodDelete).Handle(deletePlanProvider))
}

func listBalanceProviders(c *gin.Context) {
	providers, err := planprovider.ListProviders(c.Request.Context(), model.PlanProviderTypeBalance)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, providers)
}

func listTokenPlanProviders(c *gin.Context) {
	providers, err := planprovider.ListProviders(c.Request.Context(), model.PlanProviderTypeTokenPlan)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, providers)
}

func getBalanceCategories(c *gin.Context) {
	categories := planprovider.GetCategories(model.PlanProviderTypeBalance)
	resp.Success(c, categories)
}

func getTokenPlanCategories(c *gin.Context) {
	categories := planprovider.GetCategories(model.PlanProviderTypeTokenPlan)
	resp.Success(c, categories)
}

type addPlanProviderRequest struct {
	Category      model.PlanProviderCategory `json:"category" binding:"required"`
	APIKey        string                     `json:"api_key"`
	ForwardAPIKey string                     `json:"forward_api_key,omitempty"`
	Name          string                     `json:"name"`
	// 代理配置：目前仅 Codex 类生效（chatgpt.com 国内不可直连）。
	ProxyMode     model.ProxyUsageMode `json:"proxy_mode,omitempty"`
	ProxyConfigID *int                 `json:"proxy_config_id,omitempty"`
}

func addPlanProvider(c *gin.Context) {
	var req addPlanProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}

	provider, err := planprovider.AddProvider(c.Request.Context(), req.Category, req.APIKey, req.ForwardAPIKey, req.Name, req.ProxyMode, req.ProxyConfigID)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, provider)
}

func refreshPlanProvider(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	provider, err := planprovider.RefreshProvider(c.Request.Context(), id)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, provider)
}

func deletePlanProvider(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	if err := planprovider.DeleteProvider(c.Request.Context(), id); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, nil)
}
