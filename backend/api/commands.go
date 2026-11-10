package api

import (
	"net/url"
	"strconv"
)

func (h *Handler) command(path string, query url.Values) (any, error, bool) {
	var result any
	var err error
	manager := h.cfg.Manager

	switch path {
	case "/api/mark":
		result, err = manager.Mark()
	case "/api/start", "/api/stop":
		result, err = manager.SetMeasuring(path == "/api/start")
	case "/api/shutdown":
		result = map[string]bool{"ok": true}
	case "/api/interval":
		value := query.Get("ms")
		if value != "250" && value != "1000" {
			return nil, nil, false
		}

		interval, _ := strconv.Atoi(value)
		result, err = manager.SetInterval(interval)
	case "/api/target":
		result, err = manager.AddTarget(query.Get("host"), query.Get("port"))
	case "/api/target/remove":
		result, err = manager.RemoveTarget(query.Get("address"))
	case "/api/route/start":
		result, err = manager.StartRoute(query.Get("adapterId"), query.Get("address"))
	case "/api/diagnostics/start":
		if err = manager.CheckReady(); err == nil {
			result, err = h.cfg.Diagnosis.Start(query.Get("adapterId"))
		}
	case "/api/diagnostics/stop":
		result, err = h.cfg.Diagnosis.Stop(query.Get("runId"))
	case "/api/diagnostics/delete":
		if err = manager.CheckReady(); err == nil {
			result, err = h.cfg.Diagnosis.Delete(query.Get("runId"), query.Get("directory"))
		}
	case "/api/cod/relay/start", "/api/cs2/relay/start":
		game := "cod"
		if path == "/api/cs2/relay/start" {
			game = "cs2"
		}

		if err = manager.CheckReady(); err == nil {
			result, err = h.cfg.Relay.Start(game)
		}
	case "/api/cod/test-target/add":
		result, err = manager.AddControlTarget(query.Get("address"))
	case "/api/logs/pick":
		result, err = manager.PickLogDirectory()
	case "/api/logs/select":
		result, err = manager.SelectLogDirectory(query.Get("path"))
	case "/api/logs/reset":
		result, err = manager.ResetLogs(query.Get("directory"))
	default:
		return nil, nil, false
	}

	return result, err, true
}
