package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lingyuins/octopus/internal/helper"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op"
	ak "github.com/lingyuins/octopus/internal/op/apikey"
	"github.com/lingyuins/octopus/internal/op/channel"
	"github.com/lingyuins/octopus/internal/op/group"
	"github.com/lingyuins/octopus/internal/op/llm"
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
	market, err := op.ModelMarketGet(c.Request.Context(), price.GetLastUpdateTime())
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, market)
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
