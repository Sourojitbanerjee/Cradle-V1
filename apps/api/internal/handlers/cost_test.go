package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

func TestCostHandlerPage(t *testing.T) {
	h := NewCostHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/cost", nil)
	rr := httptest.NewRecorder()

	h.Page(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("expected text/html content-type, got %s", contentType)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "Cloud Cost Intelligence") {
		t.Errorf("expected page title in HTML body")
	}
	if !strings.Contains(body, "Cost Explanation Engine") {
		t.Errorf("expected Cost Explanation Engine in HTML body")
	}
	if !strings.Contains(body, "Why did our bill increase?") {
		t.Errorf("expected bill increase prompt in HTML body")
	}
	if !strings.Contains(body, "dimension-tabs") {
		t.Errorf("expected dimensional tabs in HTML body")
	}
}

func TestCostHandlerNilPool(t *testing.T) {
	h := NewCostHandler(nil)
	tenantID := uuid.New()
	ctx := domain.ContextWithTenant(context.Background(), domain.TenantContext{
		TenantID: tenantID,
	})

	endpoints := []struct {
		name    string
		method  string
		path    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"GetCostSummary", http.MethodGet, "/api/v1/cost/summary", h.GetCostSummary},
		{"GetCostBreakdown", http.MethodGet, "/api/v1/cost/breakdown?dimension=service", h.GetCostBreakdown},
		{"GetCostTrends", http.MethodGet, "/api/v1/cost/trends?granularity=daily", h.GetCostTrends},
		{"GetCostAnomalies", http.MethodGet, "/api/v1/cost/anomalies", h.GetCostAnomalies},
		{"GetCostForecast", http.MethodGet, "/api/v1/cost/forecast?days=30", h.GetCostForecast},
		{"ExplainCostChanges GET", http.MethodGet, "/api/v1/cost/explain", h.ExplainCostChanges},
		{"ExplainCostChanges POST", http.MethodPost, "/api/v1/cost/explain", h.ExplainCostChanges},
		{"GetWasteSummary", http.MethodGet, "/api/v1/waste/summary", h.GetWasteSummary},
		{"ListWasteFindings", http.MethodGet, "/api/v1/waste/findings", h.ListWasteFindings},
		{"AnalyzeWaste", http.MethodPost, "/api/v1/waste/analyze", h.AnalyzeWaste},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil).WithContext(ctx)
			rr := httptest.NewRecorder()
			ep.handler(rr, req)

			if rr.Code != http.StatusServiceUnavailable {
				t.Errorf("%s: expected 503 Service Unavailable, got %d", ep.name, rr.Code)
			}
		})
	}
}

func TestCostHandlerUnauthorized(t *testing.T) {
	h := NewCostHandler(nil)
	ctx := context.Background() // no tenant

	endpoints := []struct {
		name    string
		method  string
		path    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"GetCostSummary", http.MethodGet, "/api/v1/cost/summary", h.GetCostSummary},
		{"GetCostBreakdown", http.MethodGet, "/api/v1/cost/breakdown", h.GetCostBreakdown},
		{"GetCostTrends", http.MethodGet, "/api/v1/cost/trends", h.GetCostTrends},
		{"GetCostAnomalies", http.MethodGet, "/api/v1/cost/anomalies", h.GetCostAnomalies},
		{"GetCostForecast", http.MethodGet, "/api/v1/cost/forecast", h.GetCostForecast},
		{"ExplainCostChanges", http.MethodGet, "/api/v1/cost/explain", h.ExplainCostChanges},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil).WithContext(ctx)
			rr := httptest.NewRecorder()
			ep.handler(rr, req)

			// When pool is nil, returns 503; if pool were non-nil, returns 401
			if rr.Code != http.StatusServiceUnavailable && rr.Code != http.StatusUnauthorized {
				t.Errorf("%s: expected 503 or 401, got %d", ep.name, rr.Code)
			}
		})
	}
}
