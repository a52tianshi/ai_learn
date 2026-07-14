package store

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

func TestQuality99ArchivesWord(t *testing.T) {
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		dsn = "root@tcp(127.0.0.1:3306)/wordbot?parseTime=false&charset=utf8mb4"
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Skip("skipping test; database not running or connection failed")
	}

	// 1. Create a dummy word
	res, err := db.Exec(`INSERT INTO words (text, phonetic) VALUES (?, ?)`, "testwordut", "/test/")
	if err != nil {
		t.Fatalf("failed to insert word: %v", err)
	}
	wordID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	// 2. Create user_word for a mock user (tg_user_id = 999999)
	var tgUserID int64 = 999999
	res, err = db.Exec(`INSERT INTO user_words (tg_user_id, word_id, due_at, status) VALUES (?, ?, NOW(), 1)`, tgUserID, wordID)
	if err != nil {
		t.Fatalf("failed to insert user_word: %v", err)
	}
	userWordID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id for user_word: %v", err)
	}

	// Setup cleanup
	defer func() {
		_, _ = db.Exec(`DELETE FROM review_logs WHERE user_word_id = ?`, userWordID)
		_, _ = db.Exec(`DELETE FROM user_words WHERE id = ?`, userWordID)
		_, _ = db.Exec(`DELETE FROM words WHERE id = ?`, wordID)
	}()

	s := New(db)
	ctx := context.Background()

	// 3. Call SubmitReview with quality 99
	uw, err := s.SubmitReview(ctx, userWordID, 99)
	if err != nil {
		t.Fatalf("failed to submit review: %v", err)
	}

	// 4. Assertions
	if uw.Status != 3 {
		t.Errorf("expected status 3 (archived), got %d", uw.Status)
	}
	if uw.IntervalDays != 36500 {
		t.Errorf("expected interval_days 36500, got %d", uw.IntervalDays)
	}

	// 5. Test GetDueWords excludes status 3
	cards, err := s.GetDueWords(ctx, tgUserID, 20)
	if err != nil {
		t.Fatalf("failed to get due words: %v", err)
	}

	for _, card := range cards {
		if card.UserWordID == userWordID {
			t.Errorf("expected archived word to be excluded from due words list, but it was found")
		}
	}
}
