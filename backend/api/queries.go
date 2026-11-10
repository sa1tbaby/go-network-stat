package api

import (
	"net/http"
	"net/url"
)

func (h *Handler) serveQuery(w http.ResponseWriter, r *http.Request, query url.Values) {
	var result any
	manager := h.cfg.Manager

	switch r.URL.Path {
	case "/", "/dashboard.html":
		h.asset(w, "dashboard.html", "text/html; charset=utf-8")

		return
	case "/api/status":
		result = manager.Snapshot()
	case "/api/logs":
		result = manager.LogStatus()
	case "/api/diagnostics":
		result = h.cfg.Diagnosis.Status()
	case "/api/official-relays":
		h.asset(w, "official-relays.json", "application/json; charset=utf-8")

		return
	case "/api/cod/test-targets":
		h.asset(w, "cod-test-targets.json", "application/json; charset=utf-8")

		return
	case "/api/cod/relay/status":
		result = h.cfg.Relay.Status("cod")
	case "/api/cs2/relay/status":
		result = h.cfg.Relay.Status("cs2")
	case "/api/route/status":
		result = manager.RouteStatus(query.Get("adapterId"), query.Get("address"))
	default:
		http.NotFound(w, r)

		return
	}

	respond(w, http.StatusOK, result)
}
