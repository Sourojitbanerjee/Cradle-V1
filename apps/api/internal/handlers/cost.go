package handlers

import (
	_ "embed"
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

//go:embed cost.html
var costHTML []byte

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

// Page serves the interactive Cloud Cost Intelligence dashboard.
func (h *CostHandler) Page(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(costHTML)
}

// GetCostSummary handles GET /api/v1/cost/summary.
func (h *CostHandler) GetCostSummary(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		http.Error(w, `{"error": "database connection is required"}`, http.StatusServiceUnavailable)
		return
	}

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
		"source":       "AWS Cost Explorer (Observed)",
		"services":     summaries,
	})
}

// GetCostBreakdown handles GET /api/v1/cost/breakdown?dimension=service|account|region|resource|team|application|environment&days=30
func (h *CostHandler) GetCostBreakdown(w http.ResponseWriter, r *http.Request) {
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
	dimension := q.Get("dimension")
	if dimension == "" {
		dimension = "service"
	}

	days := parseInt(q.Get("days"), 30)
	endDate := time.Now().UTC()
	startDate := endDate.Add(-time.Duration(days) * 24 * time.Hour)

	breakdown, err := h.costEngine.GetCostBreakdown(r.Context(), tc.TenantID, dimension, startDate, endDate)
	if err != nil {
		http.Error(w, `{"error": "failed to query cost breakdown"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(breakdown)
}

// GetCostTrends handles GET /api/v1/cost/trends?granularity=daily|monthly&days=30
func (h *CostHandler) GetCostTrends(w http.ResponseWriter, r *http.Request) {
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
	granularity := q.Get("granularity")
	if granularity != "monthly" {
		granularity = "daily"
	}

	days := parseInt(q.Get("days"), 30)
	endDate := time.Now().UTC()
	startDate := endDate.Add(-time.Duration(days) * 24 * time.Hour)

	trends, err := h.costEngine.GetCostTrends(r.Context(), tc.TenantID, granularity, startDate, endDate)
	if err != nil {
		http.Error(w, `{"error": "failed to query cost trends"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(trends)
}

// GetCostAnomalies handles GET /api/v1/cost/anomalies?days=30
func (h *CostHandler) GetCostAnomalies(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		http.Error(w, `{"error": "database connection is required"}`, http.StatusServiceUnavailable)
		return
	}

	tc, err := domain.TenantFromContext(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	days := parseInt(r.URL.Query().Get("days"), 30)
	endDate := time.Now().UTC()
	startDate := endDate.Add(-time.Duration(days) * 24 * time.Hour)

	anomalies, err := h.costEngine.DetectCostAnomalies(r.Context(), tc.TenantID, startDate, endDate)
	if err != nil {
		http.Error(w, `{"error": "failed detecting cost anomalies"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(anomalies)
}

// GetCostForecast handles GET /api/v1/cost/forecast?days=30
func (h *CostHandler) GetCostForecast(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		http.Error(w, `{"error": "database connection is required"}`, http.StatusServiceUnavailable)
		return
	}

	tc, err := domain.TenantFromContext(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	days := parseInt(r.URL.Query().Get("days"), 30)
	forecast, err := h.costEngine.GetCostForecast(r.Context(), tc.TenantID, days)
	if err != nil {
		http.Error(w, `{"error": "failed computing cost forecast"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(forecast)
}

// ExplainCostChanges handles GET & POST /api/v1/cost/explain.
// Answers "Why did our bill increase?" with evidence-based service drivers and resource correlations.
func (h *CostHandler) ExplainCostChanges(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		http.Error(w, `{"error": "database connection is required"}`, http.StatusServiceUnavailable)
		return
	}

	tc, err := domain.TenantFromContext(r.Context())
	if err != nil {
		http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
		return
	}

	now := time.Now().UTC()
	days := parseInt(r.URL.Query().Get("days"), 30)

	currEnd := now
	currStart := currEnd.AddDate(0, 0, -days)
	prevEnd := currStart
	prevStart := prevEnd.AddDate(0, 0, -days)

	// If explicit date query parameters are provided, parse them
	q := r.URL.Query()
	if v := q.Get("current_start"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			currStart = t
		}
	}
	if v := q.Get("current_end"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			currEnd = t
		}
	}
	if v := q.Get("previous_start"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			prevStart = t
		}
	}
	if v := q.Get("previous_end"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			prevEnd = t
		}
	}

	explanation, err := h.costEngine.ExplainCostChanges(r.Context(), tc.TenantID, currStart, currEnd, prevStart, prevEnd)
	if err != nil {
		http.Error(w, `{"error": "failed to generate cost explanation"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(explanation)
}

// GetWasteSummary handles GET /api/v1/waste/summary.
func (h *CostHandler) GetWasteSummary(w http.ResponseWriter, r *http.Request) {
	if h.pool == nil {
		http.Error(w, `{"error": "database connection is required"}`, http.StatusServiceUnavailable)
		return
	}

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
	if h.pool == nil {
		http.Error(w, `{"error": "database connection is required"}`, http.StatusServiceUnavailable)
		return
	}

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

	// 2. Fetch associated metric rollups for resources
	resourceIDs := make([]uuid.UUID, 0, len(resResult.Resources))
	for _, res := range resResult.Resources {
		resourceIDs = append(resourceIDs, res.ID)
	}

	metricsMap, err := h.costEngine.GetMetricRollups(r.Context(), tc.TenantID, resourceIDs)
	if err != nil {
		http.Error(w, `{"error": "failed retrieving metrics for waste analysis"}`, http.StatusInternalServerError)
		return
	}

	// 3. Run waste detector
	dc := waste.DetectionContext{
		Entities: resResult.Resources,
		Metrics:  metricsMap,
		Now:      time.Now().UTC(),
	}

	findings := h.detector.Detect(r.Context(), dc)

	// 4. Persist findings to database
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
