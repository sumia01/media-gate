-- Exact time qBittorrent recorded that a download's files became complete.
-- Existing rows remain NULL because this time cannot be reconstructed reliably.
ALTER TABLE "downloads" ADD COLUMN "downloaded_at" datetime;
