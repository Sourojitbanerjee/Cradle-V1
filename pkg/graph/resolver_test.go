package graph

import (
	"context"
	"testing"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/google/uuid"
)

func TestResolverEmptyCandidates(t *testing.T) {
	resolver := NewResolver()
	res, err := resolver.ResolveAndPersist(context.Background(), nil, uuid.New(), uuid.New(), []domain.ResourceRelationship{})
	if err != nil {
		t.Fatalf("unexpected error on empty candidates: %v", err)
	}
	if res.TotalCandidates != 0 || res.ResolvedCount != 0 || res.UnresolvedCount != 0 {
		t.Errorf("expected 0 for all counts, got %+v", res)
	}
}
