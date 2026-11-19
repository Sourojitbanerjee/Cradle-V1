package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// RelationshipType defines the directed edge semantics between infrastructure entities.
type RelationshipType string

const (
	// Structural containment (e.g., VPC contains Subnet, EKS Cluster contains NodeGroup)
	RelContains RelationshipType = "CONTAINS"

	// Attachment (e.g., EBS Volume attached to EC2 Instance, IAM Role attached to InstanceProfile)
	RelAttachedTo RelationshipType = "ATTACHED_TO"

	// Network routing (e.g., Subnet routes to RouteTable, RouteTable routes to NAT Gateway)
	RelRoutesTo RelationshipType = "ROUTES_TO"

	// Traffic security boundary (e.g., SecurityGroup allows traffic to SecurityGroup)
	RelAllowsTrafficTo RelationshipType = "ALLOWS_TRAFFIC_TO"

	// Identity delegation (e.g., ECS Task assumes IAM Role, Lambda assumes Execution Role)
	RelAssumesRole RelationshipType = "ASSUMES_ROLE"

	// Cryptographic protection (e.g., RDS Instance encrypted with KMS Key, S3 Bucket encrypted with KMS Key)
	RelEncryptsWith RelationshipType = "ENCRYPTS_WITH"

	// Generic dependency
	RelDependsOn RelationshipType = "DEPENDS_ON"

	// Orchestration/Management (e.g., AutoScalingGroup manages EC2 Instance, EKS NodeGroup manages EC2 Instance)
	RelManages RelationshipType = "MANAGES"

	// Traffic distribution / targeting (e.g., TargetGroup targets EC2 Instance or ECS Task)
	RelTargets RelationshipType = "TARGETS"
)

// ResourceRelationship represents a directed edge in the infrastructure dependency graph.
type ResourceRelationship struct {
	ID               uuid.UUID              `json:"id"`
	TenantID         uuid.UUID              `json:"tenant_id"`
	SourceResourceID uuid.UUID              `json:"source_resource_id"`
	TargetResourceID uuid.UUID              `json:"target_resource_id"`
	TargetARN        string                 `json:"target_arn,omitempty"`       // Unresolved candidate target ARN from provider
	TargetNativeID   string                 `json:"target_native_id,omitempty"` // Unresolved candidate native ID (e.g. subnet-xxx)
	RelationshipType RelationshipType       `json:"relationship_type"`
	Properties       map[string]interface{} `json:"properties"`
	IsImplicit       bool                   `json:"is_implicit"` // True if inferred/synthesized by graph analysis
	Confidence       float64                `json:"confidence"`  // Epistemic certainty (0.00 to 1.00)
	Provenance       ProvenanceCategory     `json:"provenance"`  // Provenance categorization
	DiscoveredAt     time.Time              `json:"discovered_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

// Validate ensures structural invariants of the relationship edge.
func (rel *ResourceRelationship) Validate() error {
	if rel.TenantID == uuid.Nil {
		return errors.New("tenant ID is required")
	}
	if rel.SourceResourceID == uuid.Nil {
		return errors.New("source resource ID is required")
	}
	if rel.TargetResourceID == uuid.Nil {
		return errors.New("target resource ID is required")
	}
	if rel.SourceResourceID == rel.TargetResourceID {
		return errors.New("self-referencing relationships are not permitted")
	}
	if rel.RelationshipType == "" {
		return errors.New("relationship type is required")
	}
	if rel.Confidence < 0.0 || rel.Confidence > 1.0 {
		return errors.New("confidence must be between 0.0 and 1.0")
	}
	return nil
}
