package adapters

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

func TestParseEC2Tags(t *testing.T) {
	tags := []ec2Types.Tag{
		{Key: aws.String("Environment"), Value: aws.String("Production")},
		{Key: aws.String("Owner"), Value: aws.String("PlatformTeam")},
		{Key: nil, Value: aws.String("Invalid")}, // Should be ignored
	}

	result := parseEC2Tags(tags)
	if len(result) != 2 {
		t.Fatalf("expected 2 valid tags, got %d", len(result))
	}
	if result["Environment"] != "Production" {
		t.Errorf("expected Environment='Production', got '%s'", result["Environment"])
	}
	if result["Owner"] != "PlatformTeam" {
		t.Errorf("expected Owner='PlatformTeam', got '%s'", result["Owner"])
	}
}

func TestNormalizeInstanceStatus(t *testing.T) {
	tests := []struct {
		stateName ec2Types.InstanceStateName
		expected  domain.ResourceStatus
	}{
		{ec2Types.InstanceStateNameRunning, domain.ResourceStatusRunning},
		{ec2Types.InstanceStateNameStopped, domain.ResourceStatusStopped},
		{ec2Types.InstanceStateNameTerminated, domain.ResourceStatusTerminated},
		{ec2Types.InstanceStateNamePending, domain.ResourceStatusUnknown},
	}

	for _, tc := range tests {
		status := normalizeInstanceStatus(&ec2Types.InstanceState{Name: tc.stateName})
		if status != tc.expected {
			t.Errorf("for state '%s', expected '%s', got '%s'", tc.stateName, tc.expected, status)
		}
	}
}

func TestProvenanceSeparation(t *testing.T) {
	entity := domain.ResourceEntity{
		ID:             uuid.New(),
		TenantID:       uuid.New(),
		CloudAccountID: uuid.New(),
		Provider:       domain.ProviderAWS,
		AccountID:      "123456789012",
		Region:         "us-east-1",
		ResourceType:   "aws:ec2:instance",
		NativeID:       "i-0123456789abcdef0",
		ARN:            "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0",
		Status:         domain.ResourceStatusRunning,
		ObservedAt:     time.Now().UTC(),
		ObservedState: map[string]interface{}{
			"state":         "running",
			"instance_type": "m5.xlarge",
		},
		DerivedState: map[string]interface{}{
			"is_vpc_attached": true,
		},
		InferredState: map[string]interface{}{
			"idle_confidence": 0.85,
			"inferred_owner":  "team-data",
		},
	}

	if err := entity.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	if err := entity.ComputeConfigurationHash(); err != nil {
		t.Fatalf("failed computing hash: %v", err)
	}

	// Verify that configuration hash is computed strictly from ObservedState, NOT InferredState
	hashBefore := entity.ConfigurationHash

	// Mutating inference state must NOT change configuration hash (drift detection invariant!)
	entity.InferredState["idle_confidence"] = 0.99
	_ = entity.ComputeConfigurationHash()

	if entity.ConfigurationHash != hashBefore {
		t.Fatalf("ConfigurationHash must depend strictly on observed facts, not inferences! %s != %s",
			hashBefore, entity.ConfigurationHash)
	}
}
