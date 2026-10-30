package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrRequiredPlugin  = errors.New("required plugin call failed")
	ErrPayloadTooLarge = errors.New("plugin payload exceeds 1 MiB")
)

type CallRequest struct {
	APIVersion int             `json:"api_version"`
	RequestID  string          `json:"request_id"`
	PluginID   string          `json:"plugin_id"`
	Event      string          `json:"event"`
	AttemptID  string          `json:"attempt_id,omitempty"`
	Payload    json.RawMessage `json:"payload"`
	Config     map[string]any  `json:"config,omitempty"`
}

type CallResponse struct {
	APIVersion int             `json:"api_version,omitempty"`
	Decision   string          `json:"decision"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	Tag        string          `json:"tag,omitempty"`
	Error      string          `json:"error,omitempty"`
}

type HostRequest struct {
	APIVersion int             `json:"api_version,omitempty"`
	Capability string          `json:"capability"`
	Payload    json.RawMessage `json:"payload"`
}

type HostResponse struct {
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

type RequiredError struct {
	ID  string
	Err error
}

func (e *RequiredError) Error() string { return fmt.Sprintf("plugin %s: %v", e.ID, e.Err) }
func (e *RequiredError) Unwrap() error { return ErrRequiredPlugin }

type EventResult struct{ Payload json.RawMessage }

// Handles reports whether the default manager currently subscribes to event.
func Handles(event string) bool {
	return DefaultManager.Handles(event)
}

func Invoke(ctx context.Context, event string, payload any) (EventResult, error) {
	manager := DefaultManager
	if !manager.Handles(event) {
		return EventResult{}, nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return EventResult{}, err
	}
	data, err = manager.Invoke(ctx, event, "", data)
	return EventResult{Payload: data}, err
}

func Publish(event string, payload any) bool {
	if DefaultManager == nil {
		return false
	}
	return DefaultManager.Publish(event, payload)
}

func newRequestID(id, attempt string) string {
	if attempt == "" {
		return fmt.Sprintf("%s:%d", id, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s:%s:%d", id, attempt, time.Now().UnixNano())
}

func validateDecision(response CallResponse) error {
	switch strings.ToLower(response.Decision) {
	case "continue", "observe", "handled", "allow":
		return nil
	default:
		return fmt.Errorf("invalid plugin decision %q", response.Decision)
	}
}
