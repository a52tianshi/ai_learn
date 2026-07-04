// Package model holds the DTOs exchanged over the REST API and stored in MySQL.
package model

// Sense is one part-of-speech entry for a word.
type Sense struct {
	POS       string   `json:"pos,omitempty"`
	MeaningEN string   `json:"meaning_en"`
	MeaningCN string   `json:"meaning_cn,omitempty"`
	Examples  []string `json:"examples,omitempty"`
	Synonyms  []string `json:"synonyms,omitempty"`
	Antonyms  []string `json:"antonyms,omitempty"`
}

// Word is a dictionary entry with all its senses.
type Word struct {
	ID       int64   `json:"id"`
	Text     string  `json:"text"`
	Phonetic string  `json:"phonetic,omitempty"`
	AudioURL string  `json:"audio_url,omitempty"`
	Senses   []Sense `json:"senses"`
}

// UserWord is a row in a user's notebook plus its SM-2 memory state.
type UserWord struct {
	ID           int64   `json:"id"`
	TgUserID     int64   `json:"tg_user_id"`
	WordID       int64   `json:"word_id"`
	Text         string  `json:"text,omitempty"`
	EaseFactor   float64 `json:"ease_factor"`
	IntervalDays int     `json:"interval_days"`
	Repetitions  int     `json:"repetitions"`
	DueAt        string  `json:"due_at"`
	Status       int     `json:"status"`
}

// DueCard is a single review card returned by GET /reviews/due.
type DueCard struct {
	UserWordID int64  `json:"user_word_id"`
	WordID     int64  `json:"word_id"`
	Text       string `json:"text"`
	Phonetic   string `json:"phonetic,omitempty"`
	Senses     []Sense `json:"senses"`
}

// Reading is one LLM-generated reading passage.
type Reading struct {
	ID          int64    `json:"id"`
	TgUserID    int64    `json:"tg_user_id"`
	Content     string   `json:"content"`
	TargetWords []string `json:"target_words,omitempty"`
	Model       string   `json:"model,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
}

// Stats is the learning summary for a user.
type Stats struct {
	TotalWords   int `json:"total_words"`
	DueToday     int `json:"due_today"`
	Mastered     int `json:"mastered"`
	ReviewsTotal int `json:"reviews_total"`
}
