package handlers

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/inventory"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed inventory.html
var inventoryHTML []byte

type InventoryHandler struct {
	pool   *pgxpool.Pool
	engine *inventory.Engine
}

func NewInventoryHandler(pool *pgxpool.Pool) *InventoryHandler {
	return &InventoryHandler{pool: pool, engine: inventory.NewEngine(pool)}
}

func (h *InventoryHandler) Page(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(inventoryHTML)
}

func inventoryJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func inventoryError(w http.ResponseWriter, status int, message string) {
	inventoryJSON(w, status, map[string]string{"error": message})
}

func (h *InventoryHandler) tenant(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	if h.pool == nil {
		inventoryError(w, http.StatusServiceUnavailable, "database connection is required")
		return uuid.Nil, false
	}
	tc, err := domain.TenantFromContext(r.Context())
	if err != nil || tc.TenantID == uuid.Nil {
		inventoryError(w, http.StatusUnauthorized, "unauthorized")
		return uuid.Nil, false
	}
	return tc.TenantID, true
}

func inventoryFilter(r *http.Request) (inventory.Filter, error) {
	q := r.URL.Query()
	filter := inventory.Filter{View: q.Get("view"), Search: q.Get("search"), Team: q.Get("team"), Application: q.Get("application"),
		Environment: q.Get("environment"), MinMonthlyCost: 100, Limit: 50}
	var err error
	if q.Has("limit") {
		filter.Limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil {
			return filter, errors.New("invalid limit")
		}
	}
	if q.Has("offset") {
		filter.Offset, err = strconv.Atoi(q.Get("offset"))
		if err != nil {
			return filter, errors.New("invalid offset")
		}
	}
	if q.Has("min_monthly_cost") {
		filter.MinMonthlyCost, err = strconv.ParseFloat(q.Get("min_monthly_cost"), 64)
		if err != nil {
			return filter, errors.New("invalid min_monthly_cost")
		}
	}
	return filter, filter.Validate()
}

func (h *InventoryHandler) ListResources(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	filter, err := inventoryFilter(r)
	if err != nil {
		inventoryError(w, http.StatusBadRequest, err.Error())
		return
	}
	resources, err := h.engine.Snapshot(r.Context(), tenantID)
	if err != nil {
		inventoryError(w, http.StatusInternalServerError, "failed to load operational inventory")
		return
	}
	inventoryJSON(w, http.StatusOK, inventory.FilterResources(resources, filter))
}

func (h *InventoryHandler) GetResource(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	resourceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		inventoryError(w, http.StatusBadRequest, "invalid resource ID")
		return
	}
	resources, err := h.engine.ResourcesByID(r.Context(), tenantID, []uuid.UUID{resourceID})
	if err != nil {
		inventoryError(w, http.StatusInternalServerError, "failed to load operational resource")
		return
	}
	if len(resources) == 0 {
		inventoryError(w, http.StatusNotFound, "resource not found")
		return
	}
	inventoryJSON(w, http.StatusOK, resources[0])
}

func (h *InventoryHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	filter, err := inventoryFilter(r)
	if err != nil {
		inventoryError(w, http.StatusBadRequest, err.Error())
		return
	}
	resources, err := h.engine.Snapshot(r.Context(), tenantID)
	if err != nil {
		inventoryError(w, http.StatusInternalServerError, "failed to summarize inventory")
		return
	}
	inventoryJSON(w, http.StatusOK, inventory.Summarize(resources, filter.MinMonthlyCost))
}

func (h *InventoryHandler) ListRules(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	rules, err := h.engine.ListRules(r.Context(), tenantID)
	if err != nil {
		inventoryError(w, http.StatusInternalServerError, "failed to load ownership rules")
		return
	}
	inventoryJSON(w, http.StatusOK, map[string]interface{}{"rules": rules})
}

func (h *InventoryHandler) SaveRule(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	var input struct {
		Name        string                      `json:"name"`
		TagKey      string                      `json:"tag_key"`
		TagValue    string                      `json:"tag_value"`
		Priority    int                         `json:"priority"`
		Enabled     bool                        `json:"enabled"`
		Assignments domain.InventoryAssignments `json:"assignments"`
	}
	input.Priority, input.Enabled = 100, true
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		inventoryError(w, http.StatusBadRequest, "invalid rule JSON")
		return
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		inventoryError(w, http.StatusBadRequest, "expected one JSON object")
		return
	}
	rule := domain.OwnershipRule{TenantID: tenantID, Name: input.Name, TagKey: input.TagKey, TagValue: input.TagValue,
		Priority: input.Priority, Enabled: input.Enabled, Assignments: input.Assignments}
	create := r.Method == http.MethodPost
	if !create {
		var err error
		rule.ID, err = uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			inventoryError(w, http.StatusBadRequest, "invalid rule ID")
			return
		}
	}
	if err := rule.Validate(); err != nil {
		inventoryError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.engine.SaveRule(r.Context(), &rule, create); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			inventoryError(w, http.StatusNotFound, "rule not found")
			return
		}
		inventoryError(w, http.StatusInternalServerError, "failed to save ownership rule")
		return
	}
	status := http.StatusOK
	if create {
		status = http.StatusCreated
	}
	inventoryJSON(w, status, rule)
}

func (h *InventoryHandler) DeleteRule(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.tenant(w, r)
	if !ok {
		return
	}
	ruleID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		inventoryError(w, http.StatusBadRequest, "invalid rule ID")
		return
	}
	if err := h.engine.DeleteRule(r.Context(), tenantID, ruleID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			inventoryError(w, http.StatusNotFound, "rule not found")
			return
		}
		inventoryError(w, http.StatusInternalServerError, "failed to delete ownership rule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
