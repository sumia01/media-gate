-- Completion time cannot establish which item/settings version a check used.
-- Existing snapshots intentionally retain unknown (NULL) input freshness.
ALTER TABLE monitor_decisions ADD COLUMN input_updated_at DATETIME;
