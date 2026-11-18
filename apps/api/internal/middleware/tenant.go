package middleware

import (
	"net/http"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const HeaderTenantID = "X-Tenant-ID"

// TenantMiddleware extracts and verifies the tenant identity on incoming HTTP requests.
// When a database pool is available, it validates that the tenant exists and is active.
func TenantMiddleware(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
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

			if pool != nil {
				var exists bool
				err := pool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM tenants WHERE id = $1 AND status = 'active')", tenantID).Scan(&exists)
				if err != nil || !exists {
					http.Error(w, `{"error": "tenant not found or inactive"}`, http.StatusUnauthorized)
					return
				}
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
}
