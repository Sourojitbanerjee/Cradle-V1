package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type HealthResponse struct {
	Status    string    `json:"status"`
	Version   string    `json:"version"`
	Timestamp time.Time `json:"timestamp"`
	Database  string    `json:"database,omitempty"`
}

type HealthHandler struct {
	pool    *pgxpool.Pool
	version string
}

func NewHealthHandler(pool *pgxpool.Pool, version string) *HealthHandler {
	return &HealthHandler{pool: pool, version: version}
}

// Liveness returns 200 OK if the process is responsive.
func (h *HealthHandler) Liveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(HealthResponse{
		Status:    "ok",
		Version:   h.version,
		Timestamp: time.Now().UTC(),
	})
}

// Readiness checks database connectivity before declaring the service ready to receive traffic.
func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	dbStatus := "disabled"
	if h.pool != nil {
		ctx := r.Context()
		if err := h.pool.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(HealthResponse{
				Status:    "unhealthy",
				Version:   h.version,
				Timestamp: time.Now().UTC(),
				Database:  "disconnected",
			})
			return
		}
		dbStatus = "connected"
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(HealthResponse{
		Status:    "ready",
		Version:   h.version,
		Timestamp: time.Now().UTC(),
		Database:  dbStatus,
	})
}
