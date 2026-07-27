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
		).
		AddRoute(
			router.NewRoute("/:id/account/test", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(testPoolAccount),
		).
		AddRoute(
			router.NewRoute("/:id/account/quota/:aid", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(fetchPoolAccountQuota),
		).
		AddRoute(
			router.NewRoute("/:id/account/refresh-token/:aid", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(refreshPoolAccountToken),
		).
		AddRoute(
			router.NewRoute("/import", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(importPoolAccounts),
		).
		AddRoute(
			router.NewRoute("/:id/account/:aid", http.MethodGet).
				Handle(getPoolAccount),
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
	resp.Success(c, pool.MaskAccounts(accounts))
}

// poolAccountRequest 账号创建/更新请求体。扩展自旧 model.PoolAccount 直绑，
// 增加平台/类型/模型/备注/token_expires_at 字段，凭据在写入前加密。
type poolAccountRequest struct {
	Name           string `json:"name"`
	Platform       string `json:"platform"`
	Type           string `json:"type"`
	Models         string `json:"models"`
	Credentials    string `json:"credentials"`
	BaseURL        string `json:"base_url"`
	Status         string `json:"status"`
	Schedulable    *bool  `json:"schedulable"`
	Priority       *int   `json:"priority"`
	Concurrency    *int   `json:"concurrency"`
	ProxyConfigID  *int   `json:"proxy_config_id"`
	Notes          string `json:"notes"`
	TokenExpiresAt *int64 `json:"token_expires_at"`
}

func createPoolAccount(c *gin.Context) {
	poolID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid pool id")
		return
	}
	var req poolAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	acct := model.PoolAccount{
		PoolID:        poolID,
		Name:          req.Name,
		Platform:      req.Platform,
		Type:          req.Type,
		Models:        req.Models,
		Credentials:   pool.EncryptCredentials(req.Credentials),
		BaseURL:       req.BaseURL,
		Status:        req.Status,
		Priority:      derefInt(req.Priority),
		Concurrency:   derefInt(req.Concurrency),
		ProxyConfigID: req.ProxyConfigID,
		Notes:         req.Notes,
	}
	if req.Schedulable != nil {
		acct.Schedulable = *req.Schedulable
	} else {
		acct.Schedulable = true
	}
	if req.TokenExpiresAt != nil {
		acct.TokenExpiresAt = *req.TokenExpiresAt
	}
	if err := pool.CreateAccount(&acct); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, pool.MaskAccount(&acct))
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
	var req poolAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Platform != "" {
		updates["platform"] = req.Platform
	}
	if req.Type != "" {
		updates["type"] = req.Type
	}
	if req.Models != "" {
		updates["models"] = req.Models
	}
	if req.Credentials != "" {
		updates["credentials"] = pool.EncryptCredentials(req.Credentials)
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
	if req.Notes != "" {
		updates["notes"] = req.Notes
	}
	if req.TokenExpiresAt != nil {
		updates["token_expires_at"] = *req.TokenExpiresAt
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

func getPoolAccount(c *gin.Context) {
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
	acct, err := pool.GetAccount(poolID, accountID)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, pool.MaskAccount(acct))
}

func testPoolAccount(c *gin.Context) {
	poolID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid pool id")
		return
	}
	var req struct {
		AccountID int    `json:"account_id" binding:"required"`
		Model     string `json:"model"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	result, err := pool.TestAccount(poolID, req.AccountID, req.Model)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, result)
}

func fetchPoolAccountQuota(c *gin.Context) {
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
	result, err := pool.FetchAccountQuota(c.Request.Context(), poolID, accountID)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, result)
}

func refreshPoolAccountToken(c *gin.Context) {
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
	if err := pool.RefreshAccountToken(c.Request.Context(), poolID, accountID); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func importPoolAccounts(c *gin.Context) {
	var req struct {
		PoolID   int    `json:"pool_id" binding:"required"`
		Accounts string `json:"accounts"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	accounts, err := pool.ParseImportedAccounts(req.Accounts, req.PoolID)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "parse accounts: "+err.Error())
		return
	}
	if err := pool.ImportAccounts(accounts); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, map[string]int{"imported": len(accounts)})
}

func derefInt(p *int) int {
	if p != nil {
		return *p
	}
	return 0
}
