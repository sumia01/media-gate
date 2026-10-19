CREATE TABLE release_keywords (
  user_id integer NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  keyword text COLLATE NOCASE NOT NULL CHECK (length(keyword) BETWEEN 1 AND 256),
  PRIMARY KEY (user_id, keyword)
);
