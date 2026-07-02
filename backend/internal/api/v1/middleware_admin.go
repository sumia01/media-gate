package apiv1

import (
	"context"
	"net/http"

	"github.com/sumia01/media-gate/internal/auth"
)

// adminOnlyOps lists OpenAPI operationIDs that require admin privileges.
// Any operation NOT listed here is accessible to all authenticated users.
var adminOnlyOps = map[string]struct{}{
	// Settings
	"listSettings":              {},
	"updateSettings":            {},
	"testTmdbConnection":        {},
	"testTvdbConnection":        {},
	"testQbittorrentConnection": {},
	"testFlaresolverrConnection": {},
	"testDiscordConnection":     {},
	"testOpenSubtitlesConnection": {},
	"testPlexConnection":        {},

	// Library management (CRUD, scan, sync — but NOT listLibraries/getLibrary/listMediaItems)
	"createLibrary": {},
	"updateLibrary": {},
	"deleteLibrary": {},
	"browseFolder":  {},
	"scanLibrary":   {},
	"triggerSync":   {},
	"triggerMatch":  {},

	// Media operations — ONLY the destructive ones that remove tracked files
	// from disk are admin-gated (the original security finding). Add/request,
	// match, resync and status-mutation stay available to non-admin users, who
	// reach them from the (non-admin) discover, media-detail and downloads views
	// — gating those would break the request workflow.
	"deleteMediaItem": {},
	"deleteDownload":  {},

	// Indexer management (NOT searchIndexers — users need torrent search)
	"listIndexerDefinitions": {},
	"getIndexerDefinition":   {},
	"listIndexers":           {},
	"createIndexer":          {},
	"getIndexer":             {},
	"updateIndexer":          {},
	"deleteIndexer":          {},
	"testIndexerConnection":  {},

	// Media profiles
	"listMediaProfiles":      {},
	"createMediaProfile":     {},
	"getMediaProfile":        {},
	"updateMediaProfile":     {},
	"deleteMediaProfile":     {},
	"testMediaProfileSearch": {},

	// Workers & jobs
	"listJobs":    {},
	"listWorkers": {},
	"runWorker":   {},

	// Updates
	"getUpdateStatus": {},
	"checkForUpdate":  {},
	"applyUpdate":     {},

	// Plex configuration
	"listPlexSections":   {},
	"listPlexMappings":   {},
	"updatePlexMappings": {},
	"refreshPlexSection": {},

	// User management
	"listUsers":    {},
	"registerUser": {},
	"deleteUser":   {},
}

// AdminMiddleware returns a StrictMiddlewareFunc that rejects non-admin users
// for operations listed in adminOnlyOps.
func AdminMiddleware(authSvc *auth.Service) StrictMiddlewareFunc {
	return func(f StrictHandlerFunc, operationID string) StrictHandlerFunc {
		if _, adminOnly := adminOnlyOps[operationID]; !adminOnly {
			return f // not an admin-only operation — pass through
		}

		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
			userID, ok := auth.UserIDFromContext(ctx)
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"code":403,"message":"admin access required"}`))
				return nil, nil
			}

			isAdmin, err := authSvc.IsUserAdmin(userID)
			if err != nil || !isAdmin {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"code":403,"message":"admin access required"}`))
				return nil, nil
			}

			return f(ctx, w, r, request)
		}
	}
}
