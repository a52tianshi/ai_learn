-- 单词本记忆助手 · 数据层建表脚本
-- 见 TD/01-总览.md 第 3 节。无 users 表:tg_user_id 直接下沉到业务表。
-- 用法: mysql -u root -p wordbot < migrations/schema.sql
--   或先 CREATE DATABASE wordbot CHARACTER SET utf8mb4;

SET NAMES utf8mb4;

-- 单词词条(全局去重)
CREATE TABLE IF NOT EXISTS words (
  id         BIGINT       NOT NULL AUTO_INCREMENT,
  text       VARCHAR(128) NOT NULL COMMENT '单词/短语原文(小写归一)',
  phonetic   VARCHAR(64)           DEFAULT NULL COMMENT '音标',
  audio_url  VARCHAR(255)          DEFAULT NULL COMMENT '发音音频链接',
  created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_text (text)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 释义/例句缓存(DictionaryAPI.dev 结果落库),一个词性一行
CREATE TABLE IF NOT EXISTS word_senses (
  id          BIGINT        NOT NULL AUTO_INCREMENT,
  word_id     BIGINT        NOT NULL,
  pos         VARCHAR(32)            DEFAULT NULL COMMENT '词性 partOfSpeech',
  meaning_en  VARCHAR(1024) NOT NULL COMMENT '英文释义 definition',
  meaning_cn  VARCHAR(512)           DEFAULT NULL COMMENT '中文释义(纯英英,保留列位不使用)',
  examples    JSON                   DEFAULT NULL COMMENT '例句数组',
  synonyms    JSON                   DEFAULT NULL COMMENT '同义词',
  antonyms    JSON                   DEFAULT NULL COMMENT '反义词',
  source      VARCHAR(32)   NOT NULL DEFAULT 'dictionaryapi' COMMENT 'dictionaryapi/manual',
  created_at  DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_word (word_id),
  CONSTRAINT fk_sense_word FOREIGN KEY (word_id) REFERENCES words(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 用户单词本 + SM-2 记忆状态(核心表)
CREATE TABLE IF NOT EXISTS user_words (
  id            BIGINT       NOT NULL AUTO_INCREMENT,
  tg_user_id    BIGINT       NOT NULL COMMENT 'Telegram 用户 ID(owner,无 users 表)',
  word_id       BIGINT       NOT NULL,
  ease_factor   DECIMAL(4,2) NOT NULL DEFAULT 2.50 COMMENT 'SM-2 EF,最小 1.30',
  interval_days INT          NOT NULL DEFAULT 0    COMMENT '当前间隔(天)',
  repetitions   INT          NOT NULL DEFAULT 0    COMMENT '连续答对次数 n',
  due_at        DATETIME     NOT NULL COMMENT '下次到期复习时间',
  last_review_at DATETIME             DEFAULT NULL,
  status        TINYINT      NOT NULL DEFAULT 0 COMMENT '0=新词 1=学习中 2=已掌握 3=搁置',
  created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_user_word (tg_user_id, word_id),
  KEY idx_due (tg_user_id, due_at),
  CONSTRAINT fk_uw_word FOREIGN KEY (word_id) REFERENCES words(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 复习流水(可追溯记忆曲线)
CREATE TABLE IF NOT EXISTS review_logs (
  id            BIGINT       NOT NULL AUTO_INCREMENT,
  user_word_id  BIGINT       NOT NULL,
  quality       TINYINT      NOT NULL COMMENT '本次自评 0~5',
  prev_interval INT          NOT NULL,
  next_interval INT          NOT NULL,
  prev_ef       DECIMAL(4,2) NOT NULL,
  next_ef       DECIMAL(4,2) NOT NULL,
  reviewed_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_uw (user_word_id, reviewed_at),
  CONSTRAINT fk_log_uw FOREIGN KEY (user_word_id) REFERENCES user_words(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- LLM 生成的阅读短文留档
CREATE TABLE IF NOT EXISTS readings (
  id           BIGINT      NOT NULL AUTO_INCREMENT,
  tg_user_id   BIGINT      NOT NULL COMMENT '为谁生成',
  content      TEXT        NOT NULL COMMENT 'Gemini 生成的短文',
  target_words JSON                DEFAULT NULL COMMENT '文中嵌入的目标词列表',
  model        VARCHAR(64)         DEFAULT NULL COMMENT '生成模型标识',
  vec_id       VARCHAR(64)         DEFAULT NULL COMMENT '后续接 chroma 用,MVP 留空',
  created_at   DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_user (tg_user_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
