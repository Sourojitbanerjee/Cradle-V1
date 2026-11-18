package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

// Root provides an informative landing page for browser requests or JSON summary for API clients.
func (h *HealthHandler) Root(w http.ResponseWriter, r *http.Request) {
	dbStatus := "disconnected"
	if h.pool != nil {
		if err := h.pool.Ping(r.Context()); err == nil {
			dbStatus = "connected"
		}
	}

	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Cradle Control Plane</title>
    <style>
        :root {
            --bg: #0b0f19;
            --card-bg: #111827;
            --border: #1f2937;
            --text: #f3f4f6;
            --text-dim: #9ca3af;
            --primary: #3b82f6;
            --success: #10b981;
        }
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
            background: var(--bg);
            color: var(--text);
            margin: 0;
            padding: 40px 20px;
            display: flex;
            justify-content: center;
        }
        .container {
            max-width: 720px;
            width: 100%%;
        }
        .header {
            margin-bottom: 32px;
            border-bottom: 1px solid var(--border);
            padding-bottom: 24px;
        }
        h1 {
            font-size: 28px;
            font-weight: 700;
            margin: 0 0 8px 0;
            display: flex;
            align-items: center;
            gap: 12px;
        }
        .badge {
            font-size: 13px;
            font-weight: 600;
            padding: 4px 10px;
            border-radius: 9999px;
            background: rgba(16, 185, 129, 0.15);
            color: var(--success);
            border: 1px solid rgba(16, 185, 129, 0.3);
        }
        .subtitle {
            color: var(--text-dim);
            font-size: 15px;
            margin: 0;
        }
        .card {
            background: var(--card-bg);
            border: 1px solid var(--border);
            border-radius: 12px;
            padding: 24px;
            margin-bottom: 24px;
        }
        .status-grid {
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: 16px;
            margin-bottom: 8px;
        }
        .status-item {
            background: rgba(255, 255, 255, 0.02);
            border: 1px solid var(--border);
            border-radius: 8px;
            padding: 12px 16px;
        }
        .status-label {
            font-size: 12px;
            text-transform: uppercase;
            letter-spacing: 0.05em;
            color: var(--text-dim);
            margin-bottom: 4px;
        }
        .status-val {
            font-size: 15px;
            font-weight: 600;
            color: var(--text);
        }
        h2 {
            font-size: 18px;
            margin: 0 0 16px 0;
        }
        ul.links {
            list-style: none;
            padding: 0;
            margin: 0;
            display: flex;
            flex-direction: column;
            gap: 10px;
        }
        ul.links li a {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 10px 14px;
            background: rgba(255, 255, 255, 0.02);
            border: 1px solid var(--border);
            border-radius: 6px;
            color: var(--text);
            text-decoration: none;
            font-family: monospace;
            font-size: 14px;
            transition: all 0.15s ease;
        }
        ul.links li a:hover {
            background: rgba(59, 130, 246, 0.1);
            border-color: var(--primary);
            color: var(--primary);
        }
        .method {
            font-size: 11px;
            font-weight: 700;
            padding: 2px 6px;
            border-radius: 4px;
            background: rgba(59, 130, 246, 0.2);
            color: var(--primary);
        }
        .footer {
            color: var(--text-dim);
            font-size: 13px;
            line-height: 1.6;
        }
        code {
            background: rgba(255, 255, 255, 0.08);
            padding: 2px 6px;
            border-radius: 4px;
            font-size: 13px;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>Cradle Control Plane <span class="badge">Online</span></h1>
            <p class="subtitle">Cloud Infrastructure Control Plane &amp; Dependency Graph Engine</p>
        </div>

        <div class="card">
            <h2>System Status</h2>
            <div class="status-grid">
                <div class="status-item">
                    <div class="status-label">Version</div>
                    <div class="status-val">%s</div>
                </div>
                <div class="status-item">
                    <div class="status-label">PostgreSQL Database</div>
                    <div class="status-val">%s</div>
                </div>
            </div>
        </div>

        <div class="card">
            <h2>Core API Endpoints</h2>
            <ul class="links">
                <li><a href="/healthz"><span><span class="method">GET</span> /healthz</span> <span>Process Liveness &rarr;</span></a></li>
                <li><a href="/readyz"><span><span class="method">GET</span> /readyz</span> <span>Database Readiness &rarr;</span></a></li>
                <li><a href="/api/v1/tenants"><span><span class="method">GET</span> /api/v1/tenants</span> <span>Registered Tenants &rarr;</span></a></li>
            </ul>
        </div>

        <div class="card footer">
            <strong>Authenticated API Usage:</strong><br>
            Protected endpoints (e.g. <code>/api/v1/accounts</code>, <code>/api/v1/resources</code>, <code>/api/v1/waste/summary</code>) require the <code>X-Tenant-ID</code> HTTP header.<br>
            Default Dev Tenant ID: <code>a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11</code>
        </div>
    </div>
</body>
</html>`, h.version, dbStatus)
		_, _ = w.Write([]byte(html))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"service":   "Cradle Control Plane",
		"version":   h.version,
		"status":    "online",
		"database":  dbStatus,
		"timestamp": time.Now().UTC(),
		"endpoints": map[string]string{
			"health":    "/healthz",
			"ready":     "/readyz",
			"tenants":   "/api/v1/tenants",
			"accounts":  "/api/v1/accounts",
			"resources": "/api/v1/resources",
			"cost":      "/api/v1/cost/summary",
			"waste":     "/api/v1/waste/summary",
		},
	})
}
