package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// RiskLevel categorizes the danger profile of an infrastructure action.
type RiskLevel string

const (
	RiskLevelLow      RiskLevel = "low"      // e.g. updating non-critical tag, attaching cost allocation metadata
	RiskLevelMedium   RiskLevel = "medium"   // e.g. modifying snapshot retention, enabling encryption on new objects
	RiskLevelHigh     RiskLevel = "high"     // e.g. resizing an EC2 instance, modifying security group rules
	RiskLevelCritical RiskLevel = "critical" // e.g. deleting an unattached EBS volume, terminating an instance
)

// ActionStatus represents the state of an ActionPlan in the governance workflow.
type ActionStatus string

const (
	ActionStatusDraft           ActionStatus = "draft"
	ActionStatusPendingApproval ActionStatus = "pending_approval"
	ActionStatusApproved        ActionStatus = "approved"
	ActionStatusRejected        ActionStatus = "rejected"
	ActionStatusExecuting       ActionStatus = "executing"
	ActionStatusExecuted        ActionStatus = "executed"
	ActionStatusFailed          ActionStatus = "failed"
	ActionStatusRolledBack      ActionStatus = "rolled_back"
)

// BlastRadiusSummary quantifies downstream impacts before an action is permitted.
type BlastRadiusSummary struct {
	DirectResourceCount     int      `json:"direct_resource_count"`
	DownstreamResourceCount int      `json:"downstream_resource_count"`
	AffectedServices        []string `json:"affected_services"`
	CrossesProductionVPC    bool     `json:"crosses_production_vpc"`
	PotentialDowntimeSecs   int      `json:"potential_downtime_secs"`
}

// ActionPlan defines a proposed, previewable infrastructure change before execution.
type ActionPlan struct {
	ID                      uuid.UUID              `json:"id"`
	TenantID                uuid.UUID              `json:"tenant_id"`
	Title                   string                 `json:"title"`
	Description             string                 `json:"description"`
	ActionType              string                 `json:"action_type"` // e.g. "downsize_ec2", "delete_unattached_ebs"
	RiskLevel               RiskLevel              `json:"risk_level"`
	Status                  ActionStatus           `json:"status"`
	EstimatedMonthlySavings float64                `json:"estimated_monthly_savings"`
	BlastRadius             BlastRadiusSummary     `json:"blast_radius"`
	PlanPayload             map[string]interface{} `json:"plan_payload"`
	RollbackStrategy        map[string]interface{} `json:"rollback_strategy"`
	CreatedBy               string                 `json:"created_by"`
	ApprovedBy              *string                `json:"approved_by,omitempty"`
	ApprovedAt              *time.Time             `json:"approved_at,omitempty"`
	RejectionReason         string                 `json:"rejection_reason,omitempty"`
	CreatedAt               time.Time              `json:"created_at"`
	UpdatedAt               time.Time              `json:"updated_at"`
}

// ExecutionStep represents an individual step in the safe remediation execution pipeline.
type ExecutionStep string

const (
	StepPreFlight ExecutionStep = "pre_flight"
	StepExecute   ExecutionStep = "execute"
	StepVerify    ExecutionStep = "verify"
	StepRollback  ExecutionStep = "rollback"
)

// ActionExecution records a single execution attempt of an approved plan.
type ActionExecution struct {
	ID           uuid.UUID              `json:"id"`
	TenantID     uuid.UUID              `json:"tenant_id"`
	ActionPlanID uuid.UUID              `json:"action_plan_id"`
	Step         ExecutionStep          `json:"step"`
	Status       string                 `json:"status"` // "in_progress", "completed", "failed"
	OutputLog    string                 `json:"output_log,omitempty"`
	ErrorDetails map[string]interface{} `json:"error_details,omitempty"`
	StartedAt    time.Time              `json:"started_at"`
	CompletedAt  *time.Time             `json:"completed_at,omitempty"`
}

// Validate checks action plan sanity.
func (p *ActionPlan) Validate() error {
	if p.TenantID == uuid.Nil {
		return errors.New("tenant ID is required")
	}
	if p.Title == "" {
		return errors.New("action plan title is required")
	}
	if p.ActionType == "" {
		return errors.New("action type is required")
	}
	if p.RiskLevel == "" {
		return errors.New("risk level is required")
	}
	if p.CreatedBy == "" {
		return errors.New("created_by actor is required")
	}
	if p.RollbackStrategy == nil {
		return errors.New("rollback strategy is mandatory for every action plan")
	}
	return nil
}
