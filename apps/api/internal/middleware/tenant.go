package middleware

import (
	"net/http"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

const HeaderTenantID = "X-Tenant-ID"

// TenantMiddleware extracts and verifies the tenant identity on incoming HTTP requests.
func TenantMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantHeader := r.Header.Get(HeaderTenantID)
		if tenantHeader == "" {
			http.Error(w, `{"error": "missing required X-Tenant-ID header"}`, http.StatusBadRequest)
			return
		}

		tenantID, err := uuid.Parse(tenantHeader)
		if err != nil {
			http.Error(w, `{"error": "invalid X-Tenant-ID format, must be UUID"}`, http.StatusBadRequest)
			return
		}

		// Inject tenant context into request context
		tc := domain.TenantContext{
			TenantID: tenantID,
			UserRole: domain.RoleAdmin, // Default for early phase, replaced with auth claims
		}

		ctx := domain.ContextWithTenant(r.Context(), tc)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
