package apiv1

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/sumia01/media-gate/internal/auth"
)

// DatabaseExportHandler serves the SQLite database file as a download.
// This is a manual handler (binary response, not JSON) — not part of the OpenAPI spec.
// Only admin users can export the database.
func (h *Handlers) DatabaseExportHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := auth.UserIDFromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{Code: 401, Message: "unauthorized"})
			return
		}
		isAdmin, err := h.authSvc.IsUserAdmin(userID)
		if err != nil || !isAdmin {
			writeJSON(w, http.StatusForbidden, ErrorResponse{Code: 403, Message: "admin access required"})
			return
		}

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
