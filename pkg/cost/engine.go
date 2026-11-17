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
