package model

import "time"

const (
	PluginStatusDisabled       = "disabled"
	PluginStatusEnabled        = "enabled"
	PluginStatusError          = "error"
	PluginStatusNeedsReinstall = "needs_reinstall"
	PluginStatusUninstalling   = "uninstalling"
)

// Plugin stores installation metadata. Secrets and executable bytes are deliberately
// kept outside this record so listing plugins never exposes either.
type Plugin struct {
	ID             string    `json:"id" gorm:"primaryKey;size:64"`
	Name           string    `json:"name" gorm:"size:128;not null"`
	Version        string    `json:"version" gorm:"size:32;not null"`
	ManifestJSON   string    `json:"manifest" gorm:"type:text;not null"`
	ConfigJSON     string    `json:"config" gorm:"type:text"`
	SHA256         string    `json:"sha256" gorm:"size:64;not null"`
	Status         string    `json:"status" gorm:"size:32;index;not null"`
	Enabled        bool      `json:"enabled" gorm:"index;not null;default:false"`
	SchemaVersion  int       `json:"schema_version" gorm:"not null;default:0"`
	LastError      string    `json:"last_error,omitempty" gorm:"type:text"`
	Calls          int64     `json:"calls" gorm:"not null;default:0"`
	Failures       int64     `json:"failures" gorm:"not null;default:0"`
	TimeoutCount   int64     `json:"timeouts" gorm:"not null;default:0"`
	UninstallAfter bool      `json:"uninstall_after" gorm:"not null;default:false"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (Plugin) TableName() string { return "plugins" }

type PluginKV struct {
	PluginID  string    `json:"-" gorm:"primaryKey;size:64"`
	Key       string    `json:"key" gorm:"primaryKey;size:256"`
	Value     string    `json:"value" gorm:"type:text"`
	UpdatedAt time.Time `json:"-"`
}

func (PluginKV) TableName() string { return "plugin_kv" }

type PluginSecret struct {
	PluginID   string    `json:"-" gorm:"primaryKey;size:64"`
	Key        string    `json:"key" gorm:"primaryKey;size:256"`
	Value      string    `json:"-" gorm:"type:text"`
	BackupText string    `json:"value,omitempty" gorm:"-"`
	UpdatedAt  time.Time `json:"-"`
}

func (PluginSecret) TableName() string { return "plugin_secrets" }

type PluginConfigRequest struct {
	Config map[string]any `json:"config"`
}

type PluginUninstallRequest struct {
	DeleteData bool `json:"delete_data"`
}
