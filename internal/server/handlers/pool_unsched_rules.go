package handlers

import (
	"errors"
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
	router.NewGroupRouter("/api/v1/pool/unsched-rules").
		Use(middleware.Auth()).
		Use(middleware.RequirePermission(auth.PermChannelsRead)).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/create", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(createPoolUnschedRule),
		).
		AddRoute(
			router.NewRoute("/update/:id", http.MethodPost).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(updatePoolUnschedRule),
		).
		AddRoute(
			router.NewRoute("/delete/:id", http.MethodDelete).
				Use(middleware.RequirePermission(auth.PermChannelsWrite)).
				Handle(deletePoolUnschedRule),
		).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listPoolUnschedRules),
		)
}

var errInvalidUnschedRulePayload = errors.New("invalid unsched rule payload")

// poolUnschedRulePayload is the create/update body. match_status_code may be
// null (status-code dimension disabled); at least one match dimension must be
// set and duration_minutes must be positive.
type poolUnschedRulePayload struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	MatchStatusCode *int   `json:"match_status_code"`
	MatchKeyword    string `json:"match_keyword"`
	DurationMinutes *int   `json:"duration_minutes"`
	Enabled         *bool  `json:"enabled"`
	SortOrder       *int   `json:"sort_order"`
}

func (p *poolUnschedRulePayload) toModel() (*model.PoolUnschedRule, error) {
	if p.DurationMinutes == nil || *p.DurationMinutes <= 0 {
		return nil, errInvalidUnschedRulePayload
	}
	if p.MatchStatusCode == nil && p.MatchKeyword == "" {
		return nil, errInvalidUnschedRulePayload
	}
	if p.MatchStatusCode != nil && (*p.MatchStatusCode < 400 || *p.MatchStatusCode > 599) {
		return nil, errInvalidUnschedRulePayload
	}
	rule := &model.PoolUnschedRule{
		ID:              p.ID,
		Name:            p.Name,
		MatchStatusCode: p.MatchStatusCode,
		MatchKeyword:    p.MatchKeyword,
		DurationMinutes: *p.DurationMinutes,
		Enabled:         p.Enabled == nil || *p.Enabled,
	}
	if p.SortOrder != nil {
		rule.SortOrder = *p.SortOrder
	}
	return rule, nil
}

func createPoolUnschedRule(c *gin.Context) {
	var req poolUnschedRulePayload
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	rule, err := req.toModel()
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	rule.ID = 0
	if err := pool.CreatePoolUnschedRule(rule); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, rule)
}

func updatePoolUnschedRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid id")
		return
	}
	var req poolUnschedRulePayload
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	rule, err := req.toModel()
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	rule.ID = id
	if err := pool.UpdatePoolUnschedRule(rule); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func deletePoolUnschedRule(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid id")
		return
	}
	if err := pool.DeletePoolUnschedRule(id); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func listPoolUnschedRules(c *gin.Context) {
	rules, err := pool.ListPoolUnschedRules()
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, rules)
}
