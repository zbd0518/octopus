package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/pool"
	"github.com/lingyuins/octopus/internal/server/auth"
	"github.com/lingyuins/octopus/internal/server/middleware"
	"github.com/lingyuins/octopus/internal/server/resp"
	"github.com/lingyuins/octopus/internal/server/router"
)

func init() {
	router.NewGroupRouter("/api/v1/pool").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermChannelsRead)).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listPools),
		).
		AddRoute(
			router.NewRoute("/create", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(createPool),
		).
		AddRoute(
			router.NewRoute("/update", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(updatePool),
		).
		AddRoute(
			router.NewRoute("/delete/:id", http.MethodDelete).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(deletePool),
		).
		AddRoute(
			router.NewRoute("/:id/account/list", http.MethodGet).
				Handle(listPoolAccounts),
		).
		AddRoute(
			router.NewRoute("/:id/account/create", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(createPoolAccount),
		).
		AddRoute(
			router.NewRoute("/:id/account/update/:aid", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(updatePoolAccount),
		).
		AddRoute(
			router.NewRoute("/:id/account/delete/:aid", http.MethodDelete).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(deletePoolAccount),
		)
}

func listPools(c *gin.Context) {
	pools, err := pool.ListPools()
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, pools)
}

func createPool(c *gin.Context) {
	var req model.AccountPool
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if err := pool.CreatePool(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, req)
}

func updatePool(c *gin.Context) {
	var req struct {
		ID                 int    `json:"id" binding:"required"`
		Name               string `json:"name"`
		Description        string `json:"description"`
		Strategy           string `json:"strategy"`
		DefaultConcurrency *int   `json:"default_concurrency"`
		CooldownBaseSec    *int   `json:"cooldown_base_sec"`
		Enabled            *bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Description != "" {
		updates["description"] = req.Description
	}
	if req.Strategy != "" {
		updates["strategy"] = req.Strategy
	}
	if req.DefaultConcurrency != nil {
		updates["default_concurrency"] = *req.DefaultConcurrency
	}
	if req.CooldownBaseSec != nil {
		updates["cooldown_base_sec"] = *req.CooldownBaseSec
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if err := pool.UpdatePool(req.ID, updates); err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func deletePool(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid id")
		return
	}
	if err := pool.DeletePool(id); err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func listPoolAccounts(c *gin.Context) {
	poolID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid pool id")
		return
	}
	accounts, err := pool.ListAccounts(poolID)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, accounts)
}

func createPoolAccount(c *gin.Context) {
	poolID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid pool id")
		return
	}
	var req model.PoolAccount
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	req.PoolID = poolID
	if err := pool.CreateAccount(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, req)
}

func updatePoolAccount(c *gin.Context) {
	poolID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid pool id")
		return
	}
	accountID, err := strconv.Atoi(c.Param("aid"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid account id")
		return
	}
	var req struct {
		Name          string `json:"name"`
		Credentials   string `json:"credentials"`
		BaseURL       string `json:"base_url"`
		Status        string `json:"status"`
		Schedulable   *bool  `json:"schedulable"`
		Priority      *int   `json:"priority"`
		Concurrency   *int   `json:"concurrency"`
		ProxyConfigID *int   `json:"proxy_config_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Credentials != "" {
		updates["credentials"] = req.Credentials
	}
	if req.BaseURL != "" {
		updates["base_url"] = req.BaseURL
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}
	if req.Schedulable != nil {
		updates["schedulable"] = *req.Schedulable
	}
	if req.Priority != nil {
		updates["priority"] = *req.Priority
	}
	if req.Concurrency != nil {
		updates["concurrency"] = *req.Concurrency
	}
	if req.ProxyConfigID != nil {
		updates["proxy_config_id"] = *req.ProxyConfigID
	}
	if err := pool.UpdateAccount(poolID, accountID, updates); err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}

func deletePoolAccount(c *gin.Context) {
	poolID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid pool id")
		return
	}
	accountID, err := strconv.Atoi(c.Param("aid"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid account id")
		return
	}
	if err := pool.DeleteAccount(poolID, accountID); err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, nil)
}
