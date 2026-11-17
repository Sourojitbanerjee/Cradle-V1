package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cradle-dev/cradle/pkg/crypto"
	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
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
