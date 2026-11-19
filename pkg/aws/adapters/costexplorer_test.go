package adapters

import (
	"context"
	"testing"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

func TestFetchCostAndUsageNilClient(t *testing.T) {
	account := &domain.CloudAccount{
		ID:        uuid.New(),
		AccountID: "123456789012",
		TenantID:  uuid.New(),
	}

	_, err := FetchCostAndUsage(context.Background(), nil, account, "2026-10-01", "2026-10-31")
	if err == nil {
		t.Fatal("expected error when costexplorer client is nil, got nil")
	}
}
