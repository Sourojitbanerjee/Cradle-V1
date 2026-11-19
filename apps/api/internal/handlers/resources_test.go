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

func TestExplorerHandler(t *testing.T) {
	h := NewExplorerHandler(nil, "v0.2.0")
	req := httptest.NewRequest(http.MethodGet, "/explorer", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("expected text/html content-type, got %s", contentType)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "Cradle &mdash; Infrastructure Dependency Explorer") {
		t.Errorf("expected explorer page title in HTML body")
	}
	if !strings.Contains(body, "graph-svg") {
		t.Errorf("expected SVG graph element in HTML body")
	}
	if !strings.Contains(body, "inspector-drawer") {
		t.Errorf("expected inspector drawer in HTML body")
	}
}

func TestResourceHandlerNilPool(t *testing.T) {
	h := NewResourceHandler(nil)
	tenantID := uuid.New()
	ctx := domain.ContextWithTenant(context.Background(), domain.TenantContext{
		TenantID: tenantID,
	})

	t.Run("ListResources without pool", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources", nil).WithContext(ctx)
		rr := httptest.NewRecorder()
		h.ListResources(rr, req)
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d", rr.Code)
		}
	})

	t.Run("GetResource without pool", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+uuid.New().String(), nil).WithContext(ctx)
		rr := httptest.NewRecorder()
		h.GetResource(rr, req)
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d", rr.Code)
		}
	})

	t.Run("GetResourceGraph without pool", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/resources/"+uuid.New().String()+"/graph", nil).WithContext(ctx)
		rr := httptest.NewRecorder()
		h.GetResourceGraph(rr, req)
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d", rr.Code)
		}
	})

	t.Run("GetGraphStats without pool", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/graph/stats", nil).WithContext(ctx)
		rr := httptest.NewRecorder()
		h.GetGraphStats(rr, req)
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d", rr.Code)
		}
	})
}
