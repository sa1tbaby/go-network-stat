package api

import (
	"encoding/json"
	"net/http"
)

func respond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func respondResult(w http.ResponseWriter, result any, err error) {
	if err != nil {
		respond(w, http.StatusBadRequest, map[string]string{"error": err.Error()})

		return
	}

	respond(w, http.StatusOK, result)
}
