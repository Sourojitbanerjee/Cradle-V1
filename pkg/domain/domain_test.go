package domain

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestTenantContext(t *testing.T) {
	tenantID := uuid.New()
	userID := uuid.New()

	ctx := context.Background()

	// 1. Context without tenant should return error
	_, err := TenantFromContext(ctx)
	if err == nil {
		t.Fatal("expected error when tenant context is absent, got nil")
	}

	// 2. Injected context should return exact tenant details
	tcIn := TenantContext{
		TenantID: tenantID,
		UserID:   userID,
		UserRole: RoleAdmin,
		Email:    "admin@cradle.dev",
	}
	ctx = ContextWithTenant(ctx, tcIn)

	tcOut, err := TenantFromContext(ctx)
	if err != nil {
		t.Fatalf("unexpected error retrieving tenant context: %v", err)
	}

	if tcOut.TenantID != tenantID {
		t.Errorf("expected tenant ID %s, got %s", tenantID, tcOut.TenantID)
	}
	if tcOut.UserRole != RoleAdmin {
		t.Errorf("expected role %s, got %s", RoleAdmin, tcOut.UserRole)
	}
}

func TestCloudAccountValidation(t *testing.T) {
	tenantID := uuid.New()

	tests := []struct {
		name        string
		account     CloudAccount
		expectError bool
	}{
		{
			name: "Valid AWS Account",
			account: CloudAccount{
				TenantID:   tenantID,
				Provider:   ProviderAWS,
				AccountID:  "123456789012",
				Name:       "Production-AWS",
				RoleARN:    "arn:aws:iam::123456789012:role/CradleControlPlaneRole",
				ExternalID: "cradle-test-tenant-1234567890abcdef",
				Regions:    []string{"us-east-1", "eu-west-1"},
			},
			expectError: false,
		},
		{
			name: "Invalid AWS Account ID (non-12 digits)",
			account: CloudAccount{
				TenantID:   tenantID,
				Provider:   ProviderAWS,
				AccountID:  "12345", // too short
				Name:       "Dev-AWS",
				RoleARN:    "arn:aws:iam::123456789012:role/CradleControlPlaneRole",
				ExternalID: "cradle-test-tenant-1234567890abcdef",
				Regions:    []string{"us-east-1"},
			},
			expectError: true,
		},
		{
			name: "Invalid Role ARN format",
			account: CloudAccount{
				TenantID:   tenantID,
				Provider:   ProviderAWS,
				AccountID:  "123456789012",
				Name:       "Dev-AWS",
				RoleARN:    "not-a-valid-arn",
				ExternalID: "cradle-test-tenant-1234567890abcdef",
				Regions:    []string{"us-east-1"},
			},
			expectError: true,
		},
		{
			name: "ExternalID too short for security",
			account: CloudAccount{
				TenantID:   tenantID,
				Provider:   ProviderAWS,
				AccountID:  "123456789012",
				Name:       "Dev-AWS",
				RoleARN:    "arn:aws:iam::123456789012:role/CradleControlPlaneRole",
				ExternalID: "short", // less than 16 chars
				Regions:    []string{"us-east-1"},
			},
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.account.Validate()
			if (err != nil) != tc.expectError {
				t.Errorf("expected error: %v, got: %v", tc.expectError, err)
			}
		})
	}
}

func TestResourceEntityConfigurationHash(t *testing.T) {
	tenantID := uuid.New()
	accountID := uuid.New()

	entity := ResourceEntity{
		TenantID:       tenantID,
		CloudAccountID: accountID,
		Provider:       ProviderAWS,
		AccountID:      "123456789012",
		Region:         "us-east-1",
		ResourceType:   "aws:ec2:instance",
		NativeID:       "i-0123456789abcdef0",
		ARN:            "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0",
		Status:         ResourceStatusRunning,
		Configuration: map[string]interface{}{
			"instance_type": "m5.large",
			"architecture":  "x86_64",
		},
	}

	if err := entity.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	if err := entity.ComputeConfigurationHash(); err != nil {
		t.Fatalf("failed to compute config hash: %v", err)
	}

	if entity.ConfigurationHash == "" || len(entity.ConfigurationHash) != 64 {
		t.Errorf("expected 64-character SHA-256 hash, got '%s'", entity.ConfigurationHash)
	}

	// Verify idempotency
	hash1 := entity.ConfigurationHash
	_ = entity.ComputeConfigurationHash()
	if entity.ConfigurationHash != hash1 {
		t.Errorf("expected deterministic hash computation, got %s != %s", hash1, entity.ConfigurationHash)
	}
}

func TestResourceRelationshipValidation(t *testing.T) {
	tenantID := uuid.New()
	resA := uuid.New()
	resB := uuid.New()

	// 1. Valid relationship
	rel := ResourceRelationship{
		TenantID:         tenantID,
		SourceResourceID: resA,
		TargetResourceID: resB,
		RelationshipType: RelAttachedTo,
	}
	if err := rel.Validate(); err != nil {
		t.Fatalf("unexpected error for valid relationship: %v", err)
	}

	// 2. Disallow self-referencing relationship
	relSelf := ResourceRelationship{
		TenantID:         tenantID,
		SourceResourceID: resA,
		TargetResourceID: resA,
		RelationshipType: RelDependsOn,
	}
	if err := relSelf.Validate(); err == nil {
		t.Fatal("expected error for self-referencing relationship, got nil")
	}
}
