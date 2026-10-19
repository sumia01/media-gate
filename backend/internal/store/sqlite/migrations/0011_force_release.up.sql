ALTER TABLE media_items ADD COLUMN force_release numeric NOT NULL DEFAULT 1 CHECK (force_release IN (0, 1));
