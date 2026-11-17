package domain

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

// ProviderType identifies the cloud infrastructure provider.
type ProviderType string

const (
	ProviderAWS   ProviderType = "aws"
	ProviderGCP   ProviderType = "gcp"
	ProviderAzure ProviderType = "azure"
)

// AccountStatus represents the connectivity and health status of a cloud account.
type AccountStatus string

const (
	AccountStatusPending      AccountStatus = "pending"
	AccountStatusConnected    AccountStatus = "connected"
	AccountStatusError        AccountStatus = "error"
	AccountStatusUnauthorized AccountStatus = "unauthorized"
)

// AWS validation patterns.
var (
	awsAccountIDRegex = regexp.MustCompile(`^[0-9]{12}$`)
	awsRoleARNRegex   = regexp.MustCompile(`^arn:aws:iam::[0-9]{12}:role/[a-zA-Z0-9+=,.@_\-\/]+$`)
)

// CloudAccount represents a customer's cloud provider account registered with Cradle.
type CloudAccount struct {
	ID               uuid.UUID     `json:"id"`
	TenantID         uuid.UUID     `json:"tenant_id"`
	Provider         ProviderType  `json:"provider"`
	AccountID        string        `json:"account_id"` // AWS 12-digit ID, GCP Project ID, or Azure Subscription ID
	Name             string        `json:"name"`
	RoleARN          string        `json:"role_arn"`
	ExternalID       string        `json:"external_id"`
	Regions          []string      `json:"regions"`
	Status           AccountStatus `json:"status"`
	LastDiscoveredAt *time.Time    `json:"last_discovered_at,omitempty"`
	ErrorMessage     string        `json:"error_message,omitempty"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
}

// Validate validates cloud account connection parameters.
func (a *CloudAccount) Validate() error {
	if a.TenantID == uuid.Nil {
		return errors.New("tenant ID is required")
	}
	if a.Name == "" {
		return errors.New("account name is required")
	}

	switch a.Provider {
	case ProviderAWS:
		if !awsAccountIDRegex.MatchString(a.AccountID) {
			return fmt.Errorf("invalid AWS account ID '%s': must be exactly 12 digits", a.AccountID)
		}
		if !awsRoleARNRegex.MatchString(a.RoleARN) {
			return fmt.Errorf("invalid AWS IAM Role ARN '%s'", a.RoleARN)
		}
		if len(a.ExternalID) < 16 {
			return errors.New("external ID must be at least 16 characters for security")
		}
		if len(a.Regions) == 0 {
			return errors.New("at least one target AWS region must be configured")
		}
	case ProviderGCP, ProviderAzure:
		return fmt.Errorf("provider '%s' is not yet supported in Phase 0/1", a.Provider)
	default:
		return fmt.Errorf("unknown provider type '%s'", a.Provider)
	}

	return nil
}
