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

// CostBreakdownItem represents one row in a dimensional cost breakdown.
type CostBreakdownItem struct {
	Key           string    `json:"key"`           // e.g. "Amazon Elastic Compute Cloud", "us-east-1", "payments"
	Name          string    `json:"name"`          // Human-friendly label
	Amount        float64   `json:"amount"`        // Cost in currency
	Percentage    float64   `json:"percentage"`    // % of total
	ResourceCount int       `json:"resource_count,omitempty"`
	Currency      string    `json:"currency"`
	Source        string    `json:"source"`
	PeriodStart   time.Time `json:"period_start"`
	PeriodEnd     time.Time `json:"period_end"`
}

// CostBreakdownResult encapsulates a complete dimensional cost breakdown.
type CostBreakdownResult struct {
	Dimension   string              `json:"dimension"` // "account", "region", "service", "resource", "team", "application", "environment"
	TotalAmount float64             `json:"total_amount"`
	Currency    string              `json:"currency"`
	PeriodStart time.Time           `json:"period_start"`
	PeriodEnd   time.Time           `json:"period_end"`
	Source      string              `json:"source"`
	Items       []CostBreakdownItem `json:"items"`
}

// CostTrendPoint represents spend for a single time bucket (day or month).
type CostTrendPoint struct {
	Date          string  `json:"date"` // YYYY-MM-DD or YYYY-MM
	Amount        float64 `json:"amount"`
	MovingAverage float64 `json:"moving_average,omitempty"` // 7-day rolling average
	Currency      string  `json:"currency"`
	Source        string  `json:"source"`
}

// CostTrendsResult encapsulates historical spend trends.
type CostTrendsResult struct {
	Granularity string           `json:"granularity"` // "daily" or "monthly"
	TotalAmount float64          `json:"total_amount"`
	Currency    string           `json:"currency"`
	PeriodStart time.Time        `json:"period_start"`
	PeriodEnd   time.Time        `json:"period_end"`
	Source      string           `json:"source"`
	Points      []CostTrendPoint `json:"points"`
}

// CostAnomaly represents a statistically significant, unexpected spend deviation.
type CostAnomaly struct {
	ID              uuid.UUID `json:"id"`
	TenantID        uuid.UUID `json:"tenant_id"`
	CloudAccountID  *uuid.UUID `json:"cloud_account_id,omitempty"`
	AnomalyDate     time.Time `json:"anomaly_date"`
	DimensionType   string    `json:"dimension_type"`  // "service", "account", "region", "total"
	DimensionValue  string    `json:"dimension_value"` // e.g. "Amazon Elastic Kubernetes Service"
	ActualAmount    float64   `json:"actual_amount"`
	ExpectedAmount  float64   `json:"expected_amount"`
	DeviationAmount float64   `json:"deviation_amount"`
	PercentageSurge float64   `json:"percentage_surge"`
	Severity        string    `json:"severity"` // "critical", "high", "medium"
	Status          string    `json:"status"`   // "active", "acknowledged", "resolved"
	Evidence        []string  `json:"evidence"`
	Source          string    `json:"source"`
	CreatedAt       time.Time `json:"created_at"`
}

// DailyForecastPoint represents a projected daily cost value.
type DailyForecastPoint struct {
	Date            string  `json:"date"` // YYYY-MM-DD
	PredictedAmount float64 `json:"predicted_amount"`
	LowerBound      float64 `json:"lower_bound"`
	UpperBound      float64 `json:"upper_bound"`
}

// CostForecastResult provides forward-looking spend estimates with confidence bands.
type CostForecastResult struct {
	ForecastDays           int                  `json:"forecast_days"`
	PeriodStart            time.Time            `json:"period_start"`
	PeriodEnd              time.Time            `json:"period_end"`
	EstimatedMonthlySpend  float64              `json:"estimated_monthly_spend"`
	ForecastedPeriodSpend  float64              `json:"forecasted_period_spend"`
	ConfidenceIntervalLow  float64              `json:"confidence_interval_low"`
	ConfidenceIntervalHigh float64              `json:"confidence_interval_high"`
	Currency               string               `json:"currency"`
	Methodology            string               `json:"methodology"`
	Confidence             float64              `json:"confidence"`
	Source                 string               `json:"source"`
	DailyForecast          []DailyForecastPoint `json:"daily_forecast"`
}

// ResourceChangeExplanation details an infrastructure modification explaining a cost driver.
type ResourceChangeExplanation struct {
	ChangeType      string    `json:"change_type"` // "ADDED", "MODIFIED", "SCALED", "USAGE_SURGE", "DELETED"
	ResourceName    string    `json:"resource_name"`
	ResourceType    string    `json:"resource_type"`
	NativeID        string    `json:"native_id,omitempty"`
	Explanation     string    `json:"explanation"`
	Team            string    `json:"team,omitempty"`
	Application     string    `json:"application,omitempty"`
	Environment     string    `json:"environment,omitempty"`
	EstimatedImpact float64   `json:"estimated_impact"`
	ObservedAt      time.Time `json:"observed_at"`
}

// CostDriver isolates a single service contributing to an overall cost variance.
type CostDriver struct {
	ServiceName      string                      `json:"service_name"`
	PreviousAmount   float64                     `json:"previous_amount"`
	CurrentAmount    float64                     `json:"current_amount"`
	DeltaAmount      float64                     `json:"delta_amount"`
	PercentageChange float64                     `json:"percentage_change"`
	ShareOfIncrease  float64                     `json:"share_of_increase"` // % of total increase
	Explanation      string                      `json:"explanation"`
	ResourceChanges  []ResourceChangeExplanation `json:"resource_changes,omitempty"`
}

// AttributionItem groups spend delta by operational dimension.
type AttributionItem struct {
	Name             string  `json:"name"`
	PreviousAmount   float64 `json:"previous_amount"`
	CurrentAmount    float64 `json:"current_amount"`
	DeltaAmount      float64 `json:"delta_amount"`
	PercentageChange float64 `json:"percentage_change"`
}

// CostExplanation provides an evidence-based answer to "Why did our bill increase?".
type CostExplanation struct {
	Headline               string            `json:"headline"`
	Direction              string            `json:"direction"` // "INCREASED", "DECREASED", "STABLE"
	PreviousPeriodStart    time.Time         `json:"previous_period_start"`
	PreviousPeriodEnd      time.Time         `json:"previous_period_end"`
	PreviousTotalAmount    float64           `json:"previous_total_amount"`
	CurrentPeriodStart     time.Time         `json:"current_period_start"`
	CurrentPeriodEnd       time.Time         `json:"current_period_end"`
	CurrentTotalAmount     float64           `json:"current_total_amount"`
	DeltaAmount            float64           `json:"delta_amount"`
	PercentageChange       float64           `json:"percentage_change"`
	Currency               string            `json:"currency"`
	PrimaryDrivers         []CostDriver      `json:"primary_drivers"`
	TeamAttribution        []AttributionItem `json:"team_attribution"`
	ApplicationAttribution []AttributionItem `json:"application_attribution"`
	EnvironmentAttribution []AttributionItem `json:"environment_attribution"`
	WasteCorrelation       string            `json:"waste_correlation,omitempty"`
	Recommendations        []string          `json:"recommendations"`
	Source                 string            `json:"source"`
	GeneratedAt            time.Time         `json:"generated_at"`
}
