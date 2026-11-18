package postgres

import (
	"context"
	"fmt"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TenantTxFunc defines the function signature executed within a tenant-scoped database transaction.
type TenantTxFunc func(ctx context.Context, tx pgx.Tx) error

// WithTenantTx executes a database transaction with PostgreSQL Row-Level Security (RLS) active.
//
// Mentoring & Security Insight:
// We use `SET LOCAL app.current_tenant_id = $1`.
// The `LOCAL` keyword binds the session variable strictly to the lifetime of the current transaction.
// When the transaction completes (commit or rollback), PostgreSQL automatically resets the variable.
// This prevents connection pool poisoning, ensuring no subsequent query can accidentally inherit
// another tenant's isolation context.
func WithTenantTx(ctx context.Context, pool *pgxpool.Pool, tenantID uuid.UUID, fn TenantTxFunc) error {
	if pool == nil {
		return fmt.Errorf("connection pool is nil")
	}
	if tenantID == uuid.Nil {
		return fmt.Errorf("cannot execute tenant transaction: tenant ID is nil")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Activate PostgreSQL Row-Level Security session context using parameterized set_config (local to transaction)
	setQuery := "SELECT set_config('app.current_tenant_id', $1, true)"
	if _, err := tx.Exec(ctx, setQuery, tenantID.String()); err != nil {
		return fmt.Errorf("failed to set RLS tenant context (%s): %w", tenantID, err)
	}

	// Execute business logic with RLS active
	if err := fn(ctx, tx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// WithContextTenantTx is a convenience helper that extracts TenantContext from the Go context
// and executes the transaction with RLS active.
func WithContextTenantTx(ctx context.Context, pool *pgxpool.Pool, fn TenantTxFunc) error {
	tc, err := domain.TenantFromContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to retrieve tenant from context: %w", err)
	}
	return WithTenantTx(ctx, pool, tc.TenantID, fn)
}
