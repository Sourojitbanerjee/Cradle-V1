package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ProvenanceCategory defines the epistemological origin of an infrastructure data point.
// Principle: Never allow an inference to masquerade as an AWS fact.
type ProvenanceCategory string

const (
	// ProvenanceObserved represents direct, verifiable facts received from the cloud provider API.
	ProvenanceObserved ProvenanceCategory = "OBSERVED_FROM_PROVIDER"

	// ProvenanceObservedTag represents facts declared directly via provider resource tags.
	ProvenanceObservedTag ProvenanceCategory = "OBSERVED_FROM_TAG"

	// ProvenanceDerived represents deterministic facts calculated by Cradle logic
	// (e.g., canonical resource hashing, standard state normalization, cross-resource config joins).
	ProvenanceDerived ProvenanceCategory = "DERIVED_BY_CRADLE"

	// ProvenanceInferred represents heuristic or statistical interpretations
	// (e.g., idle probabilities, ownership heuristics).
	ProvenanceInferred ProvenanceCategory = "INFERRED_BY_CRADLE"
)

// ResourceStatus represents the operational lifecycle state of a cloud resource.
type ResourceStatus string

const (
	ResourceStatusRunning    ResourceStatus = "running"
	ResourceStatusStopped    ResourceStatus = "stopped"
	ResourceStatusAvailable  ResourceStatus = "available"
	ResourceStatusInUse      ResourceStatus = "in_use"
	ResourceStatusDegraded   ResourceStatus = "degraded"
	ResourceStatusTerminated ResourceStatus = "terminated"
	ResourceStatusUnknown    ResourceStatus = "unknown"
)

// ResourceEntity represents a canonical, provider-agnostic infrastructure resource
// with explicit provenance boundaries separating observed facts from derived or inferred data.
type ResourceEntity struct {
	ID                uuid.UUID         `json:"id"`
	TenantID          uuid.UUID         `json:"tenant_id"`
	CloudAccountID    uuid.UUID         `json:"cloud_account_id"`
	Provider          ProviderType      `json:"provider"`
	AccountID         string            `json:"account_id"`
	Region            string            `json:"region"`
	ResourceType      string            `json:"resource_type"` // e.g. "aws:ec2:instance", "aws:s3:bucket"
	NativeID          string            `json:"native_id"`     // e.g. "i-0123456789abcdef0"
	ARN               string            `json:"arn"`           // Full cloud ARN/URI
	Name              string            `json:"name"`
	Status            ResourceStatus    `json:"status"`
	Tags              map[string]string `json:"tags"`
	CreatedAtProvider *time.Time        `json:"created_at_provider,omitempty"`
	ObservedAt        time.Time         `json:"observed_at"`

	// Strict Epistemological Segregation
	ObservedState map[string]interface{} `json:"observed_state"`           // OBSERVED FROM AWS
	DerivedState  map[string]interface{} `json:"derived_state"`            // DERIVED BY CRADLE
	InferredState map[string]interface{} `json:"inferred_state,omitempty"` // INFERRED BY CRADLE

	RawPayload          map[string]interface{} `json:"raw_payload,omitempty"`
	ConfigurationHash   string                 `json:"configuration_hash"`
	MonthlyCostEstimate float64                `json:"monthly_cost_estimate"`
	CreatedAt           time.Time              `json:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at"`
	DeletedAt           *time.Time             `json:"deleted_at,omitempty"`
	Operational         *OperationalInventory  `json:"operational,omitempty"`
}

// Validate checks essential invariants for a ResourceEntity.
func (r *ResourceEntity) Validate() error {
	if r.TenantID == uuid.Nil {
		return errors.New("tenant ID is required")
	}
	if r.CloudAccountID == uuid.Nil {
		return errors.New("cloud account ID is required")
	}
	if r.Provider == "" {
		return errors.New("provider is required")
	}
	if r.ResourceType == "" {
		return errors.New("resource type is required")
	}
	if !strings.Contains(r.ResourceType, ":") {
		return fmt.Errorf("invalid resource type '%s': expected format 'provider:service:type'", r.ResourceType)
	}
	if r.NativeID == "" {
		return errors.New("native ID is required")
	}
	if r.ARN == "" {
		return errors.New("ARN is required")
	}
	if r.ObservedAt.IsZero() {
		return errors.New("observed_at timestamp is required")
	}
	return nil
}

// ComputeConfigurationHash computes a deterministic SHA-256 hash of the canonical observed state.
func (r *ResourceEntity) ComputeConfigurationHash() error {
	if r.ObservedState == nil {
		r.ObservedState = make(map[string]interface{})
	}
	data, err := json.Marshal(r.ObservedState)
	if err != nil {
		return fmt.Errorf("failed to marshal observed state for hashing: %w", err)
	}
	hash := sha256.Sum256(data)
	r.ConfigurationHash = hex.EncodeToString(hash[:])
	return nil
}
