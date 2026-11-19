package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/storage/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Engine struct{ pool *pgxpool.Pool }

func NewEngine(pool *pgxpool.Pool) *Engine { return &Engine{pool: pool} }

type Filter struct {
	View, Search, Team, Application, Environment string
	MinMonthlyCost                               float64
	Limit, Offset                                int
}

var Views = []string{"production", "staging", "development", "unowned", "orphaned", "critical", "high-cost"}

func (f Filter) Validate() error {
	if f.View != "" {
		valid := false
		for _, view := range Views {
			if view == f.View {
				valid = true
			}
		}
		if !valid {
			return errors.New("invalid inventory view")
		}
	}
	if math.IsNaN(f.MinMonthlyCost) || math.IsInf(f.MinMonthlyCost, 0) || f.MinMonthlyCost < 0 {
		return errors.New("min_monthly_cost must be a finite nonnegative number")
	}
	if f.Limit < 1 || f.Limit > 500 || f.Offset < 0 {
		return errors.New("limit must be 1 to 500 and offset must be nonnegative")
	}
	return nil
}

type ListResult struct {
	Total     int                     `json:"total"`
	Limit     int                     `json:"limit"`
	Offset    int                     `json:"offset"`
	Resources []domain.ResourceEntity `json:"resources"`
}

type Group struct {
	Team                string  `json:"team"`
	Application         string  `json:"application"`
	Environment         string  `json:"environment"`
	ResourceCount       int     `json:"resource_count"`
	MonthlyCostEstimate float64 `json:"monthly_cost_estimate"`
}

type Summary struct {
	Total            int            `json:"total"`
	UnknownOwnership int            `json:"unknown_ownership"`
	Untagged         int            `json:"untagged"`
	Views            map[string]int `json:"views"`
	Groups           []Group        `json:"groups"`
}

func MatchesView(resource domain.ResourceEntity, view string, threshold float64) bool {
	op := resource.Operational
	switch view {
	case "":
		return true
	case "production", "staging", "development":
		return op.Environment.Value == view
	case "unowned":
		return op.MissingOwner
	case "orphaned":
		return op.Orphan.Value == "ORPHANED"
	case "critical":
		return op.Criticality.Value == "critical"
	case "high-cost":
		return resource.MonthlyCostEstimate >= threshold && resource.MonthlyCostEstimate > 0
	}
	return false
}

func FilterResources(resources []domain.ResourceEntity, filter Filter) *ListResult {
	if filter.View == "high-cost" {
		resources = append([]domain.ResourceEntity(nil), resources...)
		sort.SliceStable(resources, func(i, j int) bool { return resources[i].MonthlyCostEstimate > resources[j].MonthlyCostEstimate })
	}
	result := &ListResult{Limit: filter.Limit, Offset: filter.Offset, Resources: []domain.ResourceEntity{}}
	for _, resource := range resources {
		op := resource.Operational
		if !MatchesView(resource, filter.View, filter.MinMonthlyCost) {
			continue
		}
		if filter.Team != "" && op.Team.Value != filter.Team {
			continue
		}
		if filter.Application != "" && op.Application.Value != filter.Application {
			continue
		}
		if filter.Environment != "" && op.Environment.Value != filter.Environment {
			continue
		}
		if filter.Search != "" && !strings.Contains(strings.ToLower(resource.Name+" "+resource.NativeID+" "+resource.ARN), strings.ToLower(filter.Search)) {
			continue
		}
		if result.Total >= filter.Offset && len(result.Resources) < filter.Limit {
			result.Resources = append(result.Resources, resource)
		}
		result.Total++
	}
	return result
}

func Summarize(resources []domain.ResourceEntity, threshold float64) *Summary {
	summary := &Summary{Total: len(resources), Views: map[string]int{}, Groups: []Group{}}
	for _, view := range Views {
		summary.Views[view] = 0
	}
	groups := map[[3]string]*Group{}
	for _, resource := range resources {
		op := resource.Operational
		if op.MissingOwner {
			summary.UnknownOwnership++
		}
		if op.Untagged {
			summary.Untagged++
		}
		for _, view := range Views {
			if MatchesView(resource, view, threshold) {
				summary.Views[view]++
			}
		}
		key := [3]string{op.Team.Value, op.Application.Value, op.Environment.Value}
		if groups[key] == nil {
			groups[key] = &Group{Team: key[0], Application: key[1], Environment: key[2]}
		}
		groups[key].ResourceCount++
		groups[key].MonthlyCostEstimate += resource.MonthlyCostEstimate
	}
	for _, group := range groups {
		summary.Groups = append(summary.Groups, *group)
	}
	sort.Slice(summary.Groups, func(i, j int) bool {
		a, b := summary.Groups[i], summary.Groups[j]
		if a.Team != b.Team {
			return a.Team < b.Team
		}
		if a.Application != b.Application {
			return a.Application < b.Application
		}
		return a.Environment < b.Environment
	})
	return summary
}

func (e *Engine) Snapshot(ctx context.Context, tenantID uuid.UUID) ([]domain.ResourceEntity, error) {
	return e.ResourcesByID(ctx, tenantID, nil)
}

func (e *Engine) ResourcesByID(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) ([]domain.ResourceEntity, error) {
	resources := []domain.ResourceEntity{}
	err := postgres.WithTenantTx(ctx, e.pool, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rules, err := loadRules(ctx, tx, tenantID)
		if err != nil {
			return err
		}
		records := map[uuid.UUID][]domain.OwnershipRecord{}
		rows, err := tx.Query(ctx, `SELECT id, resource_id, owner_type, owner_id, source, confidence, is_verified
			FROM ownership_records WHERE tenant_id = $1 AND (source = 'manual' OR is_verified)
			AND ($2::uuid[] IS NULL OR resource_id = ANY($2)) ORDER BY id`, tenantID, ids)
		if err != nil {
			return err
		}
		for rows.Next() {
			var record domain.OwnershipRecord
			record.TenantID = tenantID
			if err := rows.Scan(&record.ID, &record.ResourceID, &record.OwnerType, &record.OwnerID, &record.Source, &record.Confidence, &record.IsVerified); err != nil {
				rows.Close()
				return err
			}
			records[record.ResourceID] = append(records[record.ResourceID], record)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT id, cloud_account_id, provider, account_id, region, resource_type, native_id, arn,
			COALESCE(name, ''), status, COALESCE(tags, '{}'::jsonb), created_at_provider, observed_at,
			COALESCE(observed_state, '{}'::jsonb), COALESCE(derived_state, '{}'::jsonb), COALESCE(inferred_state, '{}'::jsonb),
			configuration_hash, COALESCE(monthly_cost_estimate, 0), created_at, updated_at
			FROM resource_entities WHERE tenant_id = $1 AND deleted_at IS NULL AND ($2::uuid[] IS NULL OR id = ANY($2))
			ORDER BY resource_type, name, id`, tenantID, ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var resource domain.ResourceEntity
			resource.TenantID = tenantID
			if err := rows.Scan(&resource.ID, &resource.CloudAccountID, &resource.Provider, &resource.AccountID, &resource.Region,
				&resource.ResourceType, &resource.NativeID, &resource.ARN, &resource.Name, &resource.Status, &resource.Tags,
				&resource.CreatedAtProvider, &resource.ObservedAt, &resource.ObservedState, &resource.DerivedState, &resource.InferredState,
				&resource.ConfigurationHash, &resource.MonthlyCostEstimate, &resource.CreatedAt, &resource.UpdatedAt); err != nil {
				return err
			}
			resource.Operational = Resolve(resource, rules, records[resource.ID])
			resources = append(resources, resource)
		}
		return rows.Err()
	})
	return resources, err
}

func loadRules(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) ([]domain.OwnershipRule, error) {
	rules := []domain.OwnershipRule{}
	rows, err := tx.Query(ctx, `SELECT id, tenant_id, name, tag_key, tag_value, priority, enabled, assignments, created_at, updated_at
		FROM ownership_rules WHERE tenant_id = $1 ORDER BY priority DESC, id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rule domain.OwnershipRule
		if err := rows.Scan(&rule.ID, &rule.TenantID, &rule.Name, &rule.TagKey, &rule.TagValue, &rule.Priority, &rule.Enabled,
			&rule.Assignments, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (e *Engine) ListRules(ctx context.Context, tenantID uuid.UUID) ([]domain.OwnershipRule, error) {
	var rules []domain.OwnershipRule
	err := postgres.WithTenantTx(ctx, e.pool, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		rules, err = loadRules(ctx, tx, tenantID)
		return err
	})
	return rules, err
}

func (e *Engine) SaveRule(ctx context.Context, rule *domain.OwnershipRule, create bool) error {
	if err := rule.Validate(); err != nil {
		return err
	}
	return postgres.WithTenantTx(ctx, e.pool, rule.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		assignments, err := json.Marshal(rule.Assignments)
		if err != nil {
			return err
		}
		if create {
			rule.ID = uuid.New()
			return tx.QueryRow(ctx, `INSERT INTO ownership_rules (id, tenant_id, name, tag_key, tag_value, priority, enabled, assignments)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING created_at, updated_at`, rule.ID, rule.TenantID, rule.Name, rule.TagKey,
				rule.TagValue, rule.Priority, rule.Enabled, assignments).Scan(&rule.CreatedAt, &rule.UpdatedAt)
		}
		return tx.QueryRow(ctx, `UPDATE ownership_rules SET name=$3, tag_key=$4, tag_value=$5, priority=$6, enabled=$7,
			assignments=$8, updated_at=NOW() WHERE tenant_id=$1 AND id=$2 RETURNING created_at, updated_at`, rule.TenantID,
			rule.ID, rule.Name, rule.TagKey, rule.TagValue, rule.Priority, rule.Enabled, assignments).Scan(&rule.CreatedAt, &rule.UpdatedAt)
	})
}

func (e *Engine) DeleteRule(ctx context.Context, tenantID, ruleID uuid.UUID) error {
	return postgres.WithTenantTx(ctx, e.pool, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		result, err := tx.Exec(ctx, `DELETE FROM ownership_rules WHERE tenant_id=$1 AND id=$2`, tenantID, ruleID)
		if err != nil {
			return fmt.Errorf("delete ownership rule: %w", err)
		}
		if result.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return nil
	})
}
