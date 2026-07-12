package sqlite

import (
	"database/sql"
	"log/slog"
)

// cleanupOrphans prunes rows whose parent no longer exists. Most tables enforce
// this with ON DELETE CASCADE / SET NULL foreign keys, but episode_monitors and
// subtitles historically carry no FK (glebarez AutoMigrate never emitted one),
// so this boot-time sweep is their cascade mechanism. It is cheap and defensive
// for the FK-backed tables too. Runs on every New().
func cleanupOrphans(db *sql.DB) {
	orphanQueries := []struct {
		label string
		query string
	}{
		{"media_metadata", `DELETE FROM media_metadata WHERE media_item_id NOT IN (SELECT id FROM media_items)`},
		{"media_files", `DELETE FROM media_files WHERE media_item_id NOT IN (SELECT id FROM media_items)`},
		{"season_monitors", `DELETE FROM season_monitors WHERE media_item_id NOT IN (SELECT id FROM media_items)`},
		{"episode_monitors", `DELETE FROM episode_monitors WHERE media_item_id NOT IN (SELECT id FROM media_items)`},
		{"episodes", `DELETE FROM episodes WHERE media_item_id NOT IN (SELECT id FROM media_items)`},
		{"downloads", `DELETE FROM downloads WHERE media_item_id NOT IN (SELECT id FROM media_items)`},
		{"downloads.episode_id", `UPDATE downloads SET episode_id = NULL WHERE episode_id IS NOT NULL AND episode_id NOT IN (SELECT id FROM episodes)`},
		{"download_blocklists", `DELETE FROM download_blocklists WHERE media_item_id NOT IN (SELECT id FROM media_items)`},
		{"subtitles", `DELETE FROM subtitles WHERE media_item_id NOT IN (SELECT id FROM media_items)`},
		{"subtitles.media_file_id", `UPDATE subtitles SET media_file_id = NULL WHERE media_file_id IS NOT NULL AND media_file_id NOT IN (SELECT id FROM media_files)`},
		{"media_items", `DELETE FROM media_items WHERE library_id NOT IN (SELECT id FROM libraries)`},
		{"refresh_tokens", `DELETE FROM refresh_tokens WHERE user_id NOT IN (SELECT id FROM users)`},
	}
	for _, oq := range orphanQueries {
		res, err := db.Exec(oq.query)
		if err != nil {
			slog.Warn("orphan cleanup failed", "table", oq.label, "error", err)
			continue
		}
		if n, _ := res.RowsAffected(); n > 0 {
			slog.Info("cleaned up orphan records", "table", oq.label, "deleted", n)
		}
	}
}
