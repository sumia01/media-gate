CREATE TABLE IF NOT EXISTS monitor_decisions (
    media_item_id INTEGER PRIMARY KEY NOT NULL,
    checked_at DATETIME NOT NULL,
    outcome TEXT NOT NULL,
    summary TEXT NOT NULL,
    details TEXT NOT NULL DEFAULT '[]',
    truncated NUMERIC NOT NULL DEFAULT 0,
    CONSTRAINT fk_monitor_decisions_media_item FOREIGN KEY (media_item_id)
        REFERENCES media_items(id) ON DELETE CASCADE
);
