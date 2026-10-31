package helper

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lingyuins/octopus/internal/nodetransport"
	nodeop "github.com/lingyuins/octopus/internal/op/node"
)

const nodeProbeTimeout = 10 * time.Second
const nodeProbeLimit = nodetransport.MaxModelsResponseBytes

type NodeProbeResult struct {
	Passed     bool   `json:"passed"`
	Code       string `json:"code"`
	LatencyMS  int64  `json:"latency_ms"`
	StatusCode int    `json:"status_code"`
	ModelCount int    `json:"model_count"`
}

// TestNode sends only the fixed models request through the existing tunnel.
// It never accepts an address or invokes a potentially billable model.
func TestNode(ctx context.Context, id int) (*NodeProbeResult, error) {
	record, err := nodeop.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return probeNode(ctx, id, record.Enabled, nodetransport.DefaultManager), nil
}

func probeNode(ctx context.Context, id int, enabled bool, manager *nodetransport.Manager) *NodeProbeResult {
	started := time.Now()
	result := &NodeProbeResult{Code: "offline"}
	defer func() { result.LatencyMS = time.Since(started).Milliseconds() }()
	if !enabled {
		result.Code = "disabled"
		return result
	}
	if !manager.Snapshot(id).Online {
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, nodeProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nodetransport.BaseURL+"/models", nil)
	if err != nil {
		result.Code = "upstream_failed"
		return result
	}
	req.Header.Set("Accept", "application/json")
	response, err := manager.Client(id, nodeProbeTimeout).Do(req)
	if err != nil {
		result.Code = nodeProbeErrorCode(err)
		return result
	}
	defer response.Body.Close()
	result.StatusCode = response.StatusCode
	if response.StatusCode != http.StatusOK {
		result.Code = "local_http"
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			result.Code = "local_authentication"
		}
		return result
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, nodeProbeLimit+1))
	if err != nil {
		result.Code = nodeProbeErrorCode(err)
		return result
	}
	var models struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if len(body) > nodeProbeLimit || json.Unmarshal(body, &models) != nil || models.Data == nil || len(models.Data) > 1024 {
		result.Code = "invalid_models"
		return result
	}
	seen := make(map[string]bool, len(models.Data))
	for _, item := range models.Data {
		if item.ID == "" || strings.TrimSpace(item.ID) != item.ID || len(item.ID) > 512 || strings.IndexFunc(item.ID, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 || seen[item.ID] {
			result.Code = "invalid_models"
			return result
		}
		seen[item.ID] = true
	}
	result.ModelCount = len(seen)
	if result.ModelCount == 0 {
		result.Code = "no_models"
		return result
	}
	result.Code, result.Passed = "ok", true
	return result
}

func nodeProbeErrorCode(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, nodetransport.ErrNodeOffline):
		return "offline"
	case errors.Is(err, nodetransport.ErrNodeBusy):
		return "busy"
	case errors.Is(err, nodetransport.ErrLocalUnavailable):
		return "local_unavailable"
	case errors.Is(err, nodetransport.ErrLocalAuthentication):
		return "local_authentication"
	case errors.Is(err, nodetransport.ErrLocalHTTP):
		return "local_http"
	case errors.Is(err, nodetransport.ErrInvalidModels):
		return "invalid_models"
	default:
		return "upstream_failed"
	}
}
