package provider

import (
	"context"
	"testing"

	"github.com/cradle-dev/cradle/pkg/domain"
)

type mockProvider struct {
	name domain.ProviderType
}

func (m *mockProvider) Name() domain.ProviderType { return m.name }
func (m *mockProvider) VerifyConnection(ctx context.Context, account *domain.CloudAccount) (*ConnectionVerificationResult, error) {
	return &ConnectionVerificationResult{Connected: true, AccountID: account.AccountID}, nil
}
func (m *mockProvider) DiscoverRegions(ctx context.Context, account *domain.CloudAccount) ([]string, error) {
	return []string{"us-east-1", "eu-west-1"}, nil
}
func (m *mockProvider) DiscoverResources(ctx context.Context, req DiscoveryRequest) (*DiscoveryResult, error) {
	return &DiscoveryResult{JobID: req.JobID, AccountID: req.Account.AccountID}, nil
}

func TestProviderRegistry(t *testing.T) {
	reg := NewRegistry()

	// 1. Unregistered provider should return error
	_, err := reg.Get(domain.ProviderAWS)
	if err == nil {
		t.Fatal("expected error for unregistered provider, got nil")
	}

	// 2. Register mock provider
	mock := &mockProvider{name: domain.ProviderAWS}
	reg.Register(mock)

	p, err := reg.Get(domain.ProviderAWS)
	if err != nil {
		t.Fatalf("unexpected error getting registered provider: %v", err)
	}
	if p.Name() != domain.ProviderAWS {
		t.Errorf("expected provider name 'aws', got '%s'", p.Name())
	}
}
