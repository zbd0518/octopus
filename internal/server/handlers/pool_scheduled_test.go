package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lingyuins/octopus/internal/op/pool"
	"github.com/lingyuins/octopus/internal/poolscheduledtest"
	"github.com/lingyuins/octopus/internal/server/auth"
	"github.com/lingyuins/octopus/internal/server/middleware"
	"github.com/lingyuins/octopus/internal/server/resp"
	"github.com/lingyuins/octopus/internal/server/router"
)

func init() {
	router.NewGroupRouter("/api/v1/pool/:id/scheduled-test").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermChannelsRead)).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/create", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(createPoolScheduledTest),
		).
		AddRoute(
			router.NewRoute("/update/:tid", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(updatePoolScheduledTest),
		).
		AddRoute(
			router.NewRoute("/delete/:tid", http.MethodDelete).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(deletePoolScheduledTest),
		).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listPoolScheduledTests),
		).
		AddRoute(
			router.NewRoute("/results/:tid", http.MethodGet).
				Handle(listPoolScheduledTestResults),
		)
}

// poolScheduledTestRequest is the create/update payload. account_id omitted
// or null scopes the plan to the whole pool; a positive value targets one
// account. The full desired shape is passed on every write (booleans are not
// sparse-diffed), mirroring UpdatePlan semantics.
type poolScheduledTestRequest struct {
	AccountID   *int   `json:"account_id"`
	CronExpr    string `json:"cron_expr" binding:"required"`
	Enabled     *bool  `json:"enabled"`
	AutoRecover *bool  `json:"auto_recover"`
}

func (r *poolScheduledTestRequest) enabled() bool {
	return r.Enabled == nil || *r.Enabled
}

func (r *poolScheduledTestRequest) autoRecover() bool {
	return r.AutoRecover != nil && *r.AutoRecover
}

// validateAccountScope rejects account scopes that reference a missing
// account (a whole-pool scope needs no validation).
func validateAccountScope(poolID int, accountID *int) error {
	if accountID == nil {
		return nil
	}
	if *accountID <= 0 {
		return errInvalidScheduledTestAccount
	}
	if _, err := pool.GetAccount(poolID, *accountID); err != nil {
		return errInvalidScheduledTestAccount
	}
	return nil
}

var errInvalidScheduledTestAccount = errors.New("invalid account_id for scheduled test")

func createPoolScheduledTest(c *gin.Context) {
	poolID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid pool id")
		return
	}
	var req poolScheduledTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	// Route-level validation enforces the narrowed cron grammar (the runner
	// never sees an unsupported expression).
	if _, err := poolscheduledtest.ParseCronExpr(req.CronExpr); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateAccountScope(poolID, req.AccountID); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	plan, err := poolscheduledtest.CreatePlan(poolID, req.AccountID, req.CronExpr, req.enabled(), req.autoRecover())
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, plan)
}

func updatePoolScheduledTest(c *gin.Context) {
	poolID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid pool id")
		return
	}
	planID, err := strconv.Atoi(c.Param("tid"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid plan id")
		return
	}
	var req poolScheduledTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	if _, err := poolscheduledtest.ParseCronExpr(req.CronExpr); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateAccountScope(poolID, req.AccountID); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	plan, err := poolscheduledtest.UpdatePlan(poolID, planID, req.CronExpr, req.AccountID, req.enabled(), req.autoRecover())
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, plan)
}

func deletePoolScheduledTest(c *gin.Context) {
	poolID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid pool id")
		return
	}
	planID, err := strconv.Atoi(c.Param("tid"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid plan id")
		return
	}
	if err := poolscheduledtest.DeletePlan(poolID, planID); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func listPoolScheduledTests(c *gin.Context) {
	poolID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid pool id")
		return
	}
	plans, err := poolscheduledtest.ListPlans(poolID)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, plans)
}

func listPoolScheduledTestResults(c *gin.Context) {
	poolID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid pool id")
		return
	}
	planID, err := strconv.Atoi(c.Param("tid"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid plan id")
		return
	}
	results, err := poolscheduledtest.ListResults(poolID, planID, 100)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, results)
}
