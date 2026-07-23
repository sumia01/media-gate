-- One-time cleanup for a download-worker race (fixed alongside this migration):
-- sendPending() and pollActive() ran in the same tick, so a torrent just handed
-- to qBittorrent could be polled before qBittorrent finished registering it,
-- spuriously reporting it missing. handleMissingTorrent then marked the row
-- "failed" and cleared its hash, even though the torrent was fine. For
-- monitored series episodes the monitor's next pass re-grabbed the episode
-- into a brand new row that completed and imported normally, leaving the
-- original "failed" row behind as dead weight.
--
-- Only removes a row when there is POSITIVE PROOF it is superseded: another
-- download for the same episode that actually completed and linked into the
-- library. Nothing is removed on fingerprint alone, so a genuinely failed
-- release with no successful replacement is left untouched for manual review.
DELETE FROM "downloads"
WHERE "status" = 'failed'
  AND "client_torrent_hash" = ''
  AND "last_error" = ''
  AND "retry_count" = 0
  AND "linked_to_library" = 0
  AND "episode_id" IS NOT NULL
  AND EXISTS (
    SELECT 1 FROM "downloads" AS "replacement"
    WHERE "replacement"."episode_id" = "downloads"."episode_id"
      AND "replacement"."status" = 'completed'
      AND "replacement"."linked_to_library" = 1
      AND "replacement"."id" != "downloads"."id"
  );
