package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cradle-dev/cradle/pkg/discovery"
	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/provider"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DiscoveryHandler struct {
	pool         *pgxpool.Pool
	orchestrator *discovery.Orchestrator
	registry     *provider.Registry
}

func NewDiscoveryHandler(pool *pgxpool.Pool, orchestrator *discovery.Orchestrator, registry *provider.Registry) *DiscoveryHandler {
	if registry == nil {
		registry = provider.DefaultRegistry
	}
	return &DiscoveryHandler{
		pool:         pool,
		orchestrator: orchestrator,
		registry:     registry,
	}
}

// VerifyConnection handles POST /api/v1/accounts/{id}/verify.
func (h *DiscoveryHandler) VerifyConnection(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		http.Error(w, `{"error": "database connection is required for cloud account operations"}`, http.StatusServiceUnavailable)
		return
	}

	tc, err := domain.TenantFromContext(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	accIDStr := chi.URLParam(r, "id")
	accID, err := uuid.Parse(accIDStr)
	if err != nil {
		http.Error(w, `{"error": "invalid account ID"}`, http.StatusBadRequest)
		return
	}

	// Fetch account details
	account, err := h.getAccount(r.Context(), tc.TenantID, accID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "account not found: %s"}`, err.Error()), http.StatusNotFound)
		return
	}

	p, err := h.registry.Get(account.Provider)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	result, err := p.VerifyConnection(r.Context(), account)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"connected": false,
			"error":     err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// TriggerSync handles POST /api/v1/accounts/{id}/sync.
func (h *DiscoveryHandler) TriggerSync(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		http.Error(w, `{"error": "database connection is required for cloud account operations"}`, http.StatusServiceUnavailable)
		return
	}

	tc, err := domain.TenantFromContext(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	accIDStr := chi.URLParam(r, "id")
	accID, err := uuid.Parse(accIDStr)
	if err != nil {
		http.Error(w, `{"error": "invalid account ID"}`, http.StatusBadRequest)
		return
	}

	account, err := h.getAccount(r.Context(), tc.TenantID, accID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "account not found: %s"}`, err.Error()), http.StatusNotFound)
		return
	}

	result, err := h.orchestrator.RunDiscovery(r.Context(), account)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "failed",
			"error":  err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"job_id":           result.JobID,
		"status":           "completed",
		"discovered_count": len(result.Entities),
		"error_count":      len(result.Errors),
		"started_at":       result.StartedAt,
		"completed_at":     result.CompletedAt,
	})
}

func (h *DiscoveryHandler) getAccount(ctx context.Context, tenantID, accountID uuid.UUID) (*domain.CloudAccount, error) {
	if h.pool == nil {
		return nil, fmt.Errorf("database connection is required for account operations")
	}

	query := `
		SELECT id, tenant_id, provider, account_id, name, role_arn, external_id, regions, status, last_discovered_at
		FROM cloud_accounts
		WHERE tenant_id = $1 AND id = $2;
	`
	row := h.pool.QueryRow(ctx, query, tenantID, accountID)

	var a domain.CloudAccount
	var providerStr string
	err := row.Scan(
		&a.ID, &a.TenantID, &providerStr, &a.AccountID, &a.Name, &a.RoleARN,
		&a.ExternalID, &a.Regions, &a.Status, &a.LastDiscoveredAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("account not found")
		}
		return nil, err
	}
	a.Provider = domain.ProviderType(providerStr)
	return &a, nil
}
