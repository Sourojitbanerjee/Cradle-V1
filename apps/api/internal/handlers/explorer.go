package handlers

import (
	_ "embed"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed explorer.html
var explorerHTML []byte

type ExplorerHandler struct {
	pool    *pgxpool.Pool
	version string
}

func NewExplorerHandler(pool *pgxpool.Pool, version string) *ExplorerHandler {
	return &ExplorerHandler{
		pool:    pool,
		version: version,
	}
}

// ServeHTTP renders the self-contained visual dependency explorer web application.
func (h *ExplorerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(explorerHTML)
}
