package cost

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

func TestCostEngineNilPool(t *testing.T) {
	engine := NewEngine(nil)
	ctx := context.Background()
	tenantID := uuid.New()

	// 1. GetAccountCostSummary on nil pool returns empty without error
	summaries, err := engine.GetAccountCostSummary(ctx, tenantID, time.Now().Add(-30*24*time.Hour), time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("expected empty summaries, got %d", len(summaries))
	}

	// 2. SaveWasteFindings on nil pool returns nil without error
	err = engine.SaveWasteFindings(ctx, tenantID, []domain.WasteFinding{
		{ID: uuid.New(), TenantID: tenantID},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 3. GetWasteSummary on nil pool returns empty initialized summary
	summary, err := engine.GetWasteSummary(ctx, tenantID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary.TotalMonthlySavings != 0 || summary.TotalFindingsCount != 0 {
		t.Errorf("expected 0 savings and count, got %v", summary)
	}

	// 4. ListWasteFindings on nil pool returns empty slice
	findings, err := engine.ListWasteFindings(ctx, tenantID, "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(findings))
	}
}

func TestCostIntelligenceNilPool(t *testing.T) {
	engine := NewEngine(nil)
	ctx := context.Background()
	tenantID := uuid.New()
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -30)

	t.Run("GetCostBreakdown", func(t *testing.T) {
		for _, dim := range []string{"service", "account", "region", "team", "application", "environment", "resource"} {
			res, err := engine.GetCostBreakdown(ctx, tenantID, dim, start, now)
			if err != nil {
				t.Fatalf("unexpected error on %s: %v", dim, err)
			}
			if res.Dimension != dim || res.TotalAmount != 0 || len(res.Items) != 0 {
				t.Errorf("expected empty breakdown for %s, got %+v", dim, res)
			}
			if res.Source != "AWS Cost Explorer (Observed)" {
				t.Errorf("expected observed source, got %s", res.Source)
			}
		}
	})

	t.Run("GetCostTrends", func(t *testing.T) {
		for _, gran := range []string{"daily", "monthly"} {
			trends, err := engine.GetCostTrends(ctx, tenantID, gran, start, now)
			if err != nil {
				t.Fatalf("unexpected error on %s: %v", gran, err)
			}
			if trends.Granularity != gran || trends.TotalAmount != 0 || len(trends.Points) != 0 {
				t.Errorf("expected empty trends for %s, got %+v", gran, trends)
			}
		}
	})

	t.Run("DetectCostAnomalies", func(t *testing.T) {
		anomalies, err := engine.DetectCostAnomalies(ctx, tenantID, start, now)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(anomalies) != 0 {
			t.Errorf("expected 0 anomalies on nil pool, got %d", len(anomalies))
		}
	})

	t.Run("GetCostForecast", func(t *testing.T) {
		forecast, err := engine.GetCostForecast(ctx, tenantID, 30)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if forecast.ForecastDays != 30 || forecast.Confidence != 0.85 {
			t.Errorf("unexpected forecast metadata: %+v", forecast)
		}
		if forecast.Source != "INFERRED_BY_CRADLE" {
			t.Errorf("expected INFERRED_BY_CRADLE source, got %s", forecast.Source)
		}
	})

	t.Run("ExplainCostChanges", func(t *testing.T) {
		explanation, err := engine.ExplainCostChanges(ctx, tenantID, start, now, start.AddDate(0, 0, -30), start)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if explanation.Direction != "STABLE" || !strings.Contains(strings.ToLower(explanation.Headline), "database connection unavailable") {
			t.Errorf("unexpected explanation on nil pool: %+v", explanation)
		}
		if explanation.Source == "" {
			t.Error("expected non-empty source provenance")
		}
	})
}

func TestSortBreakdownItems(t *testing.T) {
	items := []domain.CostBreakdownItem{
		{Key: "rds", Amount: 200},
		{Key: "eks", Amount: 500},
		{Key: "s3", Amount: 50},
	}
	sortBreakdownItems(items)
	if items[0].Key != "eks" || items[1].Key != "rds" || items[2].Key != "s3" {
		t.Errorf("expected sorted desc by amount, got %+v", items)
	}
}

