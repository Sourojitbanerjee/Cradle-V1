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

// ResourceEntity represents a canonical, provider-agnostic infrastructure resource.
type ResourceEntity struct {
	ID                  uuid.UUID              `json:"id"`
	TenantID            uuid.UUID              `json:"tenant_id"`
	CloudAccountID      uuid.UUID              `json:"cloud_account_id"`
	Provider            ProviderType           `json:"provider"`
	AccountID           string                 `json:"account_id"`
	Region              string                 `json:"region"`
	ResourceType        string                 `json:"resource_type"` // e.g. "aws:ec2:instance", "aws:s3:bucket"
	NativeID            string                 `json:"native_id"`     // e.g. "i-0123456789abcdef0"
	ARN                 string                 `json:"arn"`           // Full cloud ARN/URI
	Name                string                 `json:"name"`
	Status              ResourceStatus         `json:"status"`
	Configuration       map[string]interface{} `json:"configuration"`
	RawPayload          map[string]interface{} `json:"raw_payload,omitempty"`
	ConfigurationHash   string                 `json:"configuration_hash"`
	MonthlyCostEstimate float64                `json:"monthly_cost_estimate"`
	CreatedAt           time.Time              `json:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at"`
	DeletedAt           *time.Time             `json:"deleted_at,omitempty"`
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
	return nil
}

// ComputeConfigurationHash computes a deterministic SHA-256 hash of the normalized configuration.
// This enables O(1) drift detection by comparing current configuration hash with previous snapshot.
func (r *ResourceEntity) ComputeConfigurationHash() error {
	if r.Configuration == nil {
		r.Configuration = make(map[string]interface{})
	}
	data, err := json.Marshal(r.Configuration)
	if err != nil {
		return fmt.Errorf("failed to marshal configuration for hashing: %w", err)
	}
	hash := sha256.Sum256(data)
	r.ConfigurationHash = hex.EncodeToString(hash[:])
	return nil
}
