package model

import "time"

type ModelNormalizeTaskStatus string

const (
	ModelNormalizeTaskStatusQueued    ModelNormalizeTaskStatus = "queued"
	ModelNormalizeTaskStatusRunning   ModelNormalizeTaskStatus = "running"
	ModelNormalizeTaskStatusCompleted ModelNormalizeTaskStatus = "completed"
	ModelNormalizeTaskStatusFailed    ModelNormalizeTaskStatus = "failed"
)

type ModelNormalizeTaskStep string

const (
	ModelNormalizeTaskStepQueued     ModelNormalizeTaskStep = "queued"
	ModelNormalizeTaskStepCollecting ModelNormalizeTaskStep = "collecting_models"
	ModelNormalizeTaskStepBuilding   ModelNormalizeTaskStep = "building_batches"
	ModelNormalizeTaskStepAnalyzing  ModelNormalizeTaskStep = "analyzing_batches"
	ModelNormalizeTaskStepValidating ModelNormalizeTaskStep = "validating"
	ModelNormalizeTaskStepCompleted  ModelNormalizeTaskStep = "completed"
	ModelNormalizeTaskStepFailed     ModelNormalizeTaskStep = "failed"
)

type ModelNormalizeRuleSet struct {
	RouterPrefixes      []string            `json:"router_prefixes"`
	FunctionalSuffixes  []string            `json:"functional_suffixes"`
	ExplicitMappings    []map[string]string `json:"explicit_mappings"`
	MarketDedupeDefault bool                `json:"market_dedupe_default"`
}

type ModelNormalizeCoverageReport struct {
	InputCount         int                             `json:"input_count"`
	CanonicalCount     int                             `json:"canonical_count"`
	MergedClusterCount int                             `json:"merged_cluster_count"`
	MergedVariantCount int                             `json:"merged_variant_count"`
	UnchangedCount     int                             `json:"unchanged_count"`
	Unresolved         []string                        `json:"unresolved,omitempty"`
	Clusters           []ModelNormalizeCoverageCluster `json:"clusters,omitempty"`
	HighRiskMerges     []ModelNormalizeCoverageCluster `json:"high_risk_merges,omitempty"`
	Diagnostics        []ModelNormalizeRuleDiagnostic  `json:"diagnostics,omitempty"`
}

type ModelNormalizeCoverageCluster struct {
	Canonical string   `json:"canonical"`
	Variants  []string `json:"variants"`
}

type ModelNormalizeRuleDiagnostic struct {
	Severity  string `json:"severity"`
	Kind      string `json:"kind"`
	Message   string `json:"message"`
	Index     int    `json:"index,omitempty"`
	Variant   string `json:"variant,omitempty"`
	Canonical string `json:"canonical,omitempty"`
}

type ModelNormalizeTaskSummary struct {
	TotalModels      int `json:"total_models"`
	ProcessedModels  int `json:"processed_models"`
	TotalBatches     int `json:"total_batches"`
	CompletedBatches int `json:"completed_batches"`
}

type ModelNormalizeTaskResult struct {
	Rules            ModelNormalizeRuleSet        `json:"rules"`
	Coverage         ModelNormalizeCoverageReport `json:"coverage"`
	Valid            bool                         `json:"valid"`
	ValidationErrors []string                     `json:"validation_errors,omitempty"`
}

type ModelNormalizeTask struct {
	ID               string                     `json:"id" gorm:"primaryKey;size:64"`
	Status           ModelNormalizeTaskStatus   `json:"status" gorm:"size:32;not null;index"`
	CurrentStep      ModelNormalizeTaskStep     `json:"current_step" gorm:"size:32;not null"`
	ProgressPercent  int                        `json:"progress_percent" gorm:"not null;default:0"`
	TotalBatches     int                        `json:"total_batches" gorm:"not null;default:0"`
	CompletedBatches int                        `json:"completed_batches" gorm:"not null;default:0"`
	Done             bool                       `json:"done" gorm:"not null;default:false;index"`
	ResultReady      bool                       `json:"result_ready" gorm:"not null;default:false"`
	Message          string                     `json:"message" gorm:"type:text"`
	ErrorReason      string                     `json:"error_reason" gorm:"type:text"`
	StartedAt        *time.Time                 `json:"started_at,omitempty"`
	UpdatedAt        *time.Time                 `json:"updated_at,omitempty"`
	FinishedAt       *time.Time                 `json:"finished_at,omitempty"`
	HeartbeatAt      *time.Time                 `json:"heartbeat_at,omitempty"`
	EventSequence    int64                      `json:"event_sequence" gorm:"not null;default:0"`
	Summary          *ModelNormalizeTaskSummary `json:"summary,omitempty" gorm:"serializer:json"`
	Result           *ModelNormalizeTaskResult  `json:"result,omitempty" gorm:"serializer:json"`
}

func (ModelNormalizeTask) TableName() string { return "model_normalize_tasks" }
