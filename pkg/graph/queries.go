package graph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/storage/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Direction defines navigation traversal perspective.
type Direction string

const (
	DirectionBoth       Direction = "both"
	DirectionUpstream   Direction = "upstream"   // Incoming edges: what depends on this resource
	DirectionDownstream Direction = "downstream" // Outgoing edges: what this resource depends on
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

// DependencyGraphResult provides a complete, structured dependency view around a resource.
type DependencyGraphResult struct {
	RootResourceID  uuid.UUID                     `json:"root_resource_id"`
	RootResource    *domain.ResourceEntity        `json:"root_resource,omitempty"`
	Direction       Direction                     `json:"direction"`
	MaxDepth        int                           `json:"max_depth"`
	Nodes           []domain.ResourceEntity       `json:"nodes"`
	Edges           []domain.ResourceRelationship `json:"edges"`
	UpstreamNodes   []domain.ResourceEntity       `json:"upstream_nodes"`
	DownstreamNodes []domain.ResourceEntity       `json:"downstream_nodes"`
	UpstreamCount   int                           `json:"upstream_count"`
	DownstreamCount int                           `json:"downstream_count"`
}

// GraphStats summarizes graph topology metrics for a tenant.
type GraphStats struct {
	TotalNodes  int            `json:"total_nodes"`
	TotalEdges  int            `json:"total_edges"`
	NodesByType map[string]int `json:"nodes_by_type"`
	EdgesByType map[string]int `json:"edges_by_type"`
	TopHubs     []HubStat      `json:"top_hubs"`
}

// HubStat represents a heavily connected infrastructure hub.
type HubStat struct {
	ResourceID   uuid.UUID `json:"resource_id"`
	Name         string    `json:"name"`
	ResourceType string    `json:"resource_type"`
	EdgeCount    int       `json:"edge_count"`
}

// GetResource retrieves a single canonical resource by ID with full provenance data.
func (e *QueryEngine) GetResource(ctx context.Context, tenantID, resourceID uuid.UUID) (*domain.ResourceEntity, error) {
	if e.pool == nil {
		return nil, errors.New("database pool is required")
	}

	var res domain.ResourceEntity
	err := postgres.WithTenantTx(ctx, e.pool, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		query := `
			SELECT id, tenant_id, cloud_account_id, provider, account_id, region,
				   resource_type, native_id, arn, name, status, tags,
				   created_at_provider, observed_at, observed_state, derived_state,
				   inferred_state, configuration_hash, monthly_cost_estimate,
				   created_at, updated_at
			FROM resource_entities
			WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL;
		`
		var provStr string
		var tagsJSON, obsJSON, derJSON, infJSON []byte

		row := tx.QueryRow(ctx, query, tenantID, resourceID)
		if err := row.Scan(
			&res.ID, &res.TenantID, &res.CloudAccountID, &provStr, &res.AccountID, &res.Region,
			&res.ResourceType, &res.NativeID, &res.ARN, &res.Name, &res.Status, &tagsJSON,
			&res.CreatedAtProvider, &res.ObservedAt, &obsJSON, &derJSON,
			&infJSON, &res.ConfigurationHash, &res.MonthlyCostEstimate,
			&res.CreatedAt, &res.UpdatedAt,
		); err != nil {
			return err
		}

		res.Provider = domain.ProviderType(provStr)
		_ = json.Unmarshal(tagsJSON, &res.Tags)
		_ = json.Unmarshal(obsJSON, &res.ObservedState)
		_ = json.Unmarshal(derJSON, &res.DerivedState)
		_ = json.Unmarshal(infJSON, &res.InferredState)
		return nil
	})

	return &res, err
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
		// Count total matching records with tenant isolation
		countQuery := `SELECT COUNT(*) FROM resource_entities WHERE tenant_id = $1 AND deleted_at IS NULL`
		args := []interface{}{filter.TenantID}
		argIdx := 2

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
			WHERE tenant_id = $1 AND deleted_at IS NULL`

		selectArgs := []interface{}{filter.TenantID}
		selectArgIdx := 2
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

// GetDependencies performs upstream/downstream dependency traversal and separates incoming vs outgoing.
func (e *QueryEngine) GetDependencies(ctx context.Context, tenantID, rootResourceID uuid.UUID, dir Direction, maxDepth int) (*DependencyGraphResult, error) {
	if e.pool == nil {
		return nil, errors.New("database pool is required")
	}
	if maxDepth <= 0 || maxDepth > 6 {
		maxDepth = 3
	}
	if dir == "" {
		dir = DirectionBoth
	}

	res := &DependencyGraphResult{
		RootResourceID:  rootResourceID,
		Direction:       dir,
		MaxDepth:        maxDepth,
		Nodes:           []domain.ResourceEntity{},
		Edges:           []domain.ResourceRelationship{},
		UpstreamNodes:   []domain.ResourceEntity{},
		DownstreamNodes: []domain.ResourceEntity{},
	}

	err := postgres.WithTenantTx(ctx, e.pool, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		// 1. Fetch root resource
		root, err := e.GetResource(ctx, tenantID, rootResourceID)
		if err == nil {
			res.RootResource = root
		}

		edgeMap := make(map[uuid.UUID]domain.ResourceRelationship)
		nodeIDMap := make(map[uuid.UUID]bool)
		upstreamNodeIDs := make(map[uuid.UUID]bool)
		downstreamNodeIDs := make(map[uuid.UUID]bool)

		nodeIDMap[rootResourceID] = true

		// 2. Outgoing traversal (Downstream: what root resource depends on)
		if dir == DirectionBoth || dir == DirectionDownstream {
			downstreamCTE := `
				WITH RECURSIVE downstream AS (
					SELECT id, tenant_id, source_resource_id, target_resource_id, relationship_type,
						   properties, is_implicit, confidence, provenance, discovered_at, updated_at, 1 AS depth
					FROM resource_relationships
					WHERE tenant_id = $1 AND source_resource_id = $2
					
					UNION ALL
					
					SELECT rr.id, rr.tenant_id, rr.source_resource_id, rr.target_resource_id, rr.relationship_type,
						   rr.properties, rr.is_implicit, rr.confidence, rr.provenance, rr.discovered_at, rr.updated_at, d.depth + 1
					FROM resource_relationships rr
					INNER JOIN downstream d ON rr.source_resource_id = d.target_resource_id
					WHERE rr.tenant_id = $1 AND d.depth < $3
				)
				SELECT id, tenant_id, source_resource_id, target_resource_id, relationship_type,
					   properties, is_implicit, confidence, provenance, discovered_at, updated_at
				FROM downstream;
			`
			rows, err := tx.Query(ctx, downstreamCTE, tenantID, rootResourceID, maxDepth)
			if err != nil {
				return fmt.Errorf("downstream CTE failed: %w", err)
			}
			for rows.Next() {
				var edge domain.ResourceRelationship
				var relType, provStr string
				var propsJSON []byte
				if err := rows.Scan(
					&edge.ID, &edge.TenantID, &edge.SourceResourceID, &edge.TargetResourceID,
					&relType, &propsJSON, &edge.IsImplicit, &edge.Confidence, &provStr,
					&edge.DiscoveredAt, &edge.UpdatedAt,
				); err != nil {
					rows.Close()
					return err
				}
				edge.RelationshipType = domain.RelationshipType(relType)
				edge.Provenance = domain.ProvenanceCategory(provStr)
				_ = json.Unmarshal(propsJSON, &edge.Properties)

				edgeMap[edge.ID] = edge
				nodeIDMap[edge.SourceResourceID] = true
				nodeIDMap[edge.TargetResourceID] = true
				if edge.TargetResourceID != rootResourceID {
					downstreamNodeIDs[edge.TargetResourceID] = true
				}
			}
			rows.Close()
		}

		// 3. Incoming traversal (Upstream: what resources depend on root)
		if dir == DirectionBoth || dir == DirectionUpstream {
			upstreamCTE := `
				WITH RECURSIVE upstream AS (
					SELECT id, tenant_id, source_resource_id, target_resource_id, relationship_type,
						   properties, is_implicit, confidence, provenance, discovered_at, updated_at, 1 AS depth
					FROM resource_relationships
					WHERE tenant_id = $1 AND target_resource_id = $2
					
					UNION ALL
					
					SELECT rr.id, rr.tenant_id, rr.source_resource_id, rr.target_resource_id, rr.relationship_type,
						   rr.properties, rr.is_implicit, rr.confidence, rr.provenance, rr.discovered_at, rr.updated_at, u.depth + 1
					FROM resource_relationships rr
					INNER JOIN upstream u ON rr.target_resource_id = u.source_resource_id
					WHERE rr.tenant_id = $1 AND u.depth < $3
				)
				SELECT id, tenant_id, source_resource_id, target_resource_id, relationship_type,
					   properties, is_implicit, confidence, provenance, discovered_at, updated_at
				FROM upstream;
			`
			rows, err := tx.Query(ctx, upstreamCTE, tenantID, rootResourceID, maxDepth)
			if err != nil {
				return fmt.Errorf("upstream CTE failed: %w", err)
			}
			for rows.Next() {
				var edge domain.ResourceRelationship
				var relType, provStr string
				var propsJSON []byte
				if err := rows.Scan(
					&edge.ID, &edge.TenantID, &edge.SourceResourceID, &edge.TargetResourceID,
					&relType, &propsJSON, &edge.IsImplicit, &edge.Confidence, &provStr,
					&edge.DiscoveredAt, &edge.UpdatedAt,
				); err != nil {
					rows.Close()
					return err
				}
				edge.RelationshipType = domain.RelationshipType(relType)
				edge.Provenance = domain.ProvenanceCategory(provStr)
				_ = json.Unmarshal(propsJSON, &edge.Properties)

				edgeMap[edge.ID] = edge
				nodeIDMap[edge.SourceResourceID] = true
				nodeIDMap[edge.TargetResourceID] = true
				if edge.SourceResourceID != rootResourceID {
					upstreamNodeIDs[edge.SourceResourceID] = true
				}
			}
			rows.Close()
		}

		for _, edge := range edgeMap {
			res.Edges = append(res.Edges, edge)
		}

		// 4. Batch query all discovered entity records
		allNodeIDs := make([]uuid.UUID, 0, len(nodeIDMap))
		for id := range nodeIDMap {
			allNodeIDs = append(allNodeIDs, id)
		}

		if len(allNodeIDs) > 0 {
			entQuery := `
				SELECT id, provider, account_id, region, resource_type, native_id, arn, name, status, observed_at
				FROM resource_entities
				WHERE tenant_id = $1 AND id = ANY($2) AND deleted_at IS NULL;
			`
			entRows, err := tx.Query(ctx, entQuery, tenantID, allNodeIDs)
			if err != nil {
				return fmt.Errorf("failed querying graph node entities: %w", err)
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
				res.Nodes = append(res.Nodes, ent)

				if upstreamNodeIDs[ent.ID] {
					res.UpstreamNodes = append(res.UpstreamNodes, ent)
				}
				if downstreamNodeIDs[ent.ID] {
					res.DownstreamNodes = append(res.DownstreamNodes, ent)
				}
			}
			if err := entRows.Err(); err != nil {
				return err
			}
		}

		res.UpstreamCount = len(res.UpstreamNodes)
		res.DownstreamCount = len(res.DownstreamNodes)
		return nil
	})

	return res, err
}

// GetBlastRadius performs outward traversal (compatibility wrapper for existing callers).
func (e *QueryEngine) GetBlastRadius(ctx context.Context, tenantID uuid.UUID, rootResourceID uuid.UUID, maxDepth int) ([]domain.ResourceEntity, []domain.ResourceRelationship, error) {
	graphResult, err := e.GetDependencies(ctx, tenantID, rootResourceID, DirectionDownstream, maxDepth)
	if err != nil {
		if e.pool == nil {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	return graphResult.Nodes, graphResult.Edges, nil
}

// GetGraphStats aggregates graph topology metrics for a tenant.
func (e *QueryEngine) GetGraphStats(ctx context.Context, tenantID uuid.UUID) (*GraphStats, error) {
	if e.pool == nil {
		return nil, errors.New("database pool is required")
	}

	stats := &GraphStats{
		NodesByType: make(map[string]int),
		EdgesByType: make(map[string]int),
		TopHubs:     []HubStat{},
	}

	err := postgres.WithTenantTx(ctx, e.pool, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		// 1. Total nodes
		_ = tx.QueryRow(ctx, `SELECT COUNT(*) FROM resource_entities WHERE tenant_id = $1 AND deleted_at IS NULL;`, tenantID).Scan(&stats.TotalNodes)

		// 2. Total edges
		_ = tx.QueryRow(ctx, `SELECT COUNT(*) FROM resource_relationships WHERE tenant_id = $1;`, tenantID).Scan(&stats.TotalEdges)

		// 3. Nodes by type
		nRows, err := tx.Query(ctx, `
			SELECT resource_type, COUNT(*)
			FROM resource_entities
			WHERE tenant_id = $1 AND deleted_at IS NULL
			GROUP BY resource_type
			ORDER BY COUNT(*) DESC;
		`, tenantID)
		if err == nil {
			defer nRows.Close()
			for nRows.Next() {
				var rType string
				var count int
				if err := nRows.Scan(&rType, &count); err == nil {
					stats.NodesByType[rType] = count
				}
			}
		}

		// 4. Edges by type
		eRows, err := tx.Query(ctx, `
			SELECT relationship_type, COUNT(*)
			FROM resource_relationships
			WHERE tenant_id = $1
			GROUP BY relationship_type
			ORDER BY COUNT(*) DESC;
		`, tenantID)
		if err == nil {
			defer eRows.Close()
			for eRows.Next() {
				var relType string
				var count int
				if err := eRows.Scan(&relType, &count); err == nil {
					stats.EdgesByType[relType] = count
				}
			}
		}

		// 5. Top connected hubs
		hubQuery := `
			SELECT r.id, r.name, r.resource_type, COUNT(*) AS edge_count
			FROM resource_entities r
			INNER JOIN resource_relationships rr 
			   ON (r.id = rr.source_resource_id OR r.id = rr.target_resource_id)
			WHERE r.tenant_id = $1 AND r.deleted_at IS NULL
			GROUP BY r.id, r.name, r.resource_type
			ORDER BY edge_count DESC
			LIMIT 5;
		`
		hRows, err := tx.Query(ctx, hubQuery, tenantID)
		if err == nil {
			defer hRows.Close()
			for hRows.Next() {
				var h HubStat
				if err := hRows.Scan(&h.ResourceID, &h.Name, &h.ResourceType, &h.EdgeCount); err == nil {
					stats.TopHubs = append(stats.TopHubs, h)
				}
			}
		}

		return nil
	})

	return stats, err
}
