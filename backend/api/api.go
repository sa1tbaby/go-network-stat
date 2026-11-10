// Package api adapts the browser HTTP/JSON contract to domain services.
package api

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"sync"

	"github.com/sa1tbaby/go-network-stat/backend/contracts"
)

// NetworkManager supplies the operations exposed by the panel API.
type NetworkManager interface {
	Snapshot() any
	Stopping() bool
	CheckReady() error
	LogStatus() any
	RouteStatus(string, string) any
	Mark() (any, error)
	SetMeasuring(bool) (any, error)
	SetInterval(int) (any, error)
	AddTarget(string, string) (any, error)
	RemoveTarget(string) (any, error)
	StartRoute(string, string) (any, error)
	AddControlTarget(string) (any, error)
	PickLogDirectory() (any, error)
	SelectLogDirectory(string) (any, error)
	ResetLogs(string) (any, error)
}

type Config struct {
	Root      string
	Assets    fs.FS
	Port      int
	Manager   NetworkManager
	Diagnosis contracts.DiagnosisService
	Relay     contracts.RelayService
	Shutdown  func()
}

type Handler struct {
	cfg       Config
	commandMu sync.Mutex
}

func New(cfg Config) (*Handler, error) {
	if cfg.Manager == nil || cfg.Diagnosis == nil || cfg.Relay == nil {
		return nil, errors.New("API domain services are required")
	}

	if cfg.Port <= 0 {
		cfg.Port = 8765
	}

	return &Handler{cfg: cfg}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if r.Method == http.MethodPost && !h.acceptOrigin(r.Header.Get("Origin")) {
		http.Error(w, "Origin rejected", http.StatusForbidden)

		return
	}

	query := r.URL.Query()
	switch r.Method {
	case http.MethodGet:
		h.serveQuery(w, r, query)
	case http.MethodPost:
		// Serialize commands with storage changes and diagnostic starts.
		h.commandMu.Lock()
		defer h.commandMu.Unlock()

		if h.cfg.Manager.Stopping() {
			respond(w, http.StatusServiceUnavailable, map[string]string{
				"error": "The collector is shutting down.",
			})

			return
		}

		// Shutdown runs after the response is written, while the command is owned.
		if r.URL.Path == "/api/shutdown" && h.cfg.Shutdown != nil {
			defer h.cfg.Shutdown()
		}

		result, err, found := h.command(r.URL.Path, query)
		if !found {
			http.NotFound(w, r)

			return
		}

		respondResult(w, result, err)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) acceptOrigin(origin string) bool {
	return origin == "" ||
		origin == fmt.Sprintf("http://127.0.0.1:%d", h.cfg.Port) ||
		origin == fmt.Sprintf("http://localhost:%d", h.cfg.Port)
}
