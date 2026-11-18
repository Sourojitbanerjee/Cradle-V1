package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/graph"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ResourceHandler struct {
	pool        *pgxpool.Pool
	queryEngine *graph.QueryEngine
}

func NewResourceHandler(pool *pgxpool.Pool) *ResourceHandler {
	return &ResourceHandler{
		pool:        pool,
		queryEngine: graph.NewQueryEngine(pool),
	}
}

// ListResources returns the paginated, filtered inventory of discovered resources.
func (h *ResourceHandler) ListResources(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		http.Error(w, `{"error": "database connection is required"}`, http.StatusServiceUnavailable)
		return
	}

	tc, err := domain.TenantFromContext(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	q := r.URL.Query()
	limit := 50
	offset := 0
	if v := q.Get("limit"); v != "" {
		if n := parseInt(v, 50); n > 0 {
			limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n := parseInt(v, 0); n >= 0 {
			offset = n
		}
	}

	filter := graph.ResourceFilter{
		TenantID:     tc.TenantID,
		Region:       q.Get("region"),
		ResourceType: q.Get("type"),
		Status:       q.Get("status"),
		NameContains: q.Get("search"),
		Limit:        limit,
		Offset:       offset,
	}

	result, err := h.queryEngine.ListResources(r.Context(), filter)
	if err != nil {
		http.Error(w, `{"error": "failed to query resources"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// GetResourceGraph returns the blast radius / dependency subgraph around a resource.
func (h *ResourceHandler) GetResourceGraph(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		http.Error(w, `{"error": "database connection is required"}`, http.StatusServiceUnavailable)
		return
	}

	tc, err := domain.TenantFromContext(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	resourceIDStr := chi.URLParam(r, "id")
	resourceID, err := uuid.Parse(resourceIDStr)
	if err != nil {
		http.Error(w, `{"error": "invalid resource ID format"}`, http.StatusBadRequest)
		return
	}

	depth := parseInt(r.URL.Query().Get("depth"), 3)

	entities, rels, err := h.queryEngine.GetBlastRadius(r.Context(), tc.TenantID, resourceID, depth)
	if err != nil {
		http.Error(w, `{"error": "graph query failed"}`, http.StatusInternalServerError)
		return
	}

	resp := map[string]interface{}{
		"root_resource_id": resourceID,
		"depth":            depth,
		"nodes":            entities,
		"edges":            rels,
		"node_count":       len(entities),
		"edge_count":       len(rels),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func parseInt(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	val := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			val = val*10 + int(c-'0')
		} else {
			return defaultVal
		}
	}
	return val
}
