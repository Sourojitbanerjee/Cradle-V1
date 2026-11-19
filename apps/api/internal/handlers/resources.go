package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/graph"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// GetResource returns a single canonical resource by its UUID.
func (h *ResourceHandler) GetResource(w http.ResponseWriter, r *http.Request) {
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

	res, err := h.queryEngine.GetResource(r.Context(), tc.TenantID, resourceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, `{"error": "resource not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error": "failed to retrieve resource"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// GetResourceGraph returns the dependency subgraph around a resource with upstream/downstream navigation.
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
	if depth < 1 {
		depth = 1
	}
	if depth > 10 {
		depth = 10
	}

	dirStr := r.URL.Query().Get("direction")
	var dir graph.Direction
	switch dirStr {
	case "upstream":
		dir = graph.DirectionUpstream
	case "downstream":
		dir = graph.DirectionDownstream
	default:
		dir = graph.DirectionBoth
	}

	result, err := h.queryEngine.GetDependencies(r.Context(), tc.TenantID, resourceID, dir, depth)
	if err != nil {
		http.Error(w, `{"error": "graph query failed"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// GetGraphStats returns topological infrastructure summary metrics for the tenant.
func (h *ResourceHandler) GetGraphStats(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		http.Error(w, `{"error": "database connection is required"}`, http.StatusServiceUnavailable)
		return
	}

	tc, err := domain.TenantFromContext(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	stats, err := h.queryEngine.GetGraphStats(r.Context(), tc.TenantID)
	if err != nil {
		http.Error(w, `{"error": "failed to query graph stats"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
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
