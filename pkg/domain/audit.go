package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ActorType specifies who or what initiated an auditable event.
type ActorType string

const (
	ActorTypeUser         ActorType = "user"
	ActorTypeServiceToken ActorType = "service_token"
	ActorTypeWorker       ActorType = "worker"
	ActorTypeSystem       ActorType = "system"
)

// AuditEvent represents a single immutable event in the cryptographically chained audit log.
type AuditEvent struct {
	ID           int64                  `json:"id"`
	TenantID     uuid.UUID              `json:"tenant_id"`
	ActorID      string                 `json:"actor_id"`
	ActorType    ActorType              `json:"actor_type"`
	EventAction  string                 `json:"event_action"` // e.g. "account.connected", "plan.approved"
	TargetType   string                 `json:"target_type"`  // e.g. "account", "resource", "action_plan"
	TargetID     string                 `json:"target_id"`
	ClientIP     string                 `json:"client_ip,omitempty"`
	UserAgent    string                 `json:"user_agent,omitempty"`
	Details      map[string]interface{} `json:"details"`
	PreviousHash string                 `json:"previous_hash"` // Hash of the preceding record in the chain
	RecordHash   string                 `json:"record_hash"`   // HMAC-SHA256 of current event + previous hash
	CreatedAt    time.Time              `json:"created_at"`
}

// Validate ensures required audit event fields are populated.
func (a *AuditEvent) Validate() error {
	if a.TenantID == uuid.Nil {
		return errors.New("tenant ID is required for audit events")
	}
	if a.ActorID == "" {
		return errors.New("actor ID is required")
	}
	if a.ActorType == "" {
		return errors.New("actor type is required")
	}
	if a.EventAction == "" {
		return errors.New("event action is required")
	}
	if a.TargetType == "" {
		return errors.New("target type is required")
	}
	if a.TargetID == "" {
		return errors.New("target ID is required")
	}
	return nil
}
