package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/storage/postgres"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DiscoveryJob represents a synchronization job in the API response.
type DiscoveryJob struct {
	ID              uuid.UUID   `json:"id"`
	TenantID        uuid.UUID   `json:"tenant_id"`
	CloudAccountID  uuid.UUID   `json:"cloud_account_id"`
	Status          string      `json:"status"`
	Regions         []string    `json:"regions"`
	DiscoveredCount int         `json:"discovered_count"`
	ErrorSummary    interface{} `json:"error_summary"`
	StartedAt       time.Time   `json:"started_at"`
	CompletedAt     *time.Time  `json:"completed_at,omitempty"`
}

type JobsHandler struct {
	pool *pgxpool.Pool
}

func NewJobsHandler(pool *pgxpool.Pool) *JobsHandler {
	return &JobsHandler{pool: pool}
}

// ListDiscoveryJobs returns recent discovery jobs for the tenant.
func (h *JobsHandler) ListDiscoveryJobs(w http.ResponseWriter, r *http.Request) {
	tc, err := domain.TenantFromContext(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	if h.pool == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jobs": []DiscoveryJob{},
		})
		return
	}

	limit := parseInt(r.URL.Query().Get("limit"), 20)
	if limit > 100 {
		limit = 100
	}

	var jobs []DiscoveryJob
	err = postgres.WithTenantTx(r.Context(), h.pool, tc.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		query := `
			SELECT id, tenant_id, cloud_account_id, status, regions, discovered_count, error_summary, started_at, completed_at
			FROM discovery_jobs
			ORDER BY started_at DESC
			LIMIT $1
		`
		rows, err := tx.Query(ctx, query, limit)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var j DiscoveryJob
			if err := rows.Scan(
				&j.ID, &j.TenantID, &j.CloudAccountID, &j.Status,
				&j.Regions, &j.DiscoveredCount, &j.ErrorSummary,
				&j.StartedAt, &j.CompletedAt,
			); err != nil {
				return err
			}
			jobs = append(jobs, j)
		}
		return rows.Err()
	})

	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed to list jobs: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"jobs": jobs,
	})
}

// GetAccountStatus returns the connection status and last sync time for a specific account.
func (h *JobsHandler) GetAccountStatus(w http.ResponseWriter, r *http.Request) {
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

	if h.pool == nil {
		// Mock response for development without DB
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"account_id":         accID,
			"status":             "pending",
			"last_discovered":    nil,
			"total_resources":    0,
			"resource_breakdown": map[string]int{},
		})
		return
	}

	type accountStatus struct {
		AccountID         uuid.UUID      `json:"account_id"`
		Status            string         `json:"status"`
		LastDiscoveredAt  *time.Time     `json:"last_discovered_at"`
		TotalResources    int            `json:"total_resources"`
		ResourceBreakdown map[string]int `json:"resource_breakdown"`
	}

	var result accountStatus
	result.AccountID = accID
	result.ResourceBreakdown = make(map[string]int)

	err = postgres.WithTenantTx(r.Context(), h.pool, tc.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		// Get account status
		accountQuery := `SELECT status, last_discovered_at FROM cloud_accounts WHERE id = $1`
		if err := tx.QueryRow(ctx, accountQuery, accID).Scan(&result.Status, &result.LastDiscoveredAt); err != nil {
			if err == pgx.ErrNoRows {
				return fmt.Errorf("account not found")
			}
			return err
		}

		// Get resource breakdown by type
		breakdownQuery := `
			SELECT resource_type, COUNT(*)
			FROM resource_entities
			WHERE cloud_account_id = $1 AND deleted_at IS NULL
			GROUP BY resource_type
			ORDER BY COUNT(*) DESC
		`
		rows, err := tx.Query(ctx, breakdownQuery, accID)
		if err != nil {
			return err
		}
		defer rows.Close()

		total := 0
		for rows.Next() {
			var resType string
			var count int
			if err := rows.Scan(&resType, &count); err != nil {
				return err
			}
			result.ResourceBreakdown[resType] = count
			total += count
		}
		result.TotalResources = total
		return rows.Err()
	})

	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
