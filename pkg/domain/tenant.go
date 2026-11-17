package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// TenantStatus represents the operational status of a customer tenant.
type TenantStatus string

const (
	TenantStatusActive    TenantStatus = "active"
	TenantStatusSuspended TenantStatus = "suspended"
	TenantStatusPending   TenantStatus = "pending"
)

// PlanTier indicates the subscription and feature access tier.
type PlanTier string

const (
	PlanTierStandard   PlanTier = "standard"
	PlanTierEnterprise PlanTier = "enterprise"
)

// Tenant represents an isolated customer organization within Cradle.
type Tenant struct {
	ID        uuid.UUID    `json:"id"`
	Name      string       `json:"name"`
	Slug      string       `json:"slug"`
	PlanTier  PlanTier     `json:"plan_tier"`
	Status    TenantStatus `json:"status"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// UserRole defines RBAC roles within a tenant.
type UserRole string

const (
	RoleViewer   UserRole = "viewer"
	RoleOperator UserRole = "operator"
	RoleApprover UserRole = "approver"
	RoleAdmin    UserRole = "admin"
	RoleOwner    UserRole = "owner"
)

// TenantUser represents a user membership within a tenant.
type TenantUser struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      UserRole  `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// tenantContextKey is an unexported type to prevent key collisions in context.
type tenantContextKey struct{}

// TenantContext carries authenticated tenant and user context across request lifecycles.
type TenantContext struct {
	TenantID uuid.UUID
	UserID   uuid.UUID
	UserRole UserRole
	Email    string
}

// ContextWithTenant injects TenantContext into a context.Context.
func ContextWithTenant(ctx context.Context, tc TenantContext) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tc)
}

// TenantFromContext extracts TenantContext from a context.Context.
func TenantFromContext(ctx context.Context) (TenantContext, error) {
	val := ctx.Value(tenantContextKey{})
	if val == nil {
		return TenantContext{}, errors.New("no tenant context found in request context")
	}
	tc, ok := val.(TenantContext)
	if !ok {
		return TenantContext{}, errors.New("invalid tenant context type in request context")
	}
	if tc.TenantID == uuid.Nil {
		return TenantContext{}, errors.New("tenant ID cannot be nil")
	}
	return tc, nil
}
