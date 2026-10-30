package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	HostAPIVersion      = 1
	MaxTimeoutMillis    = 2000
	MaxMemoryMiB        = 64
	MaxInstances        = 16
	MaxPayloadBytes     = 1 << 20
	DefaultEventHandler = "handle"
)

var (
	pluginIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)
	semverPattern   = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
)

var supportedEvents = map[string]bool{
	"relay.request.before_route": true, "relay.route.filter": true,
	"relay.request.before_upstream": true, "relay.response.before_send": true,
	"relay.completed": true, "channel.health.changed": true,
	"pool.account.changed": true, "notification.created": true,
	"http.rpc": true, "plugin.migrate": true,
}

var supportedPermissions = map[string]bool{
	"kv": true, "secret": true, "http": true, "notify": true, "log": true,
	"relay:read": true, "relay:write": true, "rpc:read": true, "rpc:write": true,
}

type UIManifest struct {
	Entry string `json:"entry"`
	Title string `json:"title"`
}

type Manifest struct {
	HostAPI       int             `json:"host_api"`
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Version       string          `json:"version"`
	Description   string          `json:"description,omitempty"`
	Author        string          `json:"author,omitempty"`
	MinHostAPI    int             `json:"min_host_api,omitempty"`
	MaxHostAPI    int             `json:"max_host_api,omitempty"`
	Entry         string          `json:"entry,omitempty"`
	Events        []string        `json:"events,omitempty"`
	Permissions   []string        `json:"permissions,omitempty"`
	HTTPOrigins   []string        `json:"http_origins,omitempty"`
	Required      bool            `json:"required,omitempty"`
	Priority      int             `json:"priority,omitempty"`
	TimeoutMS     int             `json:"timeout_ms,omitempty"`
	MemoryMiB     int             `json:"memory_mib,omitempty"`
	Instances     int             `json:"instances,omitempty"`
	SchemaVersion int             `json:"schema_version,omitempty"`
	ConfigSchema  json.RawMessage `json:"config_schema,omitempty"`
	UI            *UIManifest     `json:"ui,omitempty"`
}

func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return m, fmt.Errorf("invalid manifest: %w", err)
	}
	if decoder.More() {
		return m, fmt.Errorf("invalid manifest: trailing content")
	}
	return m, m.Validate()
}

func (m *Manifest) Validate() error {
	if m.HostAPI != HostAPIVersion || (m.MinHostAPI != 0 && m.MinHostAPI > HostAPIVersion) || (m.MaxHostAPI != 0 && m.MaxHostAPI < HostAPIVersion) {
		return fmt.Errorf("plugin requires an incompatible host API")
	}
	if !pluginIDPattern.MatchString(m.ID) || strings.HasSuffix(m.ID, "-") {
		return fmt.Errorf("invalid plugin id")
	}
	if strings.TrimSpace(m.Name) == "" || len(m.Name) > 128 || !semverPattern.MatchString(m.Version) {
		return fmt.Errorf("invalid plugin name or semantic version")
	}
	if m.Entry == "" {
		m.Entry = DefaultEventHandler
	}
	if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`).MatchString(m.Entry) {
		return fmt.Errorf("invalid entry export")
	}
	if m.TimeoutMS == 0 {
		m.TimeoutMS = 100
	}
	if m.MemoryMiB == 0 {
		m.MemoryMiB = 16
	}
	if m.Instances == 0 {
		m.Instances = 4
	}
	if m.TimeoutMS < 1 || m.TimeoutMS > MaxTimeoutMillis || m.MemoryMiB < 1 || m.MemoryMiB > MaxMemoryMiB || m.Instances < 1 || m.Instances > MaxInstances {
		return fmt.Errorf("resource limits exceed host limits")
	}
	if m.Priority < -100 || m.Priority > 100 || m.SchemaVersion < 0 {
		return fmt.Errorf("invalid priority or schema version")
	}
	seenEvents, seenPermissions := map[string]bool{}, map[string]bool{}
	for _, event := range m.Events {
		if !supportedEvents[event] || seenEvents[event] {
			return fmt.Errorf("unsupported or duplicate event %q", event)
		}
		seenEvents[event] = true
	}
	for _, permission := range m.Permissions {
		if !supportedPermissions[permission] || seenPermissions[permission] {
			return fmt.Errorf("unsupported or duplicate permission %q", permission)
		}
		seenPermissions[permission] = true
	}
	for _, event := range m.Events {
		if strings.HasPrefix(event, "relay.") && !seenPermissions["relay:read"] && !seenPermissions["relay:write"] {
			return fmt.Errorf("event %q requires relay permission", event)
		}
		if (event == "relay.request.before_route" || event == "relay.route.filter" || event == "relay.request.before_upstream" || event == "relay.response.before_send") && !seenPermissions["relay:write"] {
			return fmt.Errorf("event %q requires relay:write", event)
		}
	}
	if len(m.HTTPOrigins) > 0 && !seenPermissions["http"] {
		return fmt.Errorf("http_origins requires http permission")
	}
	for _, origin := range m.HTTPOrigins {
		if !regexp.MustCompile(`^https://[^\s/]+$`).MatchString(origin) {
			return fmt.Errorf("http origin must be an HTTPS origin: %q", origin)
		}
	}
	if len(m.ConfigSchema) > 0 && !json.Valid(m.ConfigSchema) {
		return fmt.Errorf("invalid config schema")
	}
	if m.UI != nil {
		if m.UI.Entry == "" {
			m.UI.Entry = "index.html"
		}
		if strings.TrimSpace(m.UI.Title) == "" || !safePackagePath(m.UI.Entry) {
			return fmt.Errorf("invalid plugin UI")
		}
	}
	return nil
}

func (m Manifest) HasPermission(permission string) bool {
	for _, item := range m.Permissions {
		if item == permission {
			return true
		}
	}
	return false
}

func (m Manifest) Handles(event string) bool {
	for _, item := range m.Events {
		if item == event {
			return true
		}
	}
	return false
}

func CompareVersions(a, b string) int {
	parse := func(v string) [3]int {
		var n [3]int
		v = strings.SplitN(strings.SplitN(v, "+", 2)[0], "-", 2)[0]
		_, _ = fmt.Sscanf(v, "%d.%d.%d", &n[0], &n[1], &n[2])
		return n
	}
	x, y := parse(a), parse(b)
	for i := range x {
		if x[i] < y[i] {
			return -1
		}
		if x[i] > y[i] {
			return 1
		}
	}
	return 0
}

func SortManifests(items []Manifest) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority < items[j].Priority
		}
		return items[i].ID < items[j].ID
	})
}
