package store

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// 使用内存库验证双条件查询，不连接本机 MySQL。
func TestGetUserWordByIDAndUser(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE user_words (
		id INTEGER PRIMARY KEY, tg_user_id INTEGER, word_id INTEGER,
		ease_factor REAL, interval_days INTEGER, repetitions INTEGER,
		due_at TEXT, status INTEGER
	);
	INSERT INTO user_words VALUES (9, 123, 8, 2.5, 6, 2, '2026-11-11', 1),
		(10, 456, 8, 2.5, 6, 2, '2026-11-11', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	st := New(db)
	for _, tc := range []struct {
		name      string
		id, owner int64
		found     bool
	}{
		{"匹配", 9, 123, true},
		{"其他用户", 9, 456, false},
		{"同用户其他卡片", 10, 123, false},
		{"不存在", 99, 123, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card, err := st.GetUserWordByIDAndUser(context.Background(), tc.id, tc.owner)
			if err != nil {
				t.Fatal(err)
			}
			if (card != nil) != tc.found {
				t.Fatalf("card = %+v, found = %v", card, tc.found)
			}
			if card != nil && (card.ID != 9 || card.TgUserID != 123 || card.WordID != 8 || card.EaseFactor != 2.5 || card.IntervalDays != 6 || card.Repetitions != 2 || card.DueAt != "2026-11-11" || card.Status != 1) {
				t.Fatalf("card = %+v", card)
			}
		})
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetUserWordByIDAndUser(context.Background(), 9, 123); err == nil {
		t.Fatal("关闭数据库后应返回查询错误")
	}
}
