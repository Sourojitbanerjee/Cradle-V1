package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/cradle-dev/cradle/pkg/cost"
	"github.com/cradle-dev/cradle/pkg/domain"
	"github.com/cradle-dev/cradle/pkg/graph"
	"github.com/cradle-dev/cradle/pkg/waste"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CostHandler struct {
	pool        *pgxpool.Pool
	costEngine  *cost.Engine
	detector    *waste.Detector
	queryEngine *graph.QueryEngine
}

func NewCostHandler(pool *pgxpool.Pool) *CostHandler {
	return &CostHandler{
		pool:        pool,
		costEngine:  cost.NewEngine(pool),
		detector:    waste.NewDetector(),
		queryEngine: graph.NewQueryEngine(pool),
	}
}

// GetCostSummary handles GET /api/v1/cost/summary.
func (h *CostHandler) GetCostSummary(w http.ResponseWriter, r *http.Request) {
	tc, err := domain.TenantFromContext(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	days := parseInt(r.URL.Query().Get("days"), 30)
	endDate := time.Now().UTC()
	startDate := endDate.Add(-time.Duration(days) * 24 * time.Hour)

	summaries, err := h.costEngine.GetAccountCostSummary(r.Context(), tc.TenantID, startDate, endDate)
	if err != nil {
		http.Error(w, `{"error": "failed to query cost summary"}`, http.StatusInternalServerError)
		return
	}

	totalCost := 0.0
	for _, s := range summaries {
		totalCost += s.TotalAmount
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"period_days":  days,
		"period_start": startDate,
		"period_end":   endDate,
		"total_cost":   totalCost,
		"currency":     "USD",
		"services":     summaries,
	})
}

// GetWasteSummary handles GET /api/v1/waste/summary.
func (h *CostHandler) GetWasteSummary(w http.ResponseWriter, r *http.Request) {
	tc, err := domain.TenantFromContext(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	summary, err := h.costEngine.GetWasteSummary(r.Context(), tc.TenantID)
	if err != nil {
		http.Error(w, `{"error": "failed to retrieve waste summary"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(summary)
}

// ListWasteFindings handles GET /api/v1/waste/findings.
func (h *CostHandler) ListWasteFindings(w http.ResponseWriter, r *http.Request) {
	tc, err := domain.TenantFromContext(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	q := r.URL.Query()
	wasteType := q.Get("waste_type")
	minSavings := 0.0
	if rawSavings := q.Get("min_savings"); rawSavings != "" {
		minSavings, _ = strconv.ParseFloat(rawSavings, 64)
	}

	findings, err := h.costEngine.ListWasteFindings(r.Context(), tc.TenantID, wasteType, minSavings)
	if err != nil {
		http.Error(w, `{"error": "failed to query waste findings"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"total":    len(findings),
		"findings": findings,
	})
}

// AnalyzeWaste handles POST /api/v1/waste/analyze.
// Runs the waste detector against current discovered resources and persists findings.
func (h *CostHandler) AnalyzeWaste(w http.ResponseWriter, r *http.Request) {
	tc, err := domain.TenantFromContext(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	// 1. Fetch all active resources for tenant
	resResult, err := h.queryEngine.ListResources(r.Context(), graph.ResourceFilter{
		TenantID: tc.TenantID,
		Limit:    500,
	})
	if err != nil {
		http.Error(w, `{"error": "failed retrieving resources for waste analysis"}`, http.StatusInternalServerError)
		return
	}

	// 2. Run waste detector
	dc := waste.DetectionContext{
		Entities: resResult.Resources,
		Metrics:  make(map[uuid.UUID][]domain.MetricRollup),
		Now:      time.Now().UTC(),
	}

	findings := h.detector.Detect(r.Context(), dc)

	// 3. Persist findings to database
	if err := h.costEngine.SaveWasteFindings(r.Context(), tc.TenantID, findings); err != nil {
		http.Error(w, `{"error": "failed saving waste findings"}`, http.StatusInternalServerError)
		return
	}

	totalSavings := 0.0
	for _, f := range findings {
		totalSavings += f.EstimatedMonthlySavings
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":                  "completed",
		"analyzed_resource_count": len(resResult.Resources),
		"findings_count":          len(findings),
		"total_monthly_savings":   totalSavings,
		"findings":                findings,
	})
}
