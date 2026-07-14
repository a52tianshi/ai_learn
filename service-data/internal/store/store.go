// Package store is the MySQL persistence layer for the word service.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"worddata/internal/model"
	"worddata/internal/sm2"
)

// masteredInterval: once the next interval reaches this many days the card is
// considered "mastered" (status 2).
const masteredInterval = 21

// Store wraps a *sql.DB.
type Store struct{ db *sql.DB }

// New returns a Store.
func New(db *sql.DB) *Store { return &Store{db: db} }

// GetWordByText returns the cached word (with senses) or (nil, nil) if absent.
func (s *Store) GetWordByText(ctx context.Context, text string) (*model.Word, error) {
	w := &model.Word{}
	var phonetic, audio sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, text, phonetic, audio_url FROM words WHERE text=?`, text).
		Scan(&w.ID, &w.Text, &phonetic, &audio)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	w.Phonetic, w.AudioURL = phonetic.String, audio.String
	senses, err := s.loadSenses(ctx, w.ID)
	if err != nil {
		return nil, err
	}
	w.Senses = senses
	return w, nil
}

func (s *Store) loadSenses(ctx context.Context, wordID int64) ([]model.Sense, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT pos, meaning_en, meaning_cn, examples, synonyms, antonyms
		 FROM word_senses WHERE word_id=? ORDER BY id`, wordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Sense
	for rows.Next() {
		var sense model.Sense
		var pos, cn sql.NullString
		var ex, syn, ant []byte
		if err := rows.Scan(&pos, &sense.MeaningEN, &cn, &ex, &syn, &ant); err != nil {
			return nil, err
		}
		sense.POS, sense.MeaningCN = pos.String, cn.String
		sense.Examples = unmarshalList(ex)
		sense.Synonyms = unmarshalList(syn)
		sense.Antonyms = unmarshalList(ant)
		out = append(out, sense)
	}
	return out, rows.Err()
}

func truncateRune(s string, limit int) string {
	runes := []rune(s)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return s
}

// SaveWord inserts a word and its senses in one transaction, returning the
// word with IDs filled in.
func (s *Store) SaveWord(ctx context.Context, w *model.Word) (*model.Word, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	text := truncateRune(w.Text, 128)
	phonetic := truncateRune(w.Phonetic, 64)
	audioURL := truncateRune(w.AudioURL, 255)

	res, err := tx.ExecContext(ctx,
		`INSERT INTO words (text, phonetic, audio_url) VALUES (?, ?, ?)`,
		text, nullStr(phonetic), nullStr(audioURL))
	if err != nil {
		return nil, err
	}
	w.ID, err = res.LastInsertId()
	if err != nil {
		return nil, err
	}

	for _, sense := range w.Senses {
		pos := truncateRune(sense.POS, 32)
		meaningEN := truncateRune(sense.MeaningEN, 1024)
		meaningCN := truncateRune(sense.MeaningCN, 512)

		if meaningEN == "" {
			meaningEN = meaningCN
		}
		if meaningEN == "" {
			meaningEN = "No definition available"
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO word_senses (word_id, pos, meaning_en, meaning_cn, examples, synonyms, antonyms)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			w.ID, nullStr(pos), meaningEN, nullStr(meaningCN),
			marshalList(sense.Examples), marshalList(sense.Synonyms), marshalList(sense.Antonyms),
		); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return w, nil
}

// AddToNotebook adds word to the user's notebook (idempotent). New cards are
// due immediately so they surface in the next review.
func (s *Store) AddToNotebook(ctx context.Context, tgUserID, wordID int64) (*model.UserWord, error) {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_words (tg_user_id, word_id, due_at)
		 VALUES (?, ?, NOW())
		 ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`,
		tgUserID, wordID)
	if err != nil {
		return nil, err
	}
	return s.getUserWord(ctx, tgUserID, wordID)
}

func (s *Store) getUserWord(ctx context.Context, tgUserID, wordID int64) (*model.UserWord, error) {
	uw := &model.UserWord{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, tg_user_id, word_id, ease_factor, interval_days, repetitions, due_at, status
		 FROM user_words WHERE tg_user_id=? AND word_id=?`, tgUserID, wordID).
		Scan(&uw.ID, &uw.TgUserID, &uw.WordID, &uw.EaseFactor, &uw.IntervalDays,
			&uw.Repetitions, &uw.DueAt, &uw.Status)
	if err != nil {
		return nil, err
	}
	return uw, nil
}

// GetDueWords returns up to limit cards that are due now, oldest first.
func (s *Store) GetDueWords(ctx context.Context, tgUserID int64, limit int) ([]model.DueCard, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT uw.id, uw.word_id, w.text, w.phonetic
		 FROM user_words uw JOIN words w ON w.id = uw.word_id
		 WHERE uw.tg_user_id=? AND uw.due_at<=NOW() AND uw.status <> 3
		 ORDER BY uw.due_at ASC LIMIT ?`, tgUserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cards []model.DueCard
	for rows.Next() {
		var c model.DueCard
		var phonetic sql.NullString
		if err := rows.Scan(&c.UserWordID, &c.WordID, &c.Text, &phonetic); err != nil {
			return nil, err
		}
		c.Phonetic = phonetic.String
		cards = append(cards, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// attach senses (N+1 is fine at personal scale / small limit)
	for i := range cards {
		senses, err := s.loadSenses(ctx, cards[i].WordID)
		if err != nil {
			return nil, err
		}
		cards[i].Senses = senses
	}
	return cards, nil
}

// SubmitReview applies an SM-2 grade to a card inside a transaction, writes a
// review log, and returns the updated card state.
func (s *Store) SubmitReview(ctx context.Context, userWordID int64, quality int) (*model.UserWord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var cur sm2.State
	var tgUserID, wordID int64
	err = tx.QueryRowContext(ctx,
		`SELECT tg_user_id, word_id, ease_factor, interval_days, repetitions
		 FROM user_words WHERE id=? FOR UPDATE`, userWordID).
		Scan(&tgUserID, &wordID, &cur.EaseFactor, &cur.Interval, &cur.Repetitions)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user_word %d not found", userWordID)
	}
	if err != nil {
		return nil, err
	}

	var easeFactor float64
	var intervalDays int
	var repetitions int
	var status int

	if quality == 99 {
		// "完全记住" (Too simple / archived)
		easeFactor = cur.EaseFactor
		intervalDays = 36500 // 100 years
		repetitions = cur.Repetitions + 1
		status = 3 // 搁置 / archived

		if _, err := tx.ExecContext(ctx,
			`UPDATE user_words
			 SET ease_factor=?, interval_days=?, repetitions=?,
			     due_at=DATE_ADD(NOW(), INTERVAL 36500 DAY), last_review_at=NOW(), status=?
			 WHERE id=?`,
			easeFactor, intervalDays, repetitions, status, userWordID,
		); err != nil {
			return nil, err
		}
	} else {
		next := sm2.Next(cur, quality)
		easeFactor = next.EaseFactor
		intervalDays = next.Interval
		repetitions = next.Repetitions
		status = 1
		if next.Interval >= masteredInterval {
			status = 2
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE user_words
			 SET ease_factor=?, interval_days=?, repetitions=?,
			     due_at=DATE_ADD(NOW(), INTERVAL ? DAY), last_review_at=NOW(), status=?
			 WHERE id=?`,
			easeFactor, intervalDays, repetitions, intervalDays, status, userWordID,
		); err != nil {
			return nil, err
		}
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO review_logs
		   (user_word_id, quality, prev_interval, next_interval, prev_ef, next_ef)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		userWordID, quality, cur.Interval, intervalDays, cur.EaseFactor, easeFactor,
	); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.getUserWord(ctx, tgUserID, wordID)
}

// GetRecentWords returns the user's most recently added words (text only),
// used as material for reading generation.
func (s *Store) GetRecentWords(ctx context.Context, tgUserID int64, n int) ([]model.Word, error) {
	if n <= 0 {
		n = 10
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT w.id, w.text, w.phonetic
		 FROM user_words uw JOIN words w ON w.id = uw.word_id
		 WHERE uw.tg_user_id=?
		 ORDER BY uw.created_at DESC LIMIT ?`, tgUserID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Word
	for rows.Next() {
		var w model.Word
		var phonetic sql.NullString
		if err := rows.Scan(&w.ID, &w.Text, &phonetic); err != nil {
			return nil, err
		}
		w.Phonetic = phonetic.String
		out = append(out, w)
	}
	return out, rows.Err()
}

// SaveReading stores an LLM-generated passage.
func (s *Store) SaveReading(ctx context.Context, r *model.Reading) (*model.Reading, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO readings (tg_user_id, content, target_words, model)
		 VALUES (?, ?, ?, ?)`,
		r.TgUserID, r.Content, marshalList(r.TargetWords), nullStr(r.Model))
	if err != nil {
		return nil, err
	}
	r.ID, err = res.LastInsertId()
	return r, err
}

// Stats returns the learning summary for a user.
func (s *Store) Stats(ctx context.Context, tgUserID int64) (*model.Stats, error) {
	st := &model.Stats{}
	if err := s.db.QueryRowContext(ctx,
		`SELECT
		   COUNT(*),
		   COALESCE(SUM(due_at<=NOW()), 0),
		   COALESCE(SUM(status=2), 0)
		 FROM user_words WHERE tg_user_id=?`, tgUserID).
		Scan(&st.TotalWords, &st.DueToday, &st.Mastered); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM review_logs rl
		 JOIN user_words uw ON uw.id = rl.user_word_id
		 WHERE uw.tg_user_id=?`, tgUserID).Scan(&st.ReviewsTotal); err != nil {
		return nil, err
	}
	return st, nil
}

// RefreshWord updates the word's phonetic/audio and replaces all its senses.
func (s *Store) RefreshWord(ctx context.Context, w *model.Word) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	phonetic := truncateRune(w.Phonetic, 64)
	audioURL := truncateRune(w.AudioURL, 255)

	// Update word table info
	_, err = tx.ExecContext(ctx,
		`UPDATE words SET phonetic=?, audio_url=? WHERE id=?`,
		nullStr(phonetic), nullStr(audioURL), w.ID)
	if err != nil {
		return err
	}

	// Delete old senses
	_, err = tx.ExecContext(ctx, `DELETE FROM word_senses WHERE word_id=?`, w.ID)
	if err != nil {
		return err
	}

	// Insert new senses
	for _, sense := range w.Senses {
		pos := truncateRune(sense.POS, 32)
		meaningEN := truncateRune(sense.MeaningEN, 1024)
		meaningCN := truncateRune(sense.MeaningCN, 512)

		if meaningEN == "" {
			meaningEN = meaningCN
		}
		if meaningEN == "" {
			meaningEN = "No definition available"
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO word_senses (word_id, pos, meaning_en, meaning_cn, examples, synonyms, antonyms)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			w.ID, nullStr(pos), meaningEN, nullStr(meaningCN),
			marshalList(sense.Examples), marshalList(sense.Synonyms), marshalList(sense.Antonyms),
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// --- JSON helpers ---

func marshalList(v []string) any {
	if len(v) == 0 {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

func unmarshalList(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	var out []string
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
