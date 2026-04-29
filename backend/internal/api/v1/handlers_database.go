package apiv1

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

// DatabaseExportHandler serves the SQLite database file as a download.
// This is a manual handler (binary response, not JSON) — not part of the OpenAPI spec.
func (h *Handlers) DatabaseExportHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Open(h.dbPath)
		if err != nil {
			http.Error(w, "database file not found", http.StatusInternalServerError)
			return
		}
		defer f.Close()

		info, err := f.Stat()
		if err != nil {
			http.Error(w, "cannot stat database file", http.StatusInternalServerError)
			return
		}

		filename := fmt.Sprintf("media-gate-%s.db", time.Now().Format("2006-01-02"))

		w.Header().Set("Content-Type", "application/x-sqlite3")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
		http.ServeContent(w, r, filename, info.ModTime(), f)
	}
}
