package provider

import (
	"context"

	"github.com/cradle-dev/cradle/pkg/domain"
)

// Provider abstracts cloud infrastructure providers (AWS, GCP, Azure).
// Principle: Keep provider-specific SDK logic behind this boundary.
type Provider interface {
	// Name returns the provider identifier (e.g. "aws", "gcp").
	Name() domain.ProviderType

	// VerifyConnection performs STS credential assumption and caller identity verification.
	VerifyConnection(ctx context.Context, account *domain.CloudAccount) (*ConnectionVerificationResult, error)

	// DiscoverRegions returns all enabled geographic regions for the account.
	DiscoverRegions(ctx context.Context, account *domain.CloudAccount) ([]string, error)

	// DiscoverResources executes read-only resource enumeration across specified regions and services.
	DiscoverResources(ctx context.Context, req DiscoveryRequest) (*DiscoveryResult, error)
}
