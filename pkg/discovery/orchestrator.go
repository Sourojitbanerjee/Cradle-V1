package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/provider"
	"github.com/cradle-dev/cradle/pkg/storage/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Orchestrator coordinates cloud infrastructure discovery, state upserts, and audit records.
type Orchestrator struct {
	pool     *pgxpool.Pool
	registry *provider.Registry
}

// NewOrchestrator creates a new discovery orchestrator.
func NewOrchestrator(pool *pgxpool.Pool, registry *provider.Registry) *Orchestrator {
	if registry == nil {
		registry = provider.DefaultRegistry
	}
	return &Orchestrator{
		pool:     pool,
		registry: registry,
	}
}

// RunDiscovery executes an end-to-end read-only synchronization job against a cloud account.
func (o *Orchestrator) RunDiscovery(ctx context.Context, account *domain.CloudAccount) (*provider.DiscoveryResult, error) {
	if err := account.Validate(); err != nil {
		return nil, fmt.Errorf("invalid account: %w", err)
	}

	p, err := o.registry.Get(account.Provider)
	if err != nil {
		return nil, fmt.Errorf("provider unavailable: %w", err)
	}

	jobID := uuid.New()
	startTime := time.Now().UTC()

	// 1. Record job creation in database (if pool available)
	if o.pool != nil {
		_ = o.recordJobStart(ctx, jobID, account, startTime)
	}

	// 2. Execute read-only discovery via provider adapter
	req := provider.DiscoveryRequest{
		JobID:    jobID,
		TenantID: account.TenantID,
		Account:  *account,
		Regions:  account.Regions,
	}

	result, err := p.DiscoverResources(ctx, req)
	if err != nil {
		if o.pool != nil {
			_ = o.recordJobFailure(ctx, jobID, account, err)
		}
		return nil, fmt.Errorf("discovery execution failed: %w", err)
	}

	// 3. Persist resources and relationships idempotently
	if o.pool != nil {
		if err := o.persistDiscoveryResult(ctx, account, result); err != nil {
			return nil, fmt.Errorf("failed persisting discovery result: %w", err)
		}
	}

	return result, nil
}

func (o *Orchestrator) recordJobStart(ctx context.Context, jobID uuid.UUID, account *domain.CloudAccount, startTime time.Time) error {
	return postgres.WithTenantTx(ctx, o.pool, account.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		query := `
			INSERT INTO discovery_jobs (id, tenant_id, cloud_account_id, status, regions, started_at)
			VALUES ($1, $2, $3, 'in_progress', $4, $5);
		`
		_, err := tx.Exec(ctx, query, jobID, account.TenantID, account.ID, account.Regions, startTime)
		return err
	})
}

func (o *Orchestrator) recordJobFailure(ctx context.Context, jobID uuid.UUID, account *domain.CloudAccount, err error) error {
	return postgres.WithTenantTx(ctx, o.pool, account.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		errorSummary, _ := json.Marshal(map[string]string{"fatal_error": err.Error()})
		query := `
			UPDATE discovery_jobs
			SET status = 'failed', error_summary = $1, completed_at = NOW()
			WHERE id = $2;
		`
		_, execErr := tx.Exec(ctx, query, errorSummary, jobID)
		return execErr
	})
}

func (o *Orchestrator) persistDiscoveryResult(ctx context.Context, account *domain.CloudAccount, result *provider.DiscoveryResult) error {
	return postgres.WithTenantTx(ctx, o.pool, account.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		now := time.Now().UTC()

		// 1. Idempotently upsert discovered entities
		upsertEntityQuery := `
			INSERT INTO resource_entities (
				id, tenant_id, cloud_account_id, provider, account_id, region,
				resource_type, native_id, arn, name, status, tags,
				created_at_provider, observed_at, observed_state, derived_state,
				inferred_state, configuration_hash, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20
			)
			ON CONFLICT (tenant_id, arn) DO UPDATE SET
				name = EXCLUDED.name,
				status = EXCLUDED.status,
				tags = EXCLUDED.tags,
				observed_at = EXCLUDED.observed_at,
				observed_state = EXCLUDED.observed_state,
				derived_state = EXCLUDED.derived_state,
				configuration_hash = EXCLUDED.configuration_hash,
				updated_at = EXCLUDED.updated_at,
				deleted_at = NULL;
		`

		for _, entity := range result.Entities {
			tagsJSON, _ := json.Marshal(entity.Tags)
			obsJSON, _ := json.Marshal(entity.ObservedState)
			derJSON, _ := json.Marshal(entity.DerivedState)
			infJSON, _ := json.Marshal(entity.InferredState)

			_, err := tx.Exec(ctx, upsertEntityQuery,
				entity.ID, entity.TenantID, entity.CloudAccountID, entity.Provider,
				entity.AccountID, entity.Region, entity.ResourceType, entity.NativeID,
				entity.ARN, entity.Name, entity.Status, tagsJSON,
				entity.CreatedAtProvider, entity.ObservedAt, obsJSON, derJSON,
				infJSON, entity.ConfigurationHash, now, now,
			)
			if err != nil {
				return fmt.Errorf("failed upserting entity %s: %w", entity.ARN, err)
			}
		}

		// 2. Update cloud_accounts status & last_discovered_at
		status := "connected"
		jobStatus := "completed"
		if len(result.Errors) > 0 {
			jobStatus = "partial_failure"
		}

		errSummaryJSON, _ := json.Marshal(result.Errors)

		updateAccountQuery := `
			UPDATE cloud_accounts
			SET status = $1, last_discovered_at = $2, updated_at = $2
			WHERE id = $3;
		`
		if _, err := tx.Exec(ctx, updateAccountQuery, status, now, account.ID); err != nil {
			return fmt.Errorf("failed updating cloud_account: %w", err)
		}

		// 3. Mark discovery_job as completed
		updateJobQuery := `
			UPDATE discovery_jobs
			SET status = $1, discovered_count = $2, error_summary = $3, completed_at = $4
			WHERE id = $5;
		`
		if _, err := tx.Exec(ctx, updateJobQuery, jobStatus, len(result.Entities), errSummaryJSON, now, result.JobID); err != nil {
			return fmt.Errorf("failed updating discovery_job: %w", err)
		}

		return nil
	})
}
