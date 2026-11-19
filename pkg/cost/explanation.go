package cost

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/storage/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ExplainCostChanges builds an evidence-based explanation of cost variances between two periods.
func (e *Engine) ExplainCostChanges(ctx context.Context, tenantID uuid.UUID, currentStart, currentEnd, previousStart, previousEnd time.Time) (*domain.CostExplanation, error) {
	now := time.Now().UTC()

	// Default to last 30 days vs prior 30 days if unset
	if currentEnd.IsZero() {
		currentEnd = now
	}
	if currentStart.IsZero() {
		currentStart = currentEnd.AddDate(0, 0, -30)
	}
	if previousEnd.IsZero() {
		previousEnd = currentStart
	}
	if previousStart.IsZero() {
		previousStart = previousEnd.AddDate(0, 0, -30)
	}

	explanation := &domain.CostExplanation{
		PreviousPeriodStart:    previousStart,
		PreviousPeriodEnd:      previousEnd,
		CurrentPeriodStart:     currentStart,
		CurrentPeriodEnd:       currentEnd,
		Currency:               "USD",
		PrimaryDrivers:         []domain.CostDriver{},
		TeamAttribution:        []domain.AttributionItem{},
		ApplicationAttribution: []domain.AttributionItem{},
		EnvironmentAttribution: []domain.AttributionItem{},
		Recommendations:        []string{},
		Source:                 "DERIVED_BY_CRADLE (Correlation of AWS Cost Explorer & Infrastructure State History)",
		GeneratedAt:            now,
	}

	if e.pool == nil {
		explanation.Headline = "Database connection unavailable; cost explanation cannot query historical snapshots."
		explanation.Direction = "STABLE"
		return explanation, nil
	}

	err := postgres.WithTenantTx(ctx, e.pool, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		// 1. Query previous period spend by service
		queryPrev := `
			SELECT service_name, SUM(amount) AS total
			FROM cost_snapshots
			WHERE period_start >= $1 AND period_end <= $2
			GROUP BY service_name;
		`
		rowsPrev, err := tx.Query(ctx, queryPrev, previousStart, previousEnd)
		if err != nil {
			return fmt.Errorf("failed querying previous cost snapshots: %w", err)
		}
		prevByService := make(map[string]float64)
		for rowsPrev.Next() {
			var svc string
			var amt float64
			if err := rowsPrev.Scan(&svc, &amt); err != nil {
				rowsPrev.Close()
				return err
			}
			prevByService[svc] = amt
			explanation.PreviousTotalAmount += amt
		}
		rowsPrev.Close()

		// 2. Query current period spend by service
		queryCurr := `
			SELECT service_name, SUM(amount) AS total
			FROM cost_snapshots
			WHERE period_start >= $1 AND period_end <= $2
			GROUP BY service_name;
		`
		rowsCurr, err := tx.Query(ctx, queryCurr, currentStart, currentEnd)
		if err != nil {
			return fmt.Errorf("failed querying current cost snapshots: %w", err)
		}
		currByService := make(map[string]float64)
		for rowsCurr.Next() {
			var svc string
			var amt float64
			if err := rowsCurr.Scan(&svc, &amt); err != nil {
				rowsCurr.Close()
				return err
			}
			currByService[svc] = amt
			explanation.CurrentTotalAmount += amt
		}
		rowsCurr.Close()

		explanation.PreviousTotalAmount = math.Round(explanation.PreviousTotalAmount*100) / 100
		explanation.CurrentTotalAmount = math.Round(explanation.CurrentTotalAmount*100) / 100
		explanation.DeltaAmount = math.Round((explanation.CurrentTotalAmount-explanation.PreviousTotalAmount)*100) / 100

		if explanation.PreviousTotalAmount > 0 {
			explanation.PercentageChange = math.Round((explanation.DeltaAmount/explanation.PreviousTotalAmount)*10000) / 100
		} else if explanation.CurrentTotalAmount > 0 {
			explanation.PercentageChange = 100.0
		}

		if explanation.DeltaAmount > 0.01 {
			explanation.Direction = "INCREASED"
		} else if explanation.DeltaAmount < -0.01 {
			explanation.Direction = "DECREASED"
		} else {
			explanation.Direction = "STABLE"
		}

		// 3. Collect all unique services across both periods
		allServices := make(map[string]bool)
		for s := range prevByService {
			allServices[s] = true
		}
		for s := range currByService {
			allServices[s] = true
		}

		type driverCandidate struct {
			service  string
			prev     float64
			curr     float64
			delta    float64
			pctChg   float64
		}
		var candidates []driverCandidate

		for svc := range allServices {
			prev := prevByService[svc]
			curr := currByService[svc]
			delta := curr - prev
			pct := 0.0
			if prev > 0 {
				pct = math.Round((delta/prev)*10000) / 100
			} else if curr > 0 {
				pct = 100.0
			}
			candidates = append(candidates, driverCandidate{
				service: svc,
				prev:    math.Round(prev*100) / 100,
				curr:    math.Round(curr*100) / 100,
				delta:   math.Round(delta*100) / 100,
				pctChg:  pct,
			})
		}

		// Sort by absolute delta descending
		sort.Slice(candidates, func(i, j int) bool {
			if explanation.Direction == "INCREASED" {
				return candidates[i].delta > candidates[j].delta
			}
			return math.Abs(candidates[i].delta) > math.Abs(candidates[j].delta)
		})

		// 4. Fetch recent resource entities created/modified in the current period to explain drivers
		resQuery := `
			SELECT name, resource_type, native_id, COALESCE(tags, '{}'::jsonb),
			       COALESCE(monthly_cost_estimate, 0), created_at
			FROM resource_entities
			WHERE deleted_at IS NULL AND created_at >= $1
			ORDER BY monthly_cost_estimate DESC
			LIMIT 50;
		`
		resRows, err := tx.Query(ctx, resQuery, currentStart)
		type entityChange struct {
			name        string
			resType     string
			nativeID    string
			tags        map[string]string
			costEst     float64
			createdAt   time.Time
		}
		var recentEntities []entityChange
		if err == nil {
			defer resRows.Close()
			for resRows.Next() {
				var ec entityChange
				if err := resRows.Scan(&ec.name, &ec.resType, &ec.nativeID, &ec.tags, &ec.costEst, &ec.createdAt); err == nil {
					recentEntities = append(recentEntities, ec)
				}
			}
		}

		// 5. Build Cost Drivers and correlate with resource changes
		driverLimit := 5
		if len(candidates) < driverLimit {
			driverLimit = len(candidates)
		}

		for i := 0; i < driverLimit; i++ {
			c := candidates[i]
			if math.Abs(c.delta) < 0.01 {
				continue
			}

			share := 0.0
			if explanation.DeltaAmount > 0 && c.delta > 0 {
				share = math.Round((c.delta/explanation.DeltaAmount)*10000) / 100
			}

			driver := domain.CostDriver{
				ServiceName:      c.service,
				PreviousAmount:   c.prev,
				CurrentAmount:    c.curr,
				DeltaAmount:      c.delta,
				PercentageChange: c.pctChg,
				ShareOfIncrease:  share,
				ResourceChanges:  []domain.ResourceChangeExplanation{},
			}

			// Generate natural language explanation for service driver
			if c.delta > 0 {
				driver.Explanation = fmt.Sprintf("%s spend grew +$%.2f (+%.1f%%) from $%.2f to $%.2f.",
					c.service, c.delta, c.pctChg, c.prev, c.curr)
			} else {
				driver.Explanation = fmt.Sprintf("%s spend reduced -$%.2f (%.1f%%) from $%.2f to $%.2f.",
					c.service, math.Abs(c.delta), c.pctChg, c.prev, c.curr)
			}

			// Correlate with discovered resource entities
			svcLower := strings.ToLower(c.service)
			for _, re := range recentEntities {
				matches := false
				switch {
				case (strings.Contains(svcLower, "kubernetes") || strings.Contains(svcLower, "eks")) &&
					(strings.Contains(re.resType, "eks") || strings.Contains(re.tags["kubernetes.io/cluster"], "true") || strings.Contains(re.name, "eks")):
					matches = true
				case (strings.Contains(svcLower, "relational database") || strings.Contains(svcLower, "rds")) &&
					strings.Contains(re.resType, "rds"):
					matches = true
				case (strings.Contains(svcLower, "virtual private cloud") || strings.Contains(svcLower, "vpc") || strings.Contains(svcLower, "gateway")) &&
					(strings.Contains(re.resType, "vpc") || strings.Contains(re.resType, "eip") || strings.Contains(re.name, "nat")):
					matches = true
				case (strings.Contains(svcLower, "compute") || strings.Contains(svcLower, "ec2")) &&
					(strings.Contains(re.resType, "ec2:instance") || strings.Contains(re.resType, "volume")):
					matches = true
				case strings.Contains(svcLower, "simple storage") && strings.Contains(re.resType, "s3"):
					matches = true
				}

				if matches {
					driver.ResourceChanges = append(driver.ResourceChanges, domain.ResourceChangeExplanation{
						ChangeType:      "ADDED",
						ResourceName:    re.name,
						ResourceType:    re.resType,
						NativeID:        re.nativeID,
						Explanation:     fmt.Sprintf("Provisioned on %s; adds ~$%.2f/mo run-rate", re.createdAt.Format("2006-01-02"), re.costEst),
						Team:            re.tags["team"],
						Application:     re.tags["application"],
						Environment:     re.tags["environment"],
						EstimatedImpact: re.costEst,
						ObservedAt:      re.createdAt,
					})
				}
			}

			explanation.PrimaryDrivers = append(explanation.PrimaryDrivers, driver)
		}

		// 6. Operational Attribution across Team, Application, Environment
		queryAttr := func(field string) []domain.AttributionItem {
			var items []domain.AttributionItem
			q := fmt.Sprintf(`
				SELECT COALESCE(NULLIF(re.tags->>'%s', ''), NULLIF(re.tags->>'%s', ''), 'UNKNOWN') AS tag_val,
				       SUM(cs.amount) AS total
				FROM cost_snapshots cs
				LEFT JOIN resource_entities re ON re.id = cs.resource_id
				WHERE cs.period_start >= $1 AND cs.period_end <= $2
				GROUP BY tag_val
				ORDER BY total DESC;
			`, field, strings.ToUpper(field[:1])+field[1:])
			rows, err := tx.Query(ctx, q, currentStart, currentEnd)
			if err != nil {
				return items
			}
			defer rows.Close()
			for rows.Next() {
				var val string
				var amt float64
				if err := rows.Scan(&val, &amt); err == nil {
					items = append(items, domain.AttributionItem{
						Name:          val,
						CurrentAmount: math.Round(amt*100) / 100,
					})
				}
			}
			return items
		}

		explanation.TeamAttribution = queryAttr("team")
		explanation.ApplicationAttribution = queryAttr("application")
		explanation.EnvironmentAttribution = queryAttr("environment")

		// 7. Check active waste findings to correlate avoidable spend
		var totalWasteSavings float64
		var wasteCount int
		_ = tx.QueryRow(ctx, `SELECT COUNT(*), COALESCE(SUM(estimated_monthly_savings), 0) FROM waste_findings WHERE status = 'active'`).Scan(&wasteCount, &totalWasteSavings)

		if wasteCount > 0 && totalWasteSavings > 0 {
			explanation.WasteCorrelation = fmt.Sprintf("Cradle detected %d active waste findings representing $%.2f/mo in potential savings that correlate with avoidable spend.", wasteCount, totalWasteSavings)
			explanation.Recommendations = append(explanation.Recommendations,
				fmt.Sprintf("Remediate %d identified waste findings to recover approximately $%.2f/mo in unused cloud resources.", wasteCount, totalWasteSavings),
			)
		}

		// 8. Generate recommendations from drivers
		for _, d := range explanation.PrimaryDrivers {
			if d.DeltaAmount > 50.0 {
				svcLower := strings.ToLower(d.ServiceName)
				switch {
				case strings.Contains(svcLower, "eks") || strings.Contains(svcLower, "kubernetes"):
					explanation.Recommendations = append(explanation.Recommendations,
						fmt.Sprintf("Audit worker node autoscaling and instance types in %s (+$%.2f increase).", d.ServiceName, d.DeltaAmount))
				case strings.Contains(svcLower, "rds") || strings.Contains(svcLower, "database"):
					explanation.Recommendations = append(explanation.Recommendations,
						fmt.Sprintf("Evaluate read-replica sizing and Multi-AZ storage allocations in %s (+$%.2f increase).", d.ServiceName, d.DeltaAmount))
				case strings.Contains(svcLower, "vpc") || strings.Contains(svcLower, "nat"):
					explanation.Recommendations = append(explanation.Recommendations,
						fmt.Sprintf("Review inter-AZ traffic patterns and high-volume NAT gateway throughput in %s (+$%.2f increase).", d.ServiceName, d.DeltaAmount))
				default:
					explanation.Recommendations = append(explanation.Recommendations,
						fmt.Sprintf("Review provisioning logs and commit schedules for %s (+$%.2f increase).", d.ServiceName, d.DeltaAmount))
				}
			}
		}

		if len(explanation.Recommendations) == 0 {
			explanation.Recommendations = append(explanation.Recommendations, "Maintain routine cost anomaly monitoring to prevent unexpected infrastructure surges.")
		}

		// 9. Synthesize executive headline
		var driverSnippets []string
		for _, d := range explanation.PrimaryDrivers {
			if d.DeltaAmount > 0 {
				driverSnippets = append(driverSnippets, fmt.Sprintf("%s +$%.0f", d.ServiceName, d.DeltaAmount))
			} else if d.DeltaAmount < 0 {
				driverSnippets = append(driverSnippets, fmt.Sprintf("%s -$%.0f", d.ServiceName, math.Abs(d.DeltaAmount)))
			}
		}

		if explanation.Direction == "INCREASED" {
			snippet := ""
			if len(driverSnippets) > 0 {
				snippet = fmt.Sprintf(" Driven by: %s.", strings.Join(driverSnippets, ", "))
			}
			explanation.Headline = fmt.Sprintf("Monthly spend increased %.1f%% (+$%.2f) from $%.2f to $%.2f.%s",
				explanation.PercentageChange, explanation.DeltaAmount, explanation.PreviousTotalAmount, explanation.CurrentTotalAmount, snippet)
		} else if explanation.Direction == "DECREASED" {
			snippet := ""
			if len(driverSnippets) > 0 {
				snippet = fmt.Sprintf(" Offset by: %s.", strings.Join(driverSnippets, ", "))
			}
			explanation.Headline = fmt.Sprintf("Monthly spend decreased %.1f%% (-$%.2f) from $%.2f to $%.2f.%s",
				math.Abs(explanation.PercentageChange), math.Abs(explanation.DeltaAmount), explanation.PreviousTotalAmount, explanation.CurrentTotalAmount, snippet)
		} else {
			explanation.Headline = fmt.Sprintf("Monthly spend remained stable at $%.2f (variance < 1%%).", explanation.CurrentTotalAmount)
		}

		return nil
	})

	return explanation, err
}
