package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Resolver resolves unresolved cross-resource relationships by mapping ARNs and Native IDs
// to canonical database UUIDs and persisting them into resource_relationships.
type Resolver struct{}

func NewResolver() *Resolver {
	return &Resolver{}
}

// ResolutionResult reports summary of resolved and unresolved edges.
type ResolutionResult struct {
	TotalCandidates int
	ResolvedCount   int
	UnresolvedCount int
}

// ResolveAndPersist resolves relationships and writes them into tx.
// tx must already be executing within the tenant's transaction context.
func (r *Resolver) ResolveAndPersist(
	ctx context.Context,
	tx pgx.Tx,
	tenantID uuid.UUID,
	accountID uuid.UUID,
	candidates []domain.ResourceRelationship,
) (*ResolutionResult, error) {
	if len(candidates) == 0 {
		return &ResolutionResult{}, nil
	}

	// 1. Build an in-memory index of ARN -> UUID and NativeID -> UUID for this tenant
	arnToID := make(map[string]uuid.UUID)
	nativeToID := make(map[string]uuid.UUID)

	query := `
		SELECT id, arn, native_id
		FROM resource_entities
		WHERE tenant_id = $1 AND deleted_at IS NULL;
	`
	rows, err := tx.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed querying resource index for resolution: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		var arn string
		var nativeID string
		if err := rows.Scan(&id, &arn, &nativeID); err != nil {
			return nil, fmt.Errorf("failed scanning resource index: %w", err)
		}
		if arn != "" {
			arnToID[arn] = id
		}
		if nativeID != "" {
			nativeToID[nativeID] = id
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 2. Resolve target resource IDs and persist valid edges
	insertRelQuery := `
		INSERT INTO resource_relationships (
			id, tenant_id, source_resource_id, target_resource_id, relationship_type,
			properties, is_implicit, discovered_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		)
		ON CONFLICT (tenant_id, source_resource_id, target_resource_id, relationship_type) DO UPDATE SET
			properties = EXCLUDED.properties,
			updated_at = EXCLUDED.updated_at;
	`

	res := &ResolutionResult{
		TotalCandidates: len(candidates),
	}
	now := time.Now().UTC()

	for _, cand := range candidates {
		rel := cand
		if rel.TenantID == uuid.Nil {
			rel.TenantID = tenantID
		}

		// Source resource resolution
		if rel.SourceResourceID == uuid.Nil {
			res.UnresolvedCount++
			continue
		}

		// Target resource resolution
		if rel.TargetResourceID == uuid.Nil {
			if rel.TargetARN != "" {
				if id, ok := arnToID[rel.TargetARN]; ok {
					rel.TargetResourceID = id
				}
			}
			if rel.TargetResourceID == uuid.Nil && rel.TargetNativeID != "" {
				if id, ok := nativeToID[rel.TargetNativeID]; ok {
					rel.TargetResourceID = id
				}
			}
			// Check properties fallback
			if rel.TargetResourceID == uuid.Nil && rel.Properties != nil {
				if targetARN, ok := rel.Properties["target_arn"].(string); ok && targetARN != "" {
					if id, ok := arnToID[targetARN]; ok {
						rel.TargetResourceID = id
					}
				}
				if rel.TargetResourceID == uuid.Nil {
					for _, key := range []string{"subnet_id", "vpc_id", "instance_id", "volume_id", "target_native_id"} {
						if nativeID, ok := rel.Properties[key].(string); ok && nativeID != "" {
							if id, ok := nativeToID[nativeID]; ok {
								rel.TargetResourceID = id
								break
							}
						}
					}
				}
			}
		}

		// If still unresolved or self-referencing, safely skip
		if rel.TargetResourceID == uuid.Nil || rel.SourceResourceID == rel.TargetResourceID {
			res.UnresolvedCount++
			continue
		}

		if rel.ID == uuid.Nil {
			rel.ID = uuid.New()
		}
		if rel.DiscoveredAt.IsZero() {
			rel.DiscoveredAt = now
		}
		rel.UpdatedAt = now

		propsJSON, _ := json.Marshal(rel.Properties)
		_, err := tx.Exec(ctx, insertRelQuery,
			rel.ID, rel.TenantID, rel.SourceResourceID, rel.TargetResourceID,
			string(rel.RelationshipType), propsJSON, rel.IsImplicit,
			rel.DiscoveredAt, rel.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed persisting resolved relationship: %w", err)
		}
		res.ResolvedCount++
	}

	return res, nil
}
