CREATE INDEX IF NOT EXISTS idx_episodes_air_date ON episodes(air_date, media_item_id, season_number, episode_number);
