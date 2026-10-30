package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	extism "github.com/extism/go-sdk"
	"github.com/lingyuins/octopus/internal/conf"
	"github.com/lingyuins/octopus/internal/model"
	notifop "github.com/lingyuins/octopus/internal/op/notification"
	"github.com/lingyuins/octopus/internal/utils/log"
)

var sensitiveLogPattern = regexp.MustCompile(`(?i)(sk-[a-z0-9_-]+|bearer\s+[a-z0-9._~+/-]+)`)

type dataKeyInput struct {
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
	Prefix string `json:"prefix,omitempty"`
}

func (g *generation) hostFunctions() []extism.HostFunction {
	return []extism.HostFunction{extism.NewHostFunctionWithStack("host_call", func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {
		input, err := p.ReadBytes(stack[0])
		var response HostResponse
		if err == nil {
			var request HostRequest
			if err = json.Unmarshal(input, &request); err == nil {
				var payload any
				payload, err = g.executeHost(ctx, request)
				if err == nil {
					encoded, marshalErr := json.Marshal(payload)
					if marshalErr != nil {
						err = marshalErr
					} else {
						response = HostResponse{OK: true, Payload: encoded}
					}
				}
			}
		}
		if err != nil {
			response = HostResponse{OK: false, Error: err.Error()}
		}
		encoded, _ := json.Marshal(response)
		offset, writeErr := p.WriteBytes(encoded)
		if writeErr != nil {
			offset, _ = p.WriteString(`{"ok":false,"error":"host response allocation failed"}`)
		}
		stack[0] = offset
	}, []extism.ValueType{extism.ValueTypePTR}, []extism.ValueType{extism.ValueTypePTR})}
}

func (g *generation) executeHost(ctx context.Context, in HostRequest) (any, error) {
	if in.APIVersion != 0 && in.APIVersion != HostAPIVersion {
		return nil, fmt.Errorf("incompatible host call API")
	}
	if len(in.Payload) > 64<<10 && !strings.HasPrefix(in.Capability, "http.") {
		return nil, fmt.Errorf("host call payload is too large")
	}
	var input dataKeyInput
	switch in.Capability {
	case "kv.get", "kv.set", "kv.delete", "kv.list", "secret.get", "secret.set", "secret.delete":
		if err := json.Unmarshal(in.Payload, &input); err != nil {
			return nil, err
		}
	}
	switch in.Capability {
	case "kv.get", "kv.set", "kv.delete", "kv.list":
		if !g.manifest.HasPermission("kv") {
			return nil, fmt.Errorf("kv permission denied")
		}
	case "secret.get", "secret.set", "secret.delete":
		if !g.manifest.HasPermission("secret") {
			return nil, fmt.Errorf("secret permission denied")
		}
	}
	switch in.Capability {
	case "kv.get":
		value, found, err := KVGet(ctx, g.manifest.ID, input.Key)
		return map[string]any{"value": value, "found": found}, err
	case "kv.set":
		return map[string]any{}, KVSet(ctx, g.manifest.ID, input.Key, input.Value)
	case "kv.delete":
		return map[string]any{}, KVDelete(ctx, g.manifest.ID, input.Key)
	case "kv.list":
		keys, err := KVList(ctx, g.manifest.ID, input.Prefix)
		return map[string]any{"keys": keys}, err
	case "secret.get":
		value, found, err := SecretGet(ctx, g.manifest.ID, input.Key)
		return map[string]any{"value": value, "found": found}, err
	case "secret.set":
		return map[string]any{}, SecretSet(ctx, g.manifest.ID, input.Key, input.Value)
	case "secret.delete":
		return map[string]any{}, SecretDelete(ctx, g.manifest.ID, input.Key)
	case "http.request":
		return executeHTTP(ctx, g.manifest, in)
	case "notify.publish":
		return nil, g.publishNotification(ctx, in.Payload)
	case "log.write":
		if !g.manifest.HasPermission("log") {
			return nil, fmt.Errorf("log permission denied")
		}
		var request struct {
			Level   string `json:"level"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(in.Payload, &request); err != nil {
			return nil, err
		}
		if len(request.Message) > 8192 {
			return nil, fmt.Errorf("log message is too long")
		}
		message := sensitiveLogPattern.ReplaceAllString(request.Message, "[redacted]")
		switch request.Level {
		case "debug":
			log.Debugf("plugin %s: %s", g.manifest.ID, message)
		case "warn":
			log.Warnf("plugin %s: %s", g.manifest.ID, message)
		case "error":
			log.Errorf("plugin %s: %s", g.manifest.ID, message)
		default:
			log.Infof("plugin %s: %s", g.manifest.ID, message)
		}
		return map[string]any{}, nil
	default:
		return nil, fmt.Errorf("unknown host capability %q", in.Capability)
	}
}

func (g *generation) publishNotification(ctx context.Context, raw json.RawMessage) error {
	if !g.manifest.HasPermission("notify") {
		return fmt.Errorf("notify permission denied")
	}
	var request struct {
		Title     string                     `json:"title"`
		Content   string                     `json:"content"`
		Severity  model.NotificationSeverity `json:"severity"`
		DedupeKey string                     `json:"dedupe_key"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return err
	}
	request.Title, request.Content = strings.TrimSpace(request.Title), strings.TrimSpace(request.Content)
	if request.Title == "" || len(request.Title) > 256 || len(request.Content) > 16384 {
		return fmt.Errorf("invalid plugin notification")
	}
	entry := &model.Notification{Type: model.NotificationTypeSystem, Severity: request.Severity, Title: sensitiveLogPattern.ReplaceAllString(request.Title, "[redacted]"), Content: sensitiveLogPattern.ReplaceAllString(request.Content, "[redacted]"), Source: "plugin:" + g.manifest.ID, DedupeKey: request.DedupeKey, Link: "plugins"}
	if err := notifop.Create(ctx, entry); err != nil {
		return err
	}
	return nil
}

func hostLogPluginError(id string, err error) {
	if err == nil {
		return
	}
	log.Warnf("plugin %s: %v", id, sensitiveLogPattern.ReplaceAllString(err.Error(), "[redacted]"))
}

func pluginVersion() string { return conf.Version }
