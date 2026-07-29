-- 单词本记忆助手 · SQLite 建表(由 store.schemaSQL 内嵌,服务启动时自动执行)
-- 无 users 表:tg_user_id 直接下沉到业务表。JSON 列以 TEXT 存储。

CREATE TABLE IF NOT EXISTS words (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  text       TEXT    NOT NULL,
  phonetic   TEXT,
  audio_url  TEXT,
  created_at TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_text ON words(text);

CREATE TABLE IF NOT EXISTS word_senses (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  word_id    INTEGER NOT NULL REFERENCES words(id),
  pos        TEXT,
  meaning_en TEXT    NOT NULL,
  meaning_cn TEXT,
  examples   TEXT,
  synonyms   TEXT,
  antonyms   TEXT,
  source     TEXT    NOT NULL DEFAULT 'dictionaryapi',
  created_at TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_word ON word_senses(word_id);

CREATE TABLE IF NOT EXISTS user_words (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  tg_user_id     INTEGER NOT NULL,
  word_id        INTEGER NOT NULL REFERENCES words(id),
  ease_factor    REAL    NOT NULL DEFAULT 2.5,
  interval_days  INTEGER NOT NULL DEFAULT 0,
  repetitions    INTEGER NOT NULL DEFAULT 0,
  due_at         TEXT    NOT NULL,
  last_review_at TEXT,
  status         INTEGER NOT NULL DEFAULT 0,
  created_at     TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at     TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_user_word ON user_words(tg_user_id, word_id);
CREATE INDEX IF NOT EXISTS idx_due ON user_words(tg_user_id, due_at);

CREATE TABLE IF NOT EXISTS review_logs (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  user_word_id  INTEGER NOT NULL REFERENCES user_words(id),
  quality       INTEGER NOT NULL,
  prev_interval INTEGER NOT NULL,
  next_interval INTEGER NOT NULL,
  prev_ef       REAL    NOT NULL,
  next_ef       REAL    NOT NULL,
  reviewed_at   TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_uw ON review_logs(user_word_id, reviewed_at);

CREATE TABLE IF NOT EXISTS readings (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  tg_user_id   INTEGER NOT NULL,
  content      TEXT    NOT NULL,
  target_words TEXT,
  model        TEXT,
  vec_id       TEXT,
  created_at   TEXT    NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_reading_user ON readings(tg_user_id, created_at);
