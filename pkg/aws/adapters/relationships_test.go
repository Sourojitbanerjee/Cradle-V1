package adapters

import (
	"testing"
	"time"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

func TestRelationshipValidationAndProvenance(t *testing.T) {
	tenantID := uuid.New()
	srcID := uuid.New()
	targetID := uuid.New()
	now := time.Now().UTC()

	tests := []struct {
		name       string
		rel        domain.ResourceRelationship
		shouldFail bool
	}{
		{
			name: "Valid observed relationship",
			rel: domain.ResourceRelationship{
				ID:               uuid.New(),
				TenantID:         tenantID,
				SourceResourceID: srcID,
				TargetResourceID: targetID,
				RelationshipType: domain.RelAttachedTo,
				Confidence:       1.0,
				Provenance:       domain.ProvenanceObserved,
				DiscoveredAt:     now,
				UpdatedAt:        now,
			},
			shouldFail: false,
		},
		{
			name: "Valid tag-inferred relationship",
			rel: domain.ResourceRelationship{
				ID:               uuid.New(),
				TenantID:         tenantID,
				SourceResourceID: srcID,
				TargetResourceID: targetID,
				RelationshipType: domain.RelManages,
				Confidence:       0.95,
				Provenance:       domain.ProvenanceObservedTag,
				DiscoveredAt:     now,
				UpdatedAt:        now,
			},
			shouldFail: false,
		},
		{
			name: "Valid targets relationship",
			rel: domain.ResourceRelationship{
				ID:               uuid.New(),
				TenantID:         tenantID,
				SourceResourceID: srcID,
				TargetResourceID: targetID,
				RelationshipType: domain.RelTargets,
				Confidence:       1.0,
				Provenance:       domain.ProvenanceObserved,
				DiscoveredAt:     now,
				UpdatedAt:        now,
			},
			shouldFail: false,
		},
		{
			name: "Invalid confidence above 1.0",
			rel: domain.ResourceRelationship{
				ID:               uuid.New(),
				TenantID:         tenantID,
				SourceResourceID: srcID,
				TargetResourceID: targetID,
				RelationshipType: domain.RelAttachedTo,
				Confidence:       1.5,
				Provenance:       domain.ProvenanceObserved,
				DiscoveredAt:     now,
				UpdatedAt:        now,
			},
			shouldFail: true,
		},
		{
			name: "Invalid confidence below 0.0",
			rel: domain.ResourceRelationship{
				ID:               uuid.New(),
				TenantID:         tenantID,
				SourceResourceID: srcID,
				TargetResourceID: targetID,
				RelationshipType: domain.RelAttachedTo,
				Confidence:       -0.1,
				Provenance:       domain.ProvenanceObserved,
				DiscoveredAt:     now,
				UpdatedAt:        now,
			},
			shouldFail: true,
		},
		{
			name: "Missing source resource ID",
			rel: domain.ResourceRelationship{
				ID:               uuid.New(),
				TenantID:         tenantID,
				SourceResourceID: uuid.Nil,
				TargetResourceID: targetID,
				RelationshipType: domain.RelAttachedTo,
				Confidence:       1.0,
				Provenance:       domain.ProvenanceObserved,
				DiscoveredAt:     now,
				UpdatedAt:        now,
			},
			shouldFail: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.rel.Validate()
			if tc.shouldFail && err == nil {
				t.Fatalf("expected validation error for test %q, but got nil", tc.name)
			}
			if !tc.shouldFail && err != nil {
				t.Fatalf("unexpected validation error for test %q: %v", tc.name, err)
			}
		})
	}
}
