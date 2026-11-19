package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/provider"
	"github.com/cradle-dev/cradle/pkg/storage/postgres"
	"github.com/cradle-dev/cradle/pkg/telemetry"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ = uuid.MustParse("a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11")

func getTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://cradle_user:cradle_secure_password_dev@localhost:5433/cradle_db?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := postgres.NewPool(ctx, postgres.Config{URL: dbURL})
	if err != nil {
		t.Logf("PostgreSQL not available for integration tests (%v); running in mock/nil mode", err)
		return nil
	}

	migrator := postgres.NewMigrator(pool)
	if err := migrator.Up(ctx); err != nil {
		t.Logf("Failed running test migrations (%v)", err)
	}

	return pool
}

func newTestRouter(pool *pgxpool.Pool) http.Handler {
	logger := telemetry.NewLogger(io.Discard, slog.LevelInfo)
	provider.DefaultRegistry.Register(&testAWSProvider{})
	return setupRouter(pool, logger, "123456789012")
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
	instID := uuid.New()
	subnetID := uuid.New()
	return &provider.DiscoveryResult{
		JobID:     req.JobID,
		AccountID: req.Account.AccountID,
		Entities: []domain.ResourceEntity{
			{
				ID:             instID,
				TenantID:       req.TenantID,
				CloudAccountID: req.Account.ID,
				Provider:       domain.ProviderAWS,
				ResourceType:   "aws:ec2:instance",
				NativeID:       "i-1234567890abcdef0",
				ARN:            "arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890abcdef0",
				Status:         domain.ResourceStatusRunning,
				Region:         "us-east-1",
				AccountID:      req.Account.AccountID,
				ObservedAt:     time.Now().UTC(),
			},
			{
				ID:             subnetID,
				TenantID:       req.TenantID,
				CloudAccountID: req.Account.ID,
				Provider:       domain.ProviderAWS,
				ResourceType:   "aws:ec2:subnet",
				NativeID:       "subnet-12345678",
				ARN:            "arn:aws:ec2:us-east-1:123456789012:subnet/subnet-12345678",
				Status:         domain.ResourceStatusAvailable,
				Region:         "us-east-1",
				AccountID:      req.Account.AccountID,
				ObservedAt:     time.Now().UTC(),
			},
		},
		Relationships: []domain.ResourceRelationship{
			{
				TenantID:         req.TenantID,
				SourceResourceID: instID,
				TargetARN:        "arn:aws:ec2:us-east-1:123456789012:subnet/subnet-12345678",
				RelationshipType: domain.RelAttachedTo,
			},
		},
		StartedAt:   time.Now().UTC(),
		CompletedAt: time.Now().UTC(),
	}, nil
}

func TestHealthEndpoints(t *testing.T) {
	router := newTestRouter(nil)

	// 1. Test / (HTML Browser Accept)
	reqHTML := httptest.NewRequest(http.MethodGet, "/", nil)
	reqHTML.Header.Set("Accept", "text/html,application/xhtml+xml")
	rrHTML := httptest.NewRecorder()
	router.ServeHTTP(rrHTML, reqHTML)

	if rrHTML.Code != http.StatusOK {
		t.Fatalf("expected status 200 for root HTML, got %d", rrHTML.Code)
	}
	if !strings.Contains(rrHTML.Body.String(), "Cradle Control Plane") {
		t.Errorf("expected HTML landing page title, got %s", rrHTML.Body.String())
	}

	// 2. Test / (JSON Accept)
	reqJSON := httptest.NewRequest(http.MethodGet, "/", nil)
	rrJSON := httptest.NewRecorder()
	router.ServeHTTP(rrJSON, reqJSON)

	if rrJSON.Code != http.StatusOK {
		t.Fatalf("expected status 200 for root JSON, got %d", rrJSON.Code)
	}

	// 3. Test /healthz
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

	// 4. Test /readyz (no db configured in test, should return 200)
	reqReady := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rrReady := httptest.NewRecorder()
	router.ServeHTTP(rrReady, reqReady)

	if rrReady.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rrReady.Code)
	}
}

func TestTenantMiddlewareEnforcement(t *testing.T) {
	router := newTestRouter(nil)

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

func TestNilPoolServiceUnavailable(t *testing.T) {
	router := newTestRouter(nil)
	tenantID := uuid.New()

	endpoints := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/resources", ""},
		{http.MethodGet, "/api/v1/resources/" + uuid.New().String() + "/graph", ""},
		{http.MethodPost, "/api/v1/accounts/" + uuid.New().String() + "/verify", ""},
		{http.MethodPost, "/api/v1/accounts/" + uuid.New().String() + "/sync", ""},
		{http.MethodGet, "/api/v1/cost/summary", ""},
		{http.MethodGet, "/api/v1/waste/summary", ""},
		{http.MethodGet, "/api/v1/waste/findings", ""},
		{http.MethodPost, "/api/v1/waste/analyze", ""},
	}

	for _, ep := range endpoints {
		var req *http.Request
		if ep.body != "" {
			req = httptest.NewRequest(ep.method, ep.path, strings.NewReader(ep.body))
		} else {
			req = httptest.NewRequest(ep.method, ep.path, nil)
		}
		req.Header.Set("X-Tenant-ID", tenantID.String())
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("[%s %s] expected 503 Service Unavailable, got %d", ep.method, ep.path, rr.Code)
		}
	}
}

func TestExternalIDGeneration(t *testing.T) {
	router := newTestRouter(nil)
	tenantID := uuid.New()

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
	if extResp["cradle_account_id"] != "123456789012" {
		t.Errorf("expected Cradle AWS Account 123456789012, got %s", extResp["cradle_account_id"])
	}
}

func TestDatabaseIntegrationLifecycle(t *testing.T) {
	pool := getTestPool(t)
	if pool == nil {
		t.Skip("PostgreSQL not available; skipping database integration test")
	}

	router := newTestRouter(pool)

	// 1. Create a dedicated test tenant
	testTenantID := uuid.New()
	tenantBody, _ := json.Marshal(map[string]interface{}{
		"id":        testTenantID,
		"name":      "Integration Test Corp",
		"slug":      "test-corp-" + testTenantID.String()[:8],
		"plan_tier": "enterprise",
	})
	reqTenant := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", bytes.NewReader(tenantBody))
	reqTenant.Header.Set("Content-Type", "application/json")
	rrTenant := httptest.NewRecorder()
	router.ServeHTTP(rrTenant, reqTenant)

	if rrTenant.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for tenant creation, got %d: %s", rrTenant.Code, rrTenant.Body.String())
	}

	var createdTenant domain.Tenant
	if err := json.Unmarshal(rrTenant.Body.Bytes(), &createdTenant); err != nil {
		t.Fatalf("failed decoding tenant response: %v", err)
	}
	if createdTenant.ID != testTenantID {
		t.Errorf("expected tenant ID %s, got %s", testTenantID, createdTenant.ID)
	}

	// 2. Register Cloud Account for the tenant
	accPayload := map[string]interface{}{
		"provider":   "aws",
		"account_id": "123456789012",
		"name":       "Production AWS Account",
		"role_arn":   "arn:aws:iam::123456789012:role/CradleControlPlaneRole",
		"regions":    []string{"us-east-1"},
	}
	accBytes, _ := json.Marshal(accPayload)

	reqAcc := httptest.NewRequest(http.MethodPost, "/api/v1/accounts", bytes.NewReader(accBytes))
	reqAcc.Header.Set("X-Tenant-ID", testTenantID.String())
	reqAcc.Header.Set("Content-Type", "application/json")
	rrAcc := httptest.NewRecorder()
	router.ServeHTTP(rrAcc, reqAcc)

	if rrAcc.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created for account registration, got %d: %s", rrAcc.Code, rrAcc.Body.String())
	}

	var regResp struct {
		Account         domain.CloudAccount `json:"account"`
		TrustPolicyJSON string              `json:"trust_policy_json"`
	}
	if err := json.Unmarshal(rrAcc.Body.Bytes(), &regResp); err != nil {
		t.Fatalf("failed decoding account response: %v", err)
	}
	accountID := regResp.Account.ID
	if accountID == uuid.Nil {
		t.Fatal("expected non-nil account ID")
	}

	// 3. List Accounts
	reqList := httptest.NewRequest(http.MethodGet, "/api/v1/accounts", nil)
	reqList.Header.Set("X-Tenant-ID", testTenantID.String())
	rrList := httptest.NewRecorder()
	router.ServeHTTP(rrList, reqList)

	if rrList.Code != http.StatusOK {
		t.Fatalf("expected status 200 for listing accounts, got %d: %s", rrList.Code, rrList.Body.String())
	}

	// 4. Verify Connection
	reqVerify := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/"+accountID.String()+"/verify", nil)
	reqVerify.Header.Set("X-Tenant-ID", testTenantID.String())
	rrVerify := httptest.NewRecorder()
	router.ServeHTTP(rrVerify, reqVerify)

	if rrVerify.Code != http.StatusOK {
		t.Fatalf("expected status 200 for verify, got %d: %s", rrVerify.Code, rrVerify.Body.String())
	}

	// 5. Trigger Discovery Sync
	reqSync := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/"+accountID.String()+"/sync", nil)
	reqSync.Header.Set("X-Tenant-ID", testTenantID.String())
	rrSync := httptest.NewRecorder()
	router.ServeHTTP(rrSync, reqSync)

	if rrSync.Code != http.StatusAccepted {
		t.Fatalf("expected status 202 Accepted for sync trigger, got %d: %s", rrSync.Code, rrSync.Body.String())
	}

	var syncResp map[string]interface{}
	_ = json.Unmarshal(rrSync.Body.Bytes(), &syncResp)
	if syncResp["status"] != "completed" {
		t.Errorf("expected status 'completed', got %v", syncResp["status"])
	}
	if count, ok := syncResp["discovered_count"].(float64); !ok || count != 2 {
		t.Errorf("expected discovered_count 2, got %v", syncResp["discovered_count"])
	}

	// 6. List Discovered Resources from Database
	reqRes := httptest.NewRequest(http.MethodGet, "/api/v1/resources", nil)
	reqRes.Header.Set("X-Tenant-ID", testTenantID.String())
	rrRes := httptest.NewRecorder()
	router.ServeHTTP(rrRes, reqRes)

	if rrRes.Code != http.StatusOK {
		t.Fatalf("expected status 200 for list resources, got %d: %s", rrRes.Code, rrRes.Body.String())
	}

	var resListResp struct {
		Total     int                     `json:"total"`
		Resources []domain.ResourceEntity `json:"resources"`
	}
	_ = json.Unmarshal(rrRes.Body.Bytes(), &resListResp)
	if resListResp.Total != 2 {
		t.Errorf("expected 2 discovered resources in DB, got %d", resListResp.Total)
	}

	// 7. Verify Graph Blast Radius & Resource Lookup (Two-Phase Resolution Verified!)
	if len(resListResp.Resources) > 0 {
		firstResID := resListResp.Resources[0].ID

		// Get individual resource
		reqSingle := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+firstResID.String(), nil)
		reqSingle.Header.Set("X-Tenant-ID", testTenantID.String())
		rrSingle := httptest.NewRecorder()
		router.ServeHTTP(rrSingle, reqSingle)
		if rrSingle.Code != http.StatusOK {
			t.Fatalf("expected status 200 for single resource, got %d: %s", rrSingle.Code, rrSingle.Body.String())
		}

		// Direction: both
		reqGraph := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+firstResID.String()+"/graph?depth=2", nil)
		reqGraph.Header.Set("X-Tenant-ID", testTenantID.String())
		rrGraph := httptest.NewRecorder()
		router.ServeHTTP(rrGraph, reqGraph)

		if rrGraph.Code != http.StatusOK {
			t.Fatalf("expected status 200 for resource graph, got %d: %s", rrGraph.Code, rrGraph.Body.String())
		}

		// Direction: upstream & downstream navigation
		reqUpstream := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+firstResID.String()+"/graph?direction=upstream&depth=2", nil)
		reqUpstream.Header.Set("X-Tenant-ID", testTenantID.String())
		rrUpstream := httptest.NewRecorder()
		router.ServeHTTP(rrUpstream, reqUpstream)
		if rrUpstream.Code != http.StatusOK {
			t.Fatalf("expected status 200 for upstream graph, got %d", rrUpstream.Code)
		}

		reqDownstream := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+firstResID.String()+"/graph?direction=downstream&depth=2", nil)
		reqDownstream.Header.Set("X-Tenant-ID", testTenantID.String())
		rrDownstream := httptest.NewRecorder()
		router.ServeHTTP(rrDownstream, reqDownstream)
		if rrDownstream.Code != http.StatusOK {
			t.Fatalf("expected status 200 for downstream graph, got %d", rrDownstream.Code)
		}
	}

	// 8. Graph Stats
	reqStats := httptest.NewRequest(http.MethodGet, "/api/v1/graph/stats", nil)
	reqStats.Header.Set("X-Tenant-ID", testTenantID.String())
	rrStats := httptest.NewRecorder()
	router.ServeHTTP(rrStats, reqStats)
	if rrStats.Code != http.StatusOK {
		t.Fatalf("expected status 200 for graph stats, got %d: %s", rrStats.Code, rrStats.Body.String())
	}

	// 9. Visual Explorer UI
	reqExplorer := httptest.NewRequest(http.MethodGet, "/explorer", nil)
	rrExplorer := httptest.NewRecorder()
	router.ServeHTTP(rrExplorer, reqExplorer)
	if rrExplorer.Code != http.StatusOK {
		t.Fatalf("expected status 200 for /explorer, got %d", rrExplorer.Code)
	}
	if !strings.Contains(rrExplorer.Body.String(), "Cradle &mdash; Infrastructure Dependency Explorer") {
		t.Errorf("expected explorer page content in /explorer response")
	}

	// 10. Cost & Waste Analysis
	reqAnalyze := httptest.NewRequest(http.MethodPost, "/api/v1/waste/analyze", nil)
	reqAnalyze.Header.Set("X-Tenant-ID", testTenantID.String())
	rrAnalyze := httptest.NewRecorder()
	router.ServeHTTP(rrAnalyze, reqAnalyze)

	if rrAnalyze.Code != http.StatusOK {
		t.Fatalf("expected status 200 for waste analyze, got %d: %s", rrAnalyze.Code, rrAnalyze.Body.String())
	}

	reqWasteSummary := httptest.NewRequest(http.MethodGet, "/api/v1/waste/summary", nil)
	reqWasteSummary.Header.Set("X-Tenant-ID", testTenantID.String())
	rrWasteSummary := httptest.NewRecorder()
	router.ServeHTTP(rrWasteSummary, reqWasteSummary)

	if rrWasteSummary.Code != http.StatusOK {
		t.Fatalf("expected status 200 for waste summary, got %d: %s", rrWasteSummary.Code, rrWasteSummary.Body.String())
	}
}
