package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const Unknown = "UNKNOWN"

type InventoryAnswer struct {
	Value      string   `json:"value"`
	Source     string   `json:"source"`
	Confidence float64  `json:"confidence"`
	Evidence   []string `json:"evidence"`
	Candidates []string `json:"candidates"`
}

type InventoryAssignments struct {
	Owner       string `json:"owner,omitempty"`
	Team        string `json:"team,omitempty"`
	Application string `json:"application,omitempty"`
	Environment string `json:"environment,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
	Criticality string `json:"criticality,omitempty"`
}

func (a InventoryAssignments) Fields() map[string]string {
	return map[string]string{"owner": a.Owner, "team": a.Team, "application": a.Application,
		"environment": a.Environment, "purpose": a.Purpose, "criticality": a.Criticality}
}

type OwnershipRule struct {
	ID          uuid.UUID            `json:"id"`
	TenantID    uuid.UUID            `json:"tenant_id"`
	Name        string               `json:"name"`
	TagKey      string               `json:"tag_key"`
	TagValue    string               `json:"tag_value"`
	Priority    int                  `json:"priority"`
	Enabled     bool                 `json:"enabled"`
	Assignments InventoryAssignments `json:"assignments"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
}

func (r OwnershipRule) Validate() error {
	if r.TenantID == uuid.Nil {
		return errors.New("tenant ID is required")
	}
	if strings.TrimSpace(r.Name) == "" || utf8.RuneCountInString(r.Name) > 255 {
		return errors.New("name must contain 1 to 255 characters")
	}
	if strings.TrimSpace(r.TagKey) == "" || utf8.RuneCountInString(r.TagKey) > 128 {
		return errors.New("tag_key must contain 1 to 128 characters")
	}
	if strings.TrimSpace(r.TagValue) == "" || utf8.RuneCountInString(r.TagValue) > 256 {
		return errors.New("tag_value must contain 1 to 256 characters")
	}
	if r.Priority < 0 || r.Priority > 10000 {
		return errors.New("priority must be between 0 and 10000; higher values win")
	}
	count := 0
	for field, value := range r.Assignments.Fields() {
		if value == "" {
			continue
		}
		if strings.TrimSpace(value) == "" || strings.EqualFold(strings.TrimSpace(value), Unknown) || utf8.RuneCountInString(value) > 1024 {
			return fmt.Errorf("%s must be a known nonblank value of at most 1024 characters", field)
		}
		if field == "environment" && value != "production" && value != "staging" && value != "development" {
			return errors.New("environment must be production, staging, or development")
		}
		if field == "criticality" && value != "critical" && value != "high" && value != "medium" && value != "low" {
			return errors.New("criticality must be critical, high, medium, or low")
		}
		count++
	}
	if count == 0 {
		return errors.New("at least one assignment is required")
	}
	return nil
}

type TagAnalysis struct {
	UserTagCount      int      `json:"user_tag_count"`
	EmptyKeys         []string `json:"empty_keys"`
	MissingFields     []string `json:"missing_fields"`
	ConflictingFields []string `json:"conflicting_fields"`
}

type OperationalInventory struct {
	Owner        InventoryAnswer `json:"owner"`
	Team         InventoryAnswer `json:"team"`
	Application  InventoryAnswer `json:"application"`
	Environment  InventoryAnswer `json:"environment"`
	Purpose      InventoryAnswer `json:"purpose"`
	Criticality  InventoryAnswer `json:"criticality"`
	MissingOwner bool            `json:"missing_owner"`
	Untagged     bool            `json:"untagged"`
	Orphan       InventoryAnswer `json:"orphan"`
	TagAnalysis  TagAnalysis     `json:"tag_analysis"`
}
