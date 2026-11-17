package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/provider"
	"github.com/google/uuid"
)

type mockDiscoveryProvider struct {
	failRegion string
}

func (m *mockDiscoveryProvider) Name() domain.ProviderType {
	return domain.ProviderAWS
}

func (m *mockDiscoveryProvider) VerifyConnection(ctx context.Context, account *domain.CloudAccount) (*provider.ConnectionVerificationResult, error) {
	return &provider.ConnectionVerificationResult{
		Connected:         true,
		AccountID:         account.AccountID,
		CallerARN:         "arn:aws:iam::123456789012:role/CradleControlPlaneRole",
		DiscoveredRegions: []string{"us-east-1", "eu-west-1"},
	}, nil
}

func (m *mockDiscoveryProvider) DiscoverRegions(ctx context.Context, account *domain.CloudAccount) ([]string, error) {
	return []string{"us-east-1", "eu-west-1"}, nil
}

func (m *mockDiscoveryProvider) DiscoverResources(ctx context.Context, req provider.DiscoveryRequest) (*provider.DiscoveryResult, error) {
	result := &provider.DiscoveryResult{
		JobID:             req.JobID,
		AccountID:         req.Account.AccountID,
		DiscoveredRegions: req.Regions,
		StartedAt:         time.Now().UTC(),
	}

	for _, region := range req.Regions {
		if region == m.failRegion {
			result.Errors = append(result.Errors, provider.ServiceError{
				Region:    region,
				Service:   "ec2",
				Code:      "ThrottlingException",
				Message:   "Rate exceeded in " + region,
				Retryable: true,
			})
			continue
		}

		// Discovered EC2 instance
		inst := domain.ResourceEntity{
			ID:             uuid.New(),
			TenantID:       req.TenantID,
			CloudAccountID: req.Account.ID,
			Provider:       domain.ProviderAWS,
			AccountID:      req.Account.AccountID,
			Region:         region,
			ResourceType:   "aws:ec2:instance",
			NativeID:       "i-mock-" + region,
			ARN:            "arn:aws:ec2:" + region + ":" + req.Account.AccountID + ":instance/i-mock",
			Name:           "mock-server",
			Status:         domain.ResourceStatusRunning,
			ObservedAt:     time.Now().UTC(),
			ObservedState: map[string]interface{}{
				"instance_id": "i-mock-" + region,
			},
		}
		_ = inst.ComputeConfigurationHash()
		result.Entities = append(result.Entities, inst)
	}

	result.CompletedAt = time.Now().UTC()
	return result, nil
}

func TestOrchestratorDiscoveryRun(t *testing.T) {
	reg := provider.NewRegistry()
	mock := &mockDiscoveryProvider{failRegion: "eu-west-1"}
	reg.Register(mock)

	orchestrator := NewOrchestrator(nil, reg)

	account := &domain.CloudAccount{
		ID:         uuid.New(),
		TenantID:   uuid.New(),
		Provider:   domain.ProviderAWS,
		AccountID:  "123456789012",
		Name:       "Production AWS",
		RoleARN:    "arn:aws:iam::123456789012:role/CradleControlPlaneRole",
		ExternalID: "cradle-test-external-id-1234567890",
		Regions:    []string{"us-east-1", "eu-west-1"},
		Status:     domain.AccountStatusPending,
	}

	result, err := orchestrator.RunDiscovery(context.Background(), account)
	if err != nil {
		t.Fatalf("unexpected error running discovery: %v", err)
	}

	// 1. Invariant: us-east-1 should succeed with 1 entity
	if len(result.Entities) != 1 {
		t.Fatalf("expected 1 discovered entity from us-east-1, got %d", len(result.Entities))
	}
	entity := result.Entities[0]
	if entity.Region != "us-east-1" {
		t.Errorf("expected region us-east-1, got %s", entity.Region)
	}
	if entity.ResourceType != "aws:ec2:instance" {
		t.Errorf("expected resource type 'aws:ec2:instance', got %s", entity.ResourceType)
	}
	if entity.ObservedAt.IsZero() {
		t.Error("expected non-zero observed_at timestamp")
	}

	// 2. Invariant: partial failure in eu-west-1 should be captured in result.Errors without killing the job
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 partial error for eu-west-1, got %d", len(result.Errors))
	}
	if result.Errors[0].Region != "eu-west-1" {
		t.Errorf("expected error in eu-west-1, got %s", result.Errors[0].Region)
	}
	if result.Errors[0].Code != "ThrottlingException" {
		t.Errorf("expected code ThrottlingException, got %s", result.Errors[0].Code)
	}
}
