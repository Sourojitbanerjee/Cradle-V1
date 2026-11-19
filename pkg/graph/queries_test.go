package graph

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestQueryEngineNilPool(t *testing.T) {
	engine := NewQueryEngine(nil)

	// Without a pool, ListResources should return an empty result, not panic
	result, err := engine.ListResources(context.Background(), ResourceFilter{
		TenantID: uuid.New(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Total != 0 || len(result.Resources) != 0 {
		t.Errorf("expected zero resources, got %d", len(result.Resources))
	}
}

func TestResourceFilterDefaults(t *testing.T) {
	engine := NewQueryEngine(nil)

	// Test that unreasonable limits get capped
	filter := ResourceFilter{
		TenantID: uuid.New(),
		Limit:    9999,
	}

	result, err := engine.ListResources(context.Background(), filter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Limit != 500 {
		t.Errorf("expected limit to be capped at 500, got %d", result.Limit)
	}
}

func TestBlastRadiusNilPool(t *testing.T) {
	engine := NewQueryEngine(nil)

	entities, rels, err := engine.GetBlastRadius(context.Background(), uuid.New(), uuid.New(), 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entities != nil || rels != nil {
		t.Error("expected nil results from nil pool")
	}
}

func TestGetDependenciesNilPool(t *testing.T) {
	engine := NewQueryEngine(nil)

	res, err := engine.GetDependencies(context.Background(), uuid.New(), uuid.New(), DirectionBoth, 3)
	if err == nil {
		t.Fatal("expected error with nil pool")
	}
	if res != nil {
		t.Error("expected nil result with nil pool")
	}
}

func TestGetResourceNilPool(t *testing.T) {
	engine := NewQueryEngine(nil)

	res, err := engine.GetResource(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("expected error with nil pool")
	}
	if res != nil {
		t.Error("expected nil resource with nil pool")
	}
}

func TestGetGraphStatsNilPool(t *testing.T) {
	engine := NewQueryEngine(nil)

	stats, err := engine.GetGraphStats(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error with nil pool")
	}
	if stats != nil {
		t.Error("expected nil stats with nil pool")
	}
}
