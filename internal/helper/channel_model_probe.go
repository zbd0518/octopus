package helper

import (
	"fmt"
	"strings"

	appmodel "github.com/lingyuins/octopus/internal/model"
)

// ChannelModelTestRequest tests a single (channel, model, endpoint_type) tuple
// by reusing the draft-group probe pipeline. It launches an async progress
// tracked probe identical to /api/v1/group/test-draft but scoped to one item,
// so callers poll the same GET /api/v1/group/test/progress/:id endpoint.
type ChannelModelTestRequest struct {
	ChannelID    int    `json:"channel_id" binding:"required"`
	ModelName    string `json:"model_name" binding:"required"`
	EndpointType string `json:"endpoint_type" binding:"required"`
}

// StartChannelModelTest launches an async probe of a single channel + model
// combination. It is a thin wrapper around StartDraftGroupModelTest so that
// channel-level and group-level probes share the same retry, logging, and
// progress-tracking implementation.
func StartChannelModelTest(req ChannelModelTestRequest, channel appmodel.Channel) (*GroupModelTestProgress, error) {
	modelName := strings.TrimSpace(req.ModelName)
	if modelName == "" {
		return nil, fmt.Errorf("model_name is required")
	}
	if req.ChannelID <= 0 {
		return nil, fmt.Errorf("channel_id is required")
	}

	endpointType := strings.TrimSpace(req.EndpointType)
	if endpointType == "" {
		endpointType = appmodel.EndpointTypeChat
	}

	items := []GroupModelDraftTestItem{{
		ClientID:  fmt.Sprintf("channel-%d-%s", req.ChannelID, modelName),
		ChannelID: req.ChannelID,
		ModelName: modelName,
	}}
	channels := map[int]appmodel.Channel{req.ChannelID: channel}

	return StartDraftGroupModelTest(endpointType, items, channels)
}
