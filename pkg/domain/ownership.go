package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// OwnerType specifies the entity class responsible for a resource.
type OwnerType string

const (
	OwnerTypeUser    OwnerType = "user"
	OwnerTypeTeam    OwnerType = "team"
	OwnerTypeService OwnerType = "service"
)

// OwnershipSource indicates the discovery mechanism through which ownership was identified.
type OwnershipSource string

const (
	OwnershipSourceTagInferred     OwnershipSource = "tag_inferred"
	OwnershipSourceIAMCreator      OwnershipSource = "iam_creator"
	OwnershipSourceTerraformAuthor OwnershipSource = "terraform_author"
	OwnershipSourceManual          OwnershipSource = "manual"
)

// OwnershipRecord associates accountability with a cloud resource.
type OwnershipRecord struct {
	ID         uuid.UUID       `json:"id"`
	TenantID   uuid.UUID       `json:"tenant_id"`
	ResourceID uuid.UUID       `json:"resource_id"`
	OwnerType  OwnerType       `json:"owner_type"`
	OwnerID    string          `json:"owner_id"` // email, team slug, github team, slack channel
	Source     OwnershipSource `json:"source"`
	Confidence float64         `json:"confidence"` // 0.00 to 1.00
	IsVerified bool            `json:"is_verified"`
	VerifiedBy string          `json:"verified_by,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// Validate checks ownership record validity.
func (o *OwnershipRecord) Validate() error {
	if o.TenantID == uuid.Nil {
		return errors.New("tenant ID is required")
	}
	if o.ResourceID == uuid.Nil {
		return errors.New("resource ID is required")
	}
	if o.OwnerType == "" {
		return errors.New("owner type is required")
	}
	if o.OwnerID == "" {
		return errors.New("owner ID is required")
	}
	if o.Confidence < 0.0 || o.Confidence > 1.0 {
		return errors.New("confidence must be between 0.00 and 1.00")
	}
	return nil
}
