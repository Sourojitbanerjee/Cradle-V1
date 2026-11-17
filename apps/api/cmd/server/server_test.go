package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/telemetry"
	"github.com/google/uuid"
)

func newTestRouter() http.Handler {
	logger := telemetry.NewLogger(io.Discard, slog.LevelInfo)
	return setupRouter(nil, logger, "123456789012")
}

func TestHealthEndpoints(t *testing.T) {
	router := newTestRouter()

	// 1. Test /healthz
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var healthResp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &healthResp); err != nil {
		t.Fatalf("failed to decode healthz response: %v", err)
	}
	if healthResp["status"] != "ok" {
		t.Errorf("expected status 'ok', got '%v'", healthResp["status"])
	}

	// 2. Test /readyz (no db configured in test, should return ready with disabled db)
	reqReady := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rrReady := httptest.NewRecorder()
	router.ServeHTTP(rrReady, reqReady)

	if rrReady.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rrReady.Code)
	}
}

func TestTenantMiddlewareEnforcement(t *testing.T) {
	router := newTestRouter()

	// 1. Missing X-Tenant-ID header should fail with 400
	req := httptest.NewRequest(http.MethodGet, "/api/v1/resources", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for missing tenant header, got %d", rr.Code)
	}

	// 2. Invalid UUID format in X-Tenant-ID should fail with 400
	reqInvalid := httptest.NewRequest(http.MethodGet, "/api/v1/resources", nil)
	reqInvalid.Header.Set("X-Tenant-ID", "not-a-uuid")
	rrInvalid := httptest.NewRecorder()
	router.ServeHTTP(rrInvalid, reqInvalid)

	if rrInvalid.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid tenant UUID, got %d", rrInvalid.Code)
	}
}

func TestAccountRegistrationFlow(t *testing.T) {
	router := newTestRouter()
	tenantID := uuid.New()

	// 1. Request ExternalId generation
	reqExt := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/external-id", nil)
	reqExt.Header.Set("X-Tenant-ID", tenantID.String())
	rrExt := httptest.NewRecorder()
	router.ServeHTTP(rrExt, reqExt)

	if rrExt.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rrExt.Code)
	}

	var extResp map[string]string
	_ = json.Unmarshal(rrExt.Body.Bytes(), &extResp)
	if !strings.HasPrefix(extResp["external_id"], "cradle-") {
		t.Errorf("expected ExternalID prefix 'cradle-', got '%s'", extResp["external_id"])
	}

	// 2. Register valid AWS Account
	regPayload := map[string]interface{}{
		"provider":   "aws",
		"account_id": "987654321098",
		"name":       "Production AWS Account",
		"role_arn":   "arn:aws:iam::987654321098:role/CradleControlPlaneRole",
		"regions":    []string{"us-east-1", "us-west-2"},
	}
	bodyBytes, _ := json.Marshal(regPayload)

	reqReg := httptest.NewRequest(http.MethodPost, "/api/v1/accounts", bytes.NewReader(bodyBytes))
	reqReg.Header.Set("X-Tenant-ID", tenantID.String())
	reqReg.Header.Set("Content-Type", "application/json")
	rrReg := httptest.NewRecorder()
	router.ServeHTTP(rrReg, reqReg)

	if rrReg.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created, got %d: %s", rrReg.Code, rrReg.Body.String())
	}

	var regResp map[string]interface{}
	_ = json.Unmarshal(rrReg.Body.Bytes(), &regResp)

	// Verify trust policy was generated with the correct Cradle Account ID
	trustPolicy, ok := regResp["trust_policy_json"].(string)
	if !ok || !strings.Contains(trustPolicy, "123456789012") {
		t.Errorf("expected trust policy to contain Cradle AWS Account 123456789012, got: %s", trustPolicy)
	}
}

func TestResourceListEndpoint(t *testing.T) {
	router := newTestRouter()
	tenantID := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/resources", nil)
	req.Header.Set("X-Tenant-ID", tenantID.String())
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp struct {
		Total     int                     `json:"total"`
		Resources []domain.ResourceEntity `json:"resources"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Total != 0 || len(resp.Resources) != 0 {
		t.Errorf("expected empty resource list, got total %d", resp.Total)
	}
}
