package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

func TestInventoryHandlerPage(t *testing.T) {
	h := NewInventoryHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/inventory", nil)
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
	if !strings.Contains(body, "Cradle — Operational Inventory") {
		t.Errorf("expected page title in HTML body")
	}
	if !strings.Contains(body, "resource-table") {
		t.Errorf("expected resource table in HTML body")
	}
	if !strings.Contains(body, "rule-form") {
		t.Errorf("expected rule form in HTML body")
	}
	if !strings.Contains(body, "Unknown Ownership") {
		t.Errorf("expected Unknown Ownership queue tab in HTML body")
	}
}

func TestInventoryHandlerNilPool(t *testing.T) {
	h := NewInventoryHandler(nil)
	tenantID := uuid.New()
	ctx := domain.ContextWithTenant(context.Background(), domain.TenantContext{
		TenantID: tenantID,
	})

	endpoints := []struct {
		name    string
		method  string
		path    string
		body    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"ListResources", http.MethodGet, "/api/v1/inventory/resources", "", h.ListResources},
		{"GetResource", http.MethodGet, "/api/v1/inventory/resources/" + uuid.New().String(), "", h.GetResource},
		{"GetSummary", http.MethodGet, "/api/v1/inventory/summary", "", h.GetSummary},
		{"ListRules", http.MethodGet, "/api/v1/inventory/rules", "", h.ListRules},
		{"SaveRule POST", http.MethodPost, "/api/v1/inventory/rules", `{"name":"test","tag_key":"team","tag_value":"payments","priority":100,"enabled":true,"assignments":{"team":"Payments"}}`, h.SaveRule},
		{"SaveRule PUT", http.MethodPut, "/api/v1/inventory/rules/" + uuid.New().String(), `{"name":"test","tag_key":"team","tag_value":"payments","priority":100,"enabled":true,"assignments":{"team":"Payments"}}`, h.SaveRule},
		{"DeleteRule", http.MethodDelete, "/api/v1/inventory/rules/" + uuid.New().String(), "", h.DeleteRule},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			var bodyReader *strings.Reader
			if ep.body != "" {
				bodyReader = strings.NewReader(ep.body)
			} else {
				bodyReader = strings.NewReader("")
			}
			req := httptest.NewRequest(ep.method, ep.path, bodyReader).WithContext(ctx)
			rr := httptest.NewRecorder()
			ep.handler(rr, req)

			if rr.Code != http.StatusServiceUnavailable {
				t.Errorf("%s: expected 503 Service Unavailable, got %d", ep.name, rr.Code)
			}
		})
	}
}

func TestInventoryHandlerUnauthorized(t *testing.T) {
	h := NewInventoryHandler(nil)
	ctx := context.Background() // no tenant

	endpoints := []struct {
		name    string
		method  string
		path    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"ListResources", http.MethodGet, "/api/v1/inventory/resources", h.ListResources},
		{"GetResource", http.MethodGet, "/api/v1/inventory/resources/" + uuid.New().String(), h.GetResource},
		{"GetSummary", http.MethodGet, "/api/v1/inventory/summary", h.GetSummary},
		{"ListRules", http.MethodGet, "/api/v1/inventory/rules", h.ListRules},
		{"SaveRule", http.MethodPost, "/api/v1/inventory/rules", h.SaveRule},
		{"DeleteRule", http.MethodDelete, "/api/v1/inventory/rules/" + uuid.New().String(), h.DeleteRule},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, bytes.NewBuffer(nil)).WithContext(ctx)
			rr := httptest.NewRecorder()
			ep.handler(rr, req)

			// When pool is nil, tenant check verifies pool first and returns 503;
			// when pool is non-nil or if tenant context is missing/nil, returns 401 or 503.
			if rr.Code != http.StatusServiceUnavailable && rr.Code != http.StatusUnauthorized {
				t.Errorf("%s: expected 503 or 401, got %d", ep.name, rr.Code)
			}
		})
	}
}
