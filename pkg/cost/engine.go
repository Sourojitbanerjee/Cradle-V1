package cost

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/storage/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Engine manages cost aggregation, waste findings persistence, and savings reporting.
type Engine struct {
	pool *pgxpool.Pool
}

func NewEngine(pool *pgxpool.Pool) *Engine {
	return &Engine{pool: pool}
}

// ServiceCostSummary summarizes spend by cloud service.
type ServiceCostSummary struct {
	ServiceName string  `json:"service_name"`
	TotalAmount float64 `json:"total_amount"`
	Currency    string  `json:"currency"`
}

// WasteSummary groups identified waste and monthly savings by category.
type WasteSummary struct {
	TotalMonthlySavings float64            `json:"total_monthly_savings"`
	TotalFindingsCount  int                `json:"total_findings_count"`
	SavingsByCategory   map[string]float64 `json:"savings_by_category"`
	FindingsByCategory  map[string]int     `json:"findings_by_category"`
}

// GetAccountCostSummary returns spend broken down by cloud service over a date range.
func (e *Engine) GetAccountCostSummary(ctx context.Context, tenantID uuid.UUID, startDate, endDate time.Time) ([]ServiceCostSummary, error) {
	if e.pool == nil {
		return []ServiceCostSummary{}, nil
	}

	var summaries []ServiceCostSummary
	err := postgres.WithTenantTx(ctx, e.pool, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		query := `
			SELECT service_name, SUM(amount) AS total_amount, currency
			FROM cost_snapshots
			WHERE period_start >= $1 AND period_end <= $2
			GROUP BY service_name, currency
			ORDER BY total_amount DESC;
		`
		rows, err := tx.Query(ctx, query, startDate, endDate)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var s ServiceCostSummary
			if err := rows.Scan(&s.ServiceName, &s.TotalAmount, &s.Currency); err != nil {
				return err
			}
			summaries = append(summaries, s)
		}
		return rows.Err()
	})

	return summaries, err
}

// SaveWasteFindings idempotently persists detected waste findings.
func (e *Engine) SaveWasteFindings(ctx context.Context, tenantID uuid.UUID, findings []domain.WasteFinding) error {
	if e.pool == nil || len(findings) == 0 {
		return nil
	}

	return postgres.WithTenantTx(ctx, e.pool, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		query := `
			INSERT INTO waste_findings (
				id, tenant_id, cloud_account_id, resource_id, resource_arn,
				resource_type, resource_name, region, waste_type, confidence,
				estimated_monthly_savings, observation_period_days, facts,
				inference_reason, recommendation_summary, status, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
			)
			ON CONFLICT (tenant_id, resource_id, waste_type) DO UPDATE SET
				confidence = EXCLUDED.confidence,
				estimated_monthly_savings = EXCLUDED.estimated_monthly_savings,
				observation_period_days = EXCLUDED.observation_period_days,
				facts = EXCLUDED.facts,
				inference_reason = EXCLUDED.inference_reason,
				recommendation_summary = EXCLUDED.recommendation_summary,
				status = EXCLUDED.status,
				updated_at = EXCLUDED.updated_at;
		`

		for _, f := range findings {
			factsJSON, _ := json.Marshal(f.Facts)
			_, err := tx.Exec(ctx, query,
				f.ID, f.TenantID, f.CloudAccountID, f.ResourceID, f.ResourceARN,
				f.ResourceType, f.ResourceName, f.Region, string(f.WasteType), f.Confidence,
				f.EstimatedMonthlySavings, f.ObservationPeriodDays, factsJSON,
				f.InferenceReason, f.RecommendationSummary, string(f.Status), f.CreatedAt, f.UpdatedAt,
			)
			if err != nil {
				return fmt.Errorf("failed saving waste finding %s: %w", f.ResourceARN, err)
			}
		}

		return nil
	})
}

// GetWasteSummary aggregates active waste findings and potential monthly savings.
func (e *Engine) GetWasteSummary(ctx context.Context, tenantID uuid.UUID) (*WasteSummary, error) {
	summary := &WasteSummary{
		SavingsByCategory:  make(map[string]float64),
		FindingsByCategory: make(map[string]int),
	}

	if e.pool == nil {
		return summary, nil
	}

	err := postgres.WithTenantTx(ctx, e.pool, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		query := `
			SELECT waste_type, COUNT(*), SUM(estimated_monthly_savings)
			FROM waste_findings
			WHERE status = 'active'
			GROUP BY waste_type;
		`
		rows, err := tx.Query(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var wasteType string
			var count int
			var savings float64
			if err := rows.Scan(&wasteType, &count, &savings); err != nil {
				return err
			}
			summary.FindingsByCategory[wasteType] = count
			summary.SavingsByCategory[wasteType] = savings
			summary.TotalFindingsCount += count
			summary.TotalMonthlySavings += savings
		}
		return rows.Err()
	})

	return summary, err
}

// ListWasteFindings returns filtered waste findings.
func (e *Engine) ListWasteFindings(ctx context.Context, tenantID uuid.UUID, wasteType string, minSavings float64) ([]domain.WasteFinding, error) {
	if e.pool == nil {
		return []domain.WasteFinding{}, nil
	}

	var findings []domain.WasteFinding
	err := postgres.WithTenantTx(ctx, e.pool, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		query := `
			SELECT id, tenant_id, cloud_account_id, resource_id, resource_arn,
				   resource_type, resource_name, region, waste_type, confidence,
				   estimated_monthly_savings, observation_period_days, facts,
				   inference_reason, recommendation_summary, status, created_at, updated_at
			FROM waste_findings
			WHERE status = 'active'
		`
		args := []interface{}{}
		argIdx := 1

		if wasteType != "" {
			query += fmt.Sprintf(" AND waste_type = $%d", argIdx)
			args = append(args, wasteType)
			argIdx++
		}
		if minSavings > 0 {
			query += fmt.Sprintf(" AND estimated_monthly_savings >= $%d", argIdx)
			args = append(args, minSavings)
			argIdx++
		}

		query += " ORDER BY estimated_monthly_savings DESC LIMIT 100;"

		rows, err := tx.Query(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var f domain.WasteFinding
			var wasteTypeStr string
			var statusStr string
			var factsJSON []byte

			if err := rows.Scan(
				&f.ID, &f.TenantID, &f.CloudAccountID, &f.ResourceID, &f.ResourceARN,
				&f.ResourceType, &f.ResourceName, &f.Region, &wasteTypeStr, &f.Confidence,
				&f.EstimatedMonthlySavings, &f.ObservationPeriodDays, &factsJSON,
				&f.InferenceReason, &f.RecommendationSummary, &statusStr, &f.CreatedAt, &f.UpdatedAt,
			); err != nil {
				return err
			}

			f.WasteType = domain.WasteType(wasteTypeStr)
			f.Status = domain.WasteStatus(statusStr)
			_ = json.Unmarshal(factsJSON, &f.Facts)
			findings = append(findings, f)
		}
		return rows.Err()
	})

	return findings, err
}

// SaveMetricRollups persists telemetry summaries for resources.
func (e *Engine) SaveMetricRollups(ctx context.Context, tenantID uuid.UUID, rollups []domain.MetricRollup) error {
	if e.pool == nil || len(rollups) == 0 {
		return nil
	}

	return postgres.WithTenantTx(ctx, e.pool, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		query := `
			INSERT INTO metric_rollups (
				id, tenant_id, resource_id, metric_name, period_start, period_end,
				avg_value, p95_value, max_value, min_value, sample_count, unit, created_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW()
			);
		`
		for _, r := range rollups {
			id := r.ID
			if id == uuid.Nil {
				id = uuid.New()
			}
			if _, err := tx.Exec(ctx, query,
				id, tenantID, r.ResourceID, r.MetricName, r.PeriodStart, r.PeriodEnd,
				r.AvgValue, r.P95Value, r.MaxValue, r.MinValue, r.SampleCount, r.Unit,
			); err != nil {
				return fmt.Errorf("failed saving metric rollup for %s: %w", r.ResourceID, err)
			}
		}
		return nil
	})
}

// GetMetricRollups retrieves metric rollups for a list of resource IDs.
func (e *Engine) GetMetricRollups(ctx context.Context, tenantID uuid.UUID, resourceIDs []uuid.UUID) (map[uuid.UUID][]domain.MetricRollup, error) {
	result := make(map[uuid.UUID][]domain.MetricRollup)
	if e.pool == nil || len(resourceIDs) == 0 {
		return result, nil
	}

	err := postgres.WithTenantTx(ctx, e.pool, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		query := `
			SELECT id, tenant_id, resource_id, metric_name, period_start, period_end,
				   avg_value, p95_value, max_value, min_value, sample_count, unit
			FROM metric_rollups
			WHERE resource_id = ANY($1)
			ORDER BY period_start DESC;
		`
		rows, err := tx.Query(ctx, query, resourceIDs)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var r domain.MetricRollup
			if err := rows.Scan(
				&r.ID, &r.TenantID, &r.ResourceID, &r.MetricName, &r.PeriodStart, &r.PeriodEnd,
				&r.AvgValue, &r.P95Value, &r.MaxValue, &r.MinValue, &r.SampleCount, &r.Unit,
			); err != nil {
				return err
			}
			result[r.ResourceID] = append(result[r.ResourceID], r)
		}
		return rows.Err()
	})

	return result, err
}

// SaveCostSnapshots persists raw cost snapshots.
func (e *Engine) SaveCostSnapshots(ctx context.Context, tenantID uuid.UUID, snapshots []domain.CostSnapshot) error {
	if e.pool == nil || len(snapshots) == 0 {
		return nil
	}

	return postgres.WithTenantTx(ctx, e.pool, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		query := `
			INSERT INTO cost_snapshots (
				id, tenant_id, cloud_account_id, resource_id, resource_arn,
				service_name, usage_type, period_start, period_end,
				amount, currency, usage_quantity, usage_unit, dimensions, created_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
			);
		`
		for _, s := range snapshots {
			id := s.ID
			if id == uuid.Nil {
				id = uuid.New()
			}
			dimJSON, _ := json.Marshal(s.Dimensions)
			if _, err := tx.Exec(ctx, query,
				id, tenantID, s.CloudAccountID, s.ResourceID, s.ResourceARN,
				s.ServiceName, s.UsageType, s.PeriodStart, s.PeriodEnd,
				s.Amount, s.Currency, s.UsageQuantity, s.UsageUnit, dimJSON, s.CreatedAt,
			); err != nil {
				return fmt.Errorf("failed saving cost snapshot: %w", err)
			}
		}
		return nil
	})
}
