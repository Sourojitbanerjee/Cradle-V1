package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ResourceListResponse struct {
	Total     int                     `json:"total"`
	Resources []domain.ResourceEntity `json:"resources"`
}

type ResourceGraphResponse struct {
	RootResource   domain.ResourceEntity         `json:"root_resource"`
	Relationships  []domain.ResourceRelationship `json:"relationships"`
	ConnectedNodes []domain.ResourceEntity       `json:"connected_nodes"`
}

type ResourceHandler struct {
	pool *pgxpool.Pool
}

func NewResourceHandler(pool *pgxpool.Pool) *ResourceHandler {
	return &ResourceHandler{pool: pool}
}

// ListResources returns the paginated inventory of discovered resources for the tenant.
func (h *ResourceHandler) ListResources(w http.ResponseWriter, r *http.Request) {
	_, err := domain.TenantFromContext(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// In Phase 0, return initial structured response
	resp := ResourceListResponse{
		Total:     0,
		Resources: []domain.ResourceEntity{},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// GetResourceGraph returns the dependency graph around a specific resource.
func (h *ResourceHandler) GetResourceGraph(w http.ResponseWriter, r *http.Request) {
	_, err := domain.TenantFromContext(r.Context())
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

	resp := ResourceGraphResponse{
		RootResource: domain.ResourceEntity{
			ID:           resourceID,
			ResourceType: "aws:ec2:instance",
			Name:         "placeholder",
			Status:       domain.ResourceStatusRunning,
		},
		Relationships:  []domain.ResourceRelationship{},
		ConnectedNodes: []domain.ResourceEntity{},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
