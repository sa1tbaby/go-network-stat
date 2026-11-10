package api

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

func (h *Handler) asset(w http.ResponseWriter, name, kind string) {
	var content []byte
	var err error

	if h.cfg.Assets != nil {
		content, err = fs.ReadFile(h.cfg.Assets, name)
	} else {
		content, err = os.ReadFile(filepath.Join(h.cfg.Root, name))
	}

	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})

		return
	}

	w.Header().Set("Content-Type", kind)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}
