package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/cradle-dev/cradle/pkg/crypto"
	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/storage/postgres"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RegisterAccountRequest struct {
	Provider  domain.ProviderType `json:"provider"`
	AccountID string              `json:"account_id"`
	Name      string              `json:"name"`
	RoleARN   string              `json:"role_arn"`
	Regions   []string            `json:"regions"`
}

type RegisterAccountResponse struct {
	Account          domain.CloudAccount `json:"account"`
	TrustPolicyJSON  string              `json:"trust_policy_json"`
	SetupInstruction string              `json:"setup_instruction"`
}

type AccountHandler struct {
	pool            *pgxpool.Pool
	cradleAWSAccNum string
}

func NewAccountHandler(pool *pgxpool.Pool, cradleAWSAccNum string) *AccountHandler {
	if cradleAWSAccNum == "" {
		cradleAWSAccNum = "000000000000" // Placeholder for development/mock mode
	}
	return &AccountHandler{
		pool:            pool,
		cradleAWSAccNum: cradleAWSAccNum,
	}
}

// GenerateExternalID generates a secure external ID for onboarding.
func (h *AccountHandler) GenerateExternalID(w http.ResponseWriter, r *http.Request) {
	tc, err := domain.TenantFromContext(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	extID, err := crypto.GenerateExternalID(tc.TenantID.String()[:8])
	if err != nil {
		http.Error(w, `{"error": "failed to generate external ID"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"external_id":       extID,
		"cradle_account_id": h.cradleAWSAccNum,
	})
}

// RegisterAccount creates a new cloud account registration with the provided ExternalId and Role ARN.
func (h *AccountHandler) RegisterAccount(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		http.Error(w, `{"error": "database connection is required"}`, http.StatusServiceUnavailable)
		return
	}

	tc, err := domain.TenantFromContext(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req RegisterAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "invalid request body: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	extID, err := crypto.GenerateExternalID(tc.TenantID.String()[:8])
	if err != nil {
		http.Error(w, `{"error": "failed to generate external ID"}`, http.StatusInternalServerError)
		return
	}

	acc := domain.CloudAccount{
		ID:         uuid.New(),
		TenantID:   tc.TenantID,
		Provider:   req.Provider,
		AccountID:  req.AccountID,
		Name:       req.Name,
		RoleARN:    req.RoleARN,
		ExternalID: extID,
		Regions:    req.Regions,
		Status:     domain.AccountStatusPending,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	if err := acc.Validate(); err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "validation failed: %s"}`, err.Error()), http.StatusUnprocessableEntity)
		return
	}

	// Persist account to database within tenant RLS transaction
	err = postgres.WithTenantTx(r.Context(), h.pool, tc.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		query := `
			INSERT INTO cloud_accounts (
				id, tenant_id, provider, account_id, name, role_arn, external_id, regions, status, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
			);
		`
		_, execErr := tx.Exec(ctx, query,
			acc.ID, acc.TenantID, string(acc.Provider), acc.AccountID, acc.Name,
			acc.RoleARN, acc.ExternalID, acc.Regions, string(acc.Status), acc.CreatedAt, acc.UpdatedAt,
		)
		return execErr
	})
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed saving cloud account: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	trustPolicy := fmt.Sprintf(`{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": { "AWS": "arn:aws:iam::%s:root" },
      "Action": "sts:AssumeRole",
      "Condition": {
        "StringEquals": { "sts:ExternalId": "%s" }
      }
    }
  ]
}`, h.cradleAWSAccNum, extID)

	resp := RegisterAccountResponse{
		Account:          acc,
		TrustPolicyJSON:  trustPolicy,
		SetupInstruction: "Deploy the Cradle read-only IAM role CloudFormation template into your AWS account with the supplied ExternalId.",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// ListAccounts handles GET /api/v1/accounts.
func (h *AccountHandler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		http.Error(w, `{"error": "database connection is required"}`, http.StatusServiceUnavailable)
		return
	}

	tc, err := domain.TenantFromContext(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var accounts []domain.CloudAccount
	err = postgres.WithTenantTx(r.Context(), h.pool, tc.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		query := `
			SELECT id, tenant_id, provider, account_id, name, role_arn, external_id, regions, status, last_discovered_at, created_at, updated_at
			FROM cloud_accounts
			ORDER BY created_at DESC;
		`
		rows, qErr := tx.Query(ctx, query)
		if qErr != nil {
			return qErr
		}
		defer rows.Close()

		for rows.Next() {
			var acc domain.CloudAccount
			var provStr string
			var statStr string
			if scanErr := rows.Scan(
				&acc.ID, &acc.TenantID, &provStr, &acc.AccountID, &acc.Name,
				&acc.RoleARN, &acc.ExternalID, &acc.Regions, &statStr,
				&acc.LastDiscoveredAt, &acc.CreatedAt, &acc.UpdatedAt,
			); scanErr != nil {
				return scanErr
			}
			acc.Provider = domain.ProviderType(provStr)
			acc.Status = domain.AccountStatus(statStr)
			accounts = append(accounts, acc)
		}
		return rows.Err()
	})
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "failed querying accounts: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	if accounts == nil {
		accounts = []domain.CloudAccount{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"total":    len(accounts),
		"accounts": accounts,
	})
}

// GetAccount handles GET /api/v1/accounts/{id}.
func (h *AccountHandler) GetAccount(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		http.Error(w, `{"error": "database connection is required"}`, http.StatusServiceUnavailable)
		return
	}

	tc, err := domain.TenantFromContext(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	accountIDStr := chi.URLParam(r, "id")
	accountID, err := uuid.Parse(accountIDStr)
	if err != nil {
		http.Error(w, `{"error": "invalid account ID"}`, http.StatusBadRequest)
		return
	}

	var acc domain.CloudAccount
	var provStr string
	var statStr string
	err = postgres.WithTenantTx(r.Context(), h.pool, tc.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		query := `
			SELECT id, tenant_id, provider, account_id, name, role_arn, external_id, regions, status, last_discovered_at, created_at, updated_at
			FROM cloud_accounts
			WHERE id = $1;
		`
		return tx.QueryRow(ctx, query, accountID).Scan(
			&acc.ID, &acc.TenantID, &provStr, &acc.AccountID, &acc.Name,
			&acc.RoleARN, &acc.ExternalID, &acc.Regions, &statStr,
			&acc.LastDiscoveredAt, &acc.CreatedAt, &acc.UpdatedAt,
		)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, `{"error": "account not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf(`{"error": "failed querying account: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	acc.Provider = domain.ProviderType(provStr)
	acc.Status = domain.AccountStatus(statStr)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(acc)
}
