package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lingyuins/octopus/internal/helper"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op"
	ak "github.com/lingyuins/octopus/internal/op/apikey"
	"github.com/lingyuins/octopus/internal/op/channel"
	"github.com/lingyuins/octopus/internal/op/group"
	"github.com/lingyuins/octopus/internal/op/llm"
	"github.com/lingyuins/octopus/internal/op/modelnormalize"
	"github.com/lingyuins/octopus/internal/op/modelnormalizeai"
	"github.com/lingyuins/octopus/internal/price"
	"github.com/lingyuins/octopus/internal/server/auth"
	"github.com/lingyuins/octopus/internal/server/middleware"
	"github.com/lingyuins/octopus/internal/server/resp"
	"github.com/lingyuins/octopus/internal/server/router"
	"github.com/samber/lo"
)

func init() {
	router.NewGroupRouter("/api/v1/model").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermSettingsRead)).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listLLM),
		).
		AddRoute(
			router.NewRoute("/create", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermSettingsWrite)).
				Handle(createLLM),
		).
		AddRoute(
			router.NewRoute("/channel", http.MethodGet).
				Handle(listLLMByChannel),
		).
		AddRoute(
			router.NewRoute("/market", http.MethodGet).
				Handle(getModelMarket),
		).
		AddRoute(
			router.NewRoute("/normalize/rules", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermSettingsWrite)).
				Handle(saveModelNormalizeRules),
		).
		AddRoute(
			router.NewRoute("/normalize/generate", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermSettingsWrite)).
				Handle(startModelNormalize),
		).
		AddRoute(
			router.NewRoute("/normalize/progress/:id", http.MethodGet).
				Handle(getModelNormalizeProgress),
		).
		AddRoute(
			router.NewRoute("/normalize/stream/:id", http.MethodGet).
				Handle(streamModelNormalizeProgress),
		).
		AddRoute(
			router.NewRoute("/normalize/coverage", http.MethodGet).
				Handle(getModelNormalizeCoverage),
		).
		AddRoute(
			router.NewRoute("/update", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermSettingsWrite)).
				Handle(updateLLM),
		).
		AddRoute(
			router.NewRoute("/delete", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermSettingsWrite)).
				Handle(deleteLLM),
		).
		AddRoute(
			router.NewRoute("/update-price", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermSettingsWrite)).
				Handle(updateLLMPrice),
		).
		AddRoute(
			router.NewRoute("/last-update-time", http.MethodGet).
				Handle(getLastUpdateTime),
		).
		AddRoute(
			router.NewRoute("/capabilities", http.MethodGet).
				Handle(getModelCapabilities),
		).
		AddRoute(
			router.NewRoute("/price-category/list", http.MethodGet).
				Handle(listPriceCategories),
		).
		AddRoute(
			router.NewRoute("/price-category/create", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermSettingsWrite)).
				Handle(createPriceCategory),
		).
		AddRoute(
			router.NewRoute("/price-category/update", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermSettingsWrite)).
				Handle(updatePriceCategory),
		).
		AddRoute(
			router.NewRoute("/price-category/delete", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermSettingsWrite)).
				Handle(deletePriceCategory),
		).
		AddRoute(
			router.NewRoute("/price-schedule/list", http.MethodGet).
				Handle(listPriceSchedules),
		).
		AddRoute(
			router.NewRoute("/price-schedule/create", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermSettingsWrite)).
				Handle(createPriceSchedule),
		).
		AddRoute(
			router.NewRoute("/price-schedule/update", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermSettingsWrite)).
				Handle(updatePriceSchedule),
		).
		AddRoute(
			router.NewRoute("/price-schedule/delete", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermSettingsWrite)).
				Handle(deletePriceSchedule),
		)
	router.NewGroupRouter("/v1").
		Use(middleware.APIKeyAuth()).
		AddRoute(
			router.NewRoute("/models", http.MethodGet).
				Handle(getModelList),
		)
}

func getModelList(c *gin.Context) {
	endpoint := strings.TrimSpace(c.Query("endpoint"))
	if endpoint != "" && !model.IsSupportedEndpointType(endpoint) {
		resp.Error(c, http.StatusBadRequest, "invalid endpoint")
		return
	}
	// /v1/models 不传 endpoint 时返回所有类型的模型（relay 查询语义，非 group 分类）。
	if endpoint == "" {
		endpoint = model.EndpointTypeAll
	}

	models, err := group.GroupListModelByEndpoint(endpoint, c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	apiKeyId := c.GetInt("api_key_id")
	apiKey, err := ak.Get(apiKeyId, c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	if strings.TrimSpace(apiKey.AllowedGroupCategories) != "" {
		visible := make(map[string]bool)
		groups, listErr := group.GroupList(c.Request.Context())
		if listErr != nil {
			resp.InternalError(c)
			return
		}
		for _, candidate := range groups {
			for _, allowed := range strings.Split(apiKey.AllowedGroupCategories, ",") {
				if strings.EqualFold(strings.TrimSpace(allowed), strings.TrimSpace(candidate.Category)) && strings.TrimSpace(candidate.Category) != "" {
					visible[candidate.Name] = true
				}
			}
		}
		models = lo.Filter(models, func(name string, _ int) bool { return visible[name] })
	}
	if apiKey.SupportedModels != "" {
		supportedModels := lo.Map(strings.Split(apiKey.SupportedModels, ","), func(s string, _ int) string {
			return strings.TrimSpace(s)
		})
		models = lo.Filter(models, func(m string, _ int) bool {
			return lo.Contains(supportedModels, m)
		})
	}

	if c.GetString("request_type") == "anthropic" {
		var anthropicModels []model.AnthropicModel
		for _, m := range models {
			anthropicModels = append(anthropicModels, model.AnthropicModel{
				ID:          m,
				CreatedAt:   "2024-01-01T00:00:00Z",
				DisplayName: m,
				Type:        "model",
			})
		}
		response := gin.H{
			"data":     anthropicModels,
			"has_more": false,
		}
		if len(anthropicModels) > 0 {
			response["first_id"] = anthropicModels[0].ID
			response["last_id"] = anthropicModels[len(anthropicModels)-1].ID
		}
		c.JSON(200, response)
	} else {
		var openAIModels []model.OpenAIModel
		for _, m := range models {
			openAIModels = append(openAIModels, model.OpenAIModel{
				ID:      m,
				Object:  "model",
				Created: 1763395200,
				OwnedBy: "octopus",
			})
		}
		c.JSON(200, model.OpenAIModelList{
			Object: "list",
			Data:   openAIModels,
		})
	}
}

func listLLM(c *gin.Context) {
	models, err := llm.List(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	// 只读填充峰谷计费标识（不入库），供前端模型列表展示徽章。
	// 规则驱动：命中启用的峰谷计费规则即标注，不再依赖内置白名单。
	for i := range models {
		if llm.PriceScheduleMatch(models[i].Name) != nil {
			models[i].BillingSchedule = "deepseek_v4"
		}
	}
	resp.Success(c, models)
}

func listLLMByChannel(c *gin.Context) {
	channels, err := channel.LLMList(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, channels)
}

func getModelMarket(c *gin.Context) {
	var (
		market model.ModelMarketResponse
		err    error
	)
	if raw := strings.TrimSpace(c.Query("dedupe")); raw != "" {
		dedupe, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			resp.ErrorWithKey(c, http.StatusBadRequest, "invalid dedupe", "errors.invalidParameter", nil)
			return
		}
		market, err = op.ModelMarketGetWithDedupe(c.Request.Context(), price.GetLastUpdateTime(), dedupe)
	} else {
		market, err = op.ModelMarketGet(c.Request.Context(), price.GetLastUpdateTime())
	}
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, market)
}

func saveModelNormalizeRules(c *gin.Context) {
	var req struct {
		RouterPrefixes      []string                         `json:"router_prefixes"`
		FunctionalSuffixes  []string                         `json:"functional_suffixes"`
		ExplicitMappings    []modelnormalize.ExplicitMapping `json:"explicit_mappings"`
		MarketDedupeDefault bool                             `json:"market_dedupe_default"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := modelnormalize.SaveRuleSet(c.Request.Context(), modelnormalize.RuleSet{
		RouterPrefixes:      req.RouterPrefixes,
		FunctionalSuffixes:  req.FunctionalSuffixes,
		ExplicitMappings:    req.ExplicitMappings,
		MarketDedupeDefault: req.MarketDedupeDefault,
	}); err != nil {
		resp.ErrorWithKey(c, http.StatusBadRequest, err.Error(), "errors.inputValidationFailed", nil)
		return
	}
	op.ModelMarketInvalidateCache()
	resp.Success(c, nil)
}

func getModelNormalizeCoverage(c *gin.Context) {
	channels, err := channel.LLMList(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	names := make([]string, 0, len(channels))
	for _, item := range channels {
		names = append(names, item.Name)
	}
	report := modelnormalize.AnalyzeNames(names, modelnormalize.CurrentRules())
	resp.Success(c, report)
}

func startModelNormalize(c *gin.Context) {
	task, err := modelnormalizeai.Start(c.Request.Context())
	if err != nil {
		resp.ErrorWithKey(c, http.StatusBadGateway, err.Error(), "errors.upstreamServiceUnavailable", nil)
		return
	}
	resp.Success(c, task)
}

func getModelNormalizeProgress(c *gin.Context) {
	task, err := modelnormalizeai.Get(c.Request.Context(), strings.TrimSpace(c.Param("id")))
	if err != nil {
		resp.ErrorWithKey(c, http.StatusNotFound, "model normalization task not found", "errors.resourceNotFound", nil)
		return
	}
	resp.Success(c, task)
}

func streamModelNormalizeProgress(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	updates, unsubscribe := modelnormalizeai.Subscribe(id)
	defer unsubscribe()
	task, err := modelnormalizeai.Get(c.Request.Context(), id)
	if err != nil {
		resp.ErrorWithKey(c, http.StatusNotFound, "model normalization task not found", "errors.resourceNotFound", nil)
		return
	}

	// This endpoint intentionally uses SSE rather than the management envelope:
	// it is a streaming transport for progress events.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.SSEvent("progress", task)
	c.Writer.Flush()
	if task.Done {
		return
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			c.SSEvent("progress", update)
			if update.Done {
				return
			}
		case <-heartbeat.C:
			c.SSEvent("heartbeat", gin.H{"id": id})
		}
		c.Writer.Flush()
	}
}

func createLLM(c *gin.Context) {
	var model model.LLMInfo
	if err := c.ShouldBindJSON(&model); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := llm.Create(model, c.Request.Context()); err != nil {
		resp.InternalError(c)
		return
	}
	op.ModelMarketInvalidateCache()
	resp.Success(c, model)
}

func updateLLM(c *gin.Context) {
	var model model.LLMInfo
	if err := c.ShouldBindJSON(&model); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := llm.Update(model, c.Request.Context()); err != nil {
		resp.InternalError(c)
		return
	}
	op.ModelMarketInvalidateCache()
	resp.Success(c, model)
}

func deleteLLM(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := llm.Delete(req.Name, c.Request.Context()); err != nil {
		resp.InternalError(c)
		return
	}
	op.ModelMarketInvalidateCache()
	resp.Success(c, nil)
}

func updateLLMPrice(c *gin.Context) {
	err := price.UpdateLLMPrice(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	if err := helper.LLMPriceRefreshExistingModels(c.Request.Context()); err != nil {
		resp.InternalError(c)
		return
	}
	op.ModelMarketInvalidateCache()
	resp.Success(c, nil)
}

func getLastUpdateTime(c *gin.Context) {
	time := price.GetLastUpdateTime()
	resp.Success(c, time)
}

func getModelCapabilities(c *gin.Context) {
	caps, err := group.GroupListModelCapabilities(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, caps)
}

func listPriceCategories(c *gin.Context) {
	rows, err := llm.ListPriceCategories(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, rows)
}

func createPriceCategory(c *gin.Context) {
	var cat model.ModelPriceCategory
	if err := c.ShouldBindJSON(&cat); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	created, err := llm.CreatePriceCategory(cat, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, created)
}

func updatePriceCategory(c *gin.Context) {
	var cat model.ModelPriceCategory
	if err := c.ShouldBindJSON(&cat); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := llm.UpdatePriceCategory(cat, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, updated)
}

func deletePriceCategory(c *gin.Context) {
	var req struct {
		ID uint `json:"id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := llm.DeletePriceCategory(req.ID, c.Request.Context()); err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func listPriceSchedules(c *gin.Context) {
	rows, err := llm.ListPriceSchedules(c.Request.Context())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, rows)
}

func createPriceSchedule(c *gin.Context) {
	var s model.ModelPriceSchedule
	if err := c.ShouldBindJSON(&s); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	created, err := llm.CreatePriceSchedule(s, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, created)
}

func updatePriceSchedule(c *gin.Context) {
	var s model.ModelPriceSchedule
	if err := c.ShouldBindJSON(&s); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := llm.UpdatePriceSchedule(s, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, updated)
}

func deletePriceSchedule(c *gin.Context) {
	var req struct {
		ID uint `json:"id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := llm.DeletePriceSchedule(req.ID, c.Request.Context()); err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}
