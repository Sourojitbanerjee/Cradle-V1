package cost

import (
	"context"
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
