package graph

import (
	"context"
	"fmt"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/storage/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// QueryEngine provides read-only graph traversal and resource lookup operations
// against the canonical inventory and dependency edges in PostgreSQL.
type QueryEngine struct {
	pool *pgxpool.Pool
}

func NewQueryEngine(pool *pgxpool.Pool) *QueryEngine {
	return &QueryEngine{pool: pool}
}

// ResourceFilter specifies search/filter criteria for inventory queries.
type ResourceFilter struct {
	TenantID     uuid.UUID
	Region       string
	ResourceType string
	Status       string
	NameContains string
	Limit        int
	Offset       int
}

// ResourceListResult holds paginated resource inventory results.
type ResourceListResult struct {
	Total     int                     `json:"total"`
	Limit     int                     `json:"limit"`
	Offset    int                     `json:"offset"`
	Resources []domain.ResourceEntity `json:"resources"`
}

// ListResources returns filtered, paginated resource entities for a tenant.
func (e *QueryEngine) ListResources(ctx context.Context, filter ResourceFilter) (*ResourceListResult, error) {
	// Normalize limits before any early return
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 500 {
		filter.Limit = 500
	}

	if e.pool == nil {
		return &ResourceListResult{
			Limit:     filter.Limit,
			Offset:    filter.Offset,
			Resources: []domain.ResourceEntity{},
		}, nil
	}

	result := &ResourceListResult{
		Limit:     filter.Limit,
		Offset:    filter.Offset,
		Resources: []domain.ResourceEntity{},
	}

	err := postgres.WithTenantTx(ctx, e.pool, filter.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		// Count total matching records
		countQuery := `SELECT COUNT(*) FROM resource_entities WHERE deleted_at IS NULL`
		args := []interface{}{}
		argIdx := 1

		if filter.Region != "" {
			countQuery += fmt.Sprintf(" AND region = $%d", argIdx)
			args = append(args, filter.Region)
			argIdx++
		}
		if filter.ResourceType != "" {
			countQuery += fmt.Sprintf(" AND resource_type = $%d", argIdx)
			args = append(args, filter.ResourceType)
			argIdx++
		}
		if filter.Status != "" {
			countQuery += fmt.Sprintf(" AND status = $%d", argIdx)
			args = append(args, filter.Status)
			argIdx++
		}
		if filter.NameContains != "" {
			countQuery += fmt.Sprintf(" AND name ILIKE '%%' || $%d || '%%'", argIdx)
			args = append(args, filter.NameContains)
			argIdx++
		}

		if err := tx.QueryRow(ctx, countQuery, args...).Scan(&result.Total); err != nil {
			return fmt.Errorf("count query failed: %w", err)
		}

		// Fetch page
		selectQuery := `
			SELECT id, cloud_account_id, provider, account_id, region,
				   resource_type, native_id, arn, name, status,
				   tags, observed_at, configuration_hash, monthly_cost_estimate,
				   created_at, updated_at
			FROM resource_entities
			WHERE deleted_at IS NULL`

		selectArgs := make([]interface{}, len(args))
		copy(selectArgs, args)
		selectArgIdx := argIdx

		if filter.Region != "" {
			// Already added in count
		}
		// Rebuild WHERE for select (reuse same args)
		selectQuery = `
			SELECT id, cloud_account_id, provider, account_id, region,
				   resource_type, native_id, arn, name, status,
				   tags, observed_at, configuration_hash, monthly_cost_estimate,
				   created_at, updated_at
			FROM resource_entities
			WHERE deleted_at IS NULL`

		selectArgs = []interface{}{}
		selectArgIdx = 1
		if filter.Region != "" {
			selectQuery += fmt.Sprintf(" AND region = $%d", selectArgIdx)
			selectArgs = append(selectArgs, filter.Region)
			selectArgIdx++
		}
		if filter.ResourceType != "" {
			selectQuery += fmt.Sprintf(" AND resource_type = $%d", selectArgIdx)
			selectArgs = append(selectArgs, filter.ResourceType)
			selectArgIdx++
		}
		if filter.Status != "" {
			selectQuery += fmt.Sprintf(" AND status = $%d", selectArgIdx)
			selectArgs = append(selectArgs, filter.Status)
			selectArgIdx++
		}
		if filter.NameContains != "" {
			selectQuery += fmt.Sprintf(" AND name ILIKE '%%' || $%d || '%%'", selectArgIdx)
			selectArgs = append(selectArgs, filter.NameContains)
			selectArgIdx++
		}

		selectQuery += fmt.Sprintf(" ORDER BY resource_type, name LIMIT $%d OFFSET $%d", selectArgIdx, selectArgIdx+1)
		selectArgs = append(selectArgs, filter.Limit, filter.Offset)

		rows, err := tx.Query(ctx, selectQuery, selectArgs...)
		if err != nil {
			return fmt.Errorf("select query failed: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var e domain.ResourceEntity
			e.TenantID = filter.TenantID
			var provStr string
			if err := rows.Scan(
				&e.ID, &e.CloudAccountID, &provStr, &e.AccountID, &e.Region,
				&e.ResourceType, &e.NativeID, &e.ARN, &e.Name, &e.Status,
				&e.Tags, &e.ObservedAt, &e.ConfigurationHash, &e.MonthlyCostEstimate,
				&e.CreatedAt, &e.UpdatedAt,
			); err != nil {
				return fmt.Errorf("row scan failed: %w", err)
			}
			e.Provider = domain.ProviderType(provStr)
			result.Resources = append(result.Resources, e)
		}

		return rows.Err()
	})

	return result, err
}

// GetBlastRadius performs a recursive breadth-first traversal of the dependency graph
// starting from a root resource, returning all reachable nodes within maxDepth hops.
// This uses PostgreSQL recursive CTEs for efficient graph traversal.
func (e *QueryEngine) GetBlastRadius(ctx context.Context, tenantID uuid.UUID, rootResourceID uuid.UUID, maxDepth int) ([]domain.ResourceEntity, []domain.ResourceRelationship, error) {
	if e.pool == nil {
		return nil, nil, nil
	}
	if maxDepth <= 0 || maxDepth > 6 {
		maxDepth = 3
	}

	var entities []domain.ResourceEntity
	var rels []domain.ResourceRelationship

	err := postgres.WithTenantTx(ctx, e.pool, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		// Recursive CTE: traverse the graph outward from root
		query := `
			WITH RECURSIVE reachable AS (
				SELECT source_resource_id, target_resource_id, relationship_type, 1 AS depth
				FROM resource_relationships
				WHERE source_resource_id = $1
				
				UNION ALL
				
				SELECT rr.source_resource_id, rr.target_resource_id, rr.relationship_type, r.depth + 1
				FROM resource_relationships rr
				INNER JOIN reachable r ON rr.source_resource_id = r.target_resource_id
				WHERE r.depth < $2
			)
			SELECT DISTINCT target_resource_id, relationship_type, depth
			FROM reachable
			ORDER BY depth;
		`

		rows, err := tx.Query(ctx, query, rootResourceID, maxDepth)
		if err != nil {
			return fmt.Errorf("blast radius CTE failed: %w", err)
		}
		defer rows.Close()

		reachableIDs := []uuid.UUID{rootResourceID}
		for rows.Next() {
			var targetID uuid.UUID
			var relType string
			var depth int
			if err := rows.Scan(&targetID, &relType, &depth); err != nil {
				return err
			}
			reachableIDs = append(reachableIDs, targetID)
			rels = append(rels, domain.ResourceRelationship{
				TargetResourceID: targetID,
				RelationshipType: domain.RelationshipType(relType),
			})
		}
		if err := rows.Err(); err != nil {
			return err
		}

		// Fetch full entity details for all reachable nodes
		if len(reachableIDs) > 0 {
			entityQuery := `
				SELECT id, provider, account_id, region, resource_type, native_id, arn, name, status, observed_at
				FROM resource_entities
				WHERE id = ANY($1) AND deleted_at IS NULL
			`
			entRows, err := tx.Query(ctx, entityQuery, reachableIDs)
			if err != nil {
				return err
			}
			defer entRows.Close()

			for entRows.Next() {
				var ent domain.ResourceEntity
				ent.TenantID = tenantID
				var provStr string
				if err := entRows.Scan(
					&ent.ID, &provStr, &ent.AccountID, &ent.Region,
					&ent.ResourceType, &ent.NativeID, &ent.ARN, &ent.Name,
					&ent.Status, &ent.ObservedAt,
				); err != nil {
					return err
				}
				ent.Provider = domain.ProviderType(provStr)
				entities = append(entities, ent)
			}
			return entRows.Err()
		}

		return nil
	})

	return entities, rels, err
}
