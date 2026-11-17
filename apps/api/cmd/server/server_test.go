package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/provider"
	"github.com/cradle-dev/cradle/pkg/telemetry"
	"github.com/google/uuid"
)

func newTestRouter() http.Handler {
	logger := telemetry.NewLogger(io.Discard, slog.LevelInfo)
	provider.DefaultRegistry.Register(&testAWSProvider{})
	return setupRouter(nil, logger, "123456789012")
}

type testAWSProvider struct{}

func (m *testAWSProvider) Name() domain.ProviderType { return domain.ProviderAWS }
func (m *testAWSProvider) VerifyConnection(ctx context.Context, account *domain.CloudAccount) (*provider.ConnectionVerificationResult, error) {
	return &provider.ConnectionVerificationResult{
		Connected:         true,
		AccountID:         account.AccountID,
		CallerARN:         "arn:aws:iam::123456789012:role/CradleControlPlaneRole",
		DiscoveredRegions: []string{"us-east-1"},
	}, nil
}
func (m *testAWSProvider) DiscoverRegions(ctx context.Context, account *domain.CloudAccount) ([]string, error) {
	return []string{"us-east-1"}, nil
}
func (m *testAWSProvider) DiscoverResources(ctx context.Context, req provider.DiscoveryRequest) (*provider.DiscoveryResult, error) {
	return &provider.DiscoveryResult{
		JobID:     req.JobID,
		AccountID: req.Account.AccountID,
		Entities: []domain.ResourceEntity{
			{
				ID:           uuid.New(),
				TenantID:     req.TenantID,
				Provider:     domain.ProviderAWS,
				ResourceType: "aws:ec2:instance",
				NativeID:     "i-12345",
				ARN:          "arn:aws:ec2:us-east-1:123456789012:instance/i-12345",
				Status:       domain.ResourceStatusRunning,
			},
		},
		StartedAt:   time.Now().UTC(),
		CompletedAt: time.Now().UTC(),
	}, nil
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

func TestVerifyAndSyncEndpoints(t *testing.T) {
	router := newTestRouter()
	tenantID := uuid.New()
	accountID := uuid.New()

	// 1. Test POST /api/v1/accounts/{id}/verify
	reqVerify := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/"+accountID.String()+"/verify", nil)
	reqVerify.Header.Set("X-Tenant-ID", tenantID.String())
	rrVerify := httptest.NewRecorder()
	router.ServeHTTP(rrVerify, reqVerify)

	if rrVerify.Code != http.StatusOK {
		t.Fatalf("expected status 200 for verify, got %d: %s", rrVerify.Code, rrVerify.Body.String())
	}

	var verifyResp provider.ConnectionVerificationResult
	if err := json.Unmarshal(rrVerify.Body.Bytes(), &verifyResp); err != nil {
		t.Fatalf("failed decoding verify response: %v", err)
	}
	if !verifyResp.Connected {
		t.Error("expected connected=true")
	}

	// 2. Test POST /api/v1/accounts/{id}/sync
	reqSync := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/"+accountID.String()+"/sync", nil)
	reqSync.Header.Set("X-Tenant-ID", tenantID.String())
	rrSync := httptest.NewRecorder()
	router.ServeHTTP(rrSync, reqSync)

	if rrSync.Code != http.StatusAccepted {
		t.Fatalf("expected status 202 Accepted for sync trigger, got %d: %s", rrSync.Code, rrSync.Body.String())
	}

	var syncResp map[string]interface{}
	if err := json.Unmarshal(rrSync.Body.Bytes(), &syncResp); err != nil {
		t.Fatalf("failed decoding sync response: %v", err)
	}
	if syncResp["status"] != "completed" {
		t.Errorf("expected status 'completed', got '%v'", syncResp["status"])
	}
	if count, ok := syncResp["discovered_count"].(float64); !ok || count != 1 {
		t.Errorf("expected discovered_count 1, got %v", syncResp["discovered_count"])
	}
}

func TestCostAndWasteEndpoints(t *testing.T) {
	router := newTestRouter()
	tenantID := uuid.New()

	// 1. Test GET /api/v1/cost/summary
	reqCost := httptest.NewRequest(http.MethodGet, "/api/v1/cost/summary?days=30", nil)
	reqCost.Header.Set("X-Tenant-ID", tenantID.String())
	rrCost := httptest.NewRecorder()
	router.ServeHTTP(rrCost, reqCost)

	if rrCost.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for cost summary, got %d: %s", rrCost.Code, rrCost.Body.String())
	}

	var costResp map[string]interface{}
	if err := json.Unmarshal(rrCost.Body.Bytes(), &costResp); err != nil {
		t.Fatalf("failed decoding cost response: %v", err)
	}
	if costResp["currency"] != "USD" {
		t.Errorf("expected USD currency, got %v", costResp["currency"])
	}

	// 2. Test GET /api/v1/waste/summary
	reqWasteSum := httptest.NewRequest(http.MethodGet, "/api/v1/waste/summary", nil)
	reqWasteSum.Header.Set("X-Tenant-ID", tenantID.String())
	rrWasteSum := httptest.NewRecorder()
	router.ServeHTTP(rrWasteSum, reqWasteSum)

	if rrWasteSum.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for waste summary, got %d: %s", rrWasteSum.Code, rrWasteSum.Body.String())
	}

	// 3. Test GET /api/v1/waste/findings
	reqFindings := httptest.NewRequest(http.MethodGet, "/api/v1/waste/findings", nil)
	reqFindings.Header.Set("X-Tenant-ID", tenantID.String())
	rrFindings := httptest.NewRecorder()
	router.ServeHTTP(rrFindings, reqFindings)

	if rrFindings.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for waste findings, got %d: %s", rrFindings.Code, rrFindings.Body.String())
	}

	// 4. Test POST /api/v1/waste/analyze
	reqAnalyze := httptest.NewRequest(http.MethodPost, "/api/v1/waste/analyze", nil)
	reqAnalyze.Header.Set("X-Tenant-ID", tenantID.String())
	rrAnalyze := httptest.NewRecorder()
	router.ServeHTTP(rrAnalyze, reqAnalyze)

	if rrAnalyze.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for waste analyze, got %d: %s", rrAnalyze.Code, rrAnalyze.Body.String())
	}
	var analyzeResp map[string]interface{}
	if err := json.Unmarshal(rrAnalyze.Body.Bytes(), &analyzeResp); err != nil {
		t.Fatalf("failed decoding analyze response: %v", err)
	}
	if analyzeResp["status"] != "completed" {
		t.Errorf("expected status 'completed', got %v", analyzeResp["status"])
	}
}
