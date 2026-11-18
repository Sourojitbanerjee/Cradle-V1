package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TenantHandler struct {
	pool *pgxpool.Pool
}

func NewTenantHandler(pool *pgxpool.Pool) *TenantHandler {
	return &TenantHandler{pool: pool}
}

type CreateTenantRequest struct {
	ID       *uuid.UUID      `json:"id,omitempty"`
	Name     string          `json:"name"`
	Slug     string          `json:"slug,omitempty"`
	PlanTier domain.PlanTier `json:"plan_tier,omitempty"`
}

var nonAlphanumericRegex = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	slug := strings.ToLower(name)
	slug = nonAlphanumericRegex.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 60 {
		slug = slug[:60]
	}
	if slug == "" {
		slug = "tenant-" + uuid.New().String()[:8]
	}
	return slug
}

// CreateTenant handles POST /api/v1/tenants.
func (h *TenantHandler) CreateTenant(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		http.Error(w, `{"error": "database connection is required"}`, http.StatusServiceUnavailable)
		return
	}

	var req CreateTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "invalid request body: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, `{"error": "tenant name is required"}`, http.StatusBadRequest)
		return
	}

	tenantID := uuid.New()
	if req.ID != nil && *req.ID != uuid.Nil {
		tenantID = *req.ID
	}

	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = slugify(req.Name)
	}

	planTier := req.PlanTier
	if planTier == "" {
		planTier = domain.PlanTierStandard
	}

	now := time.Now().UTC()
	query := `
		INSERT INTO tenants (id, name, slug, plan_tier, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'active', $5, $5)
		RETURNING id, name, slug, plan_tier, status, created_at, updated_at;
	`

	var t domain.Tenant
	var planTierStr string
	var statusStr string

	err := h.pool.QueryRow(r.Context(), query, tenantID, req.Name, slug, string(planTier), now).Scan(
		&t.ID, &t.Name, &t.Slug, &planTierStr, &statusStr, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed creating tenant: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	t.PlanTier = domain.PlanTier(planTierStr)
	t.Status = domain.TenantStatus(statusStr)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(t)
}

// GetTenant handles GET /api/v1/tenants/{id}.
func (h *TenantHandler) GetTenant(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		http.Error(w, `{"error": "database connection is required"}`, http.StatusServiceUnavailable)
		return
	}

	idStr := chi.URLParam(r, "id")
	tenantID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, `{"error": "invalid tenant ID"}`, http.StatusBadRequest)
		return
	}

	query := `
		SELECT id, name, slug, plan_tier, status, created_at, updated_at
		FROM tenants
		WHERE id = $1;
	`

	var t domain.Tenant
	var planTierStr string
	var statusStr string

	err = h.pool.QueryRow(r.Context(), query, tenantID).Scan(
		&t.ID, &t.Name, &t.Slug, &planTierStr, &statusStr, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, `{"error": "tenant not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error": "failed querying tenant: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	t.PlanTier = domain.PlanTier(planTierStr)
	t.Status = domain.TenantStatus(statusStr)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(t)
}

// ListTenants handles GET /api/v1/tenants.
func (h *TenantHandler) ListTenants(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		http.Error(w, `{"error": "database connection is required"}`, http.StatusServiceUnavailable)
		return
	}

	query := `
		SELECT id, name, slug, plan_tier, status, created_at, updated_at
		FROM tenants
		ORDER BY created_at DESC
		LIMIT 50;
	`

	rows, err := h.pool.Query(r.Context(), query)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed listing tenants: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	tenants := make([]domain.Tenant, 0)
	for rows.Next() {
		var t domain.Tenant
		var planTierStr string
		var statusStr string

		if err := rows.Scan(
			&t.ID, &t.Name, &t.Slug, &planTierStr, &statusStr, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			http.Error(w, fmt.Sprintf(`{"error": "failed reading tenants: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		t.PlanTier = domain.PlanTier(planTierStr)
		t.Status = domain.TenantStatus(statusStr)
		tenants = append(tenants, t)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"total":   len(tenants),
		"tenants": tenants,
	})
}
