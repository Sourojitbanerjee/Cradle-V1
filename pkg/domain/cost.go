package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// WasteType classifies the category of financial or resource waste.
type WasteType string

const (
	WasteTypeUnattachedVolume WasteType = "unattached_ebs_volume"
	WasteTypeIdleInstance     WasteType = "idle_ec2_instance"
	WasteTypeStaleSnapshot    WasteType = "stale_ebs_snapshot"
	WasteTypeUnattachedEIP    WasteType = "unattached_elastic_ip"
	WasteTypeIdleNATGateway   WasteType = "idle_nat_gateway"
	WasteTypeIdleDatabase     WasteType = "idle_rds_database"
)

// WasteStatus tracks the lifecycle of an identified waste finding.
type WasteStatus string

const (
	WasteStatusActive     WasteStatus = "active"
	WasteStatusDismissed  WasteStatus = "dismissed"
	WasteStatusRemediated WasteStatus = "remediated"
)

// CostSnapshot records point-in-time cost data aggregated from the provider.
type CostSnapshot struct {
	ID             uuid.UUID              `json:"id"`
	TenantID       uuid.UUID              `json:"tenant_id"`
	CloudAccountID uuid.UUID              `json:"cloud_account_id"`
	ResourceID     *uuid.UUID             `json:"resource_id,omitempty"`
	ResourceARN    string                 `json:"resource_arn,omitempty"`
	ServiceName    string                 `json:"service_name"`
	UsageType      string                 `json:"usage_type"`
	PeriodStart    time.Time              `json:"period_start"`
	PeriodEnd      time.Time              `json:"period_end"`
	Amount         float64                `json:"amount"`
	Currency       string                 `json:"currency"`
	UsageQuantity  float64                `json:"usage_quantity"`
	UsageUnit      string                 `json:"usage_unit"`
	Dimensions     map[string]interface{} `json:"dimensions,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
}

// MetricRollup stores statistical summaries of cloud telemetry over a window.
type MetricRollup struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	ResourceID  uuid.UUID `json:"resource_id"`
	MetricName  string    `json:"metric_name"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	AvgValue    float64   `json:"avg_value"`
	P95Value    float64   `json:"p95_value"`
	MaxValue    float64   `json:"max_value"`
	MinValue    float64   `json:"min_value"`
	SampleCount int       `json:"sample_count"`
	Unit        string    `json:"unit"`
}

// FactEvidence represents an unadulterated AWS fact cited in support of a finding.
type FactEvidence struct {
	Description string      `json:"description"`
	MetricName  string      `json:"metric_name,omitempty"`
	ObservedVal interface{} `json:"observed_val"`
	Unit        string      `json:"unit,omitempty"`
	ObservedAt  time.Time   `json:"observed_at"`
}

// WasteFinding represents an analyzed idle, orphaned, or oversized resource.
// Epistemological Principle:
// Facts cited in this finding are OBSERVED FROM AWS.
// The estimated monthly savings is DERIVED BY CRADLE.
// The waste classification and confidence score is INFERRED BY CRADLE.
type WasteFinding struct {
	ID                      uuid.UUID      `json:"id"`
	TenantID                uuid.UUID      `json:"tenant_id"`
	CloudAccountID          uuid.UUID      `json:"cloud_account_id"`
	ResourceID              uuid.UUID      `json:"resource_id"`
	ResourceARN             string         `json:"resource_arn"`
	ResourceType            string         `json:"resource_type"`
	ResourceName            string         `json:"resource_name"`
	Region                  string         `json:"region"`
	WasteType               WasteType      `json:"waste_type"`
	Confidence              float64        `json:"confidence"` // 0.00 to 1.00
	EstimatedMonthlySavings float64        `json:"estimated_monthly_savings"`
	ObservationPeriodDays   int            `json:"observation_period_days"`
	Facts                   []FactEvidence `json:"facts"`
	InferenceReason         string         `json:"inference_reason"`
	RecommendationSummary   string         `json:"recommendation_summary"`
	Status                  WasteStatus    `json:"status"`
	CreatedAt               time.Time      `json:"created_at"`
	UpdatedAt               time.Time      `json:"updated_at"`
}

// Validate ensures required fields are set on a WasteFinding.
func (w *WasteFinding) Validate() error {
	if w.TenantID == uuid.Nil {
		return errors.New("tenant ID is required")
	}
	if w.ResourceID == uuid.Nil {
		return errors.New("resource ID is required")
	}
	if w.ResourceARN == "" {
		return errors.New("resource ARN is required")
	}
	if w.WasteType == "" {
		return errors.New("waste type is required")
	}
	if w.Confidence < 0.0 || w.Confidence > 1.0 {
		return errors.New("confidence must be between 0.00 and 1.00")
	}
	if len(w.Facts) == 0 {
		return errors.New("waste finding must cite at least one observed fact")
	}
	return nil
}
