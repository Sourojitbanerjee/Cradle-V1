package provider

import (
	"time"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

// ServiceError records a partial discovery failure in an isolated region or service.
type ServiceError struct {
	Region    string `json:"region"`
	Service   string `json:"service"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// DiscoveryRequest specifies target parameters for a cloud discovery run.
type DiscoveryRequest struct {
	JobID    uuid.UUID           `json:"job_id"`
	TenantID uuid.UUID           `json:"tenant_id"`
	Account  domain.CloudAccount `json:"account"`
	Regions  []string            `json:"regions"`
	Services []string            `json:"services"`
}

// DiscoveryResult aggregates all discovered entities, synthesized relationships, and errors.
type DiscoveryResult struct {
	JobID             uuid.UUID                     `json:"job_id"`
	AccountID         string                        `json:"account_id"`
	Entities          []domain.ResourceEntity       `json:"entities"`
	Relationships     []domain.ResourceRelationship `json:"relationships"`
	DiscoveredRegions []string                      `json:"discovered_regions"`
	Errors            []ServiceError                `json:"errors"`
	StartedAt         time.Time                     `json:"started_at"`
	CompletedAt       time.Time                     `json:"completed_at"`
}

// ConnectionVerificationResult holds the response from a connection validation check.
type ConnectionVerificationResult struct {
	Connected         bool                   `json:"connected"`
	AccountID         string                 `json:"account_id"`
	CallerARN         string                 `json:"caller_arn"`
	DiscoveredRegions []string               `json:"discovered_regions"`
	Details           map[string]interface{} `json:"details"`
}
