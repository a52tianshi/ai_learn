package main

import (
	"context"
	"database/sql"
	"flag"
	"log"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"worddata/internal/dict"
	"worddata/internal/store"
)

func main() {
	force := flag.Bool("force", false, "force refresh all words, even those with Chinese explanations")
	flag.Parse()

	dsn := env("MYSQL_DSN", "root@tcp(127.0.0.1:3306)/wordbot?parseTime=false&charset=utf8mb4")
	apiKey := env("GOOGLE_API_KEY", "")
	modelName := env("MODEL", "gemini-2.5-flash")

	if apiKey == "" {
		log.Fatalf("GOOGLE_API_KEY is not set. Cannot run refresh script.")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("open mysql: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("ping mysql: %v", err)
	}

	st := store.New(db)
	dc := dict.New("", apiKey, modelName) // base is empty since we query Gemini

	ctx := context.Background()

	// Query words to refresh
	var query string
	if *force {
		query = `SELECT id, text FROM words`
	} else {
		// Select words that have no senses with Chinese meaning
		query = `
			SELECT DISTINCT w.id, w.text 
			FROM words w
			WHERE w.id NOT IN (
				SELECT DISTINCT word_id 
				FROM word_senses 
				WHERE meaning_cn IS NOT NULL AND meaning_cn <> ''
			)
		`
	}

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		log.Fatalf("query words: %v", err)
	}
	defer rows.Close()

	type wordRef struct {
		id   int64
		text string
	}
	var targets []wordRef
	for rows.Next() {
		var w wordRef
		if err := rows.Scan(&w.id, &w.text); err != nil {
			log.Fatalf("scan word: %v", err)
		}
		targets = append(targets, w)
	}

	log.Printf("Found %d words needing refresh (force=%t)", len(targets), *force)

	for i, t := range targets {
		log.Printf("[%d/%d] Fetching definition for: %s ...", i+1, len(targets), t.text)

		fetched, err := dc.Fetch(ctx, t.text)
		if err != nil {
			log.Printf("  ❌ Failed to fetch %s: %v", t.text, err)
			time.Sleep(1 * time.Second)
			continue
		}

		fetched.ID = t.id
		err = st.RefreshWord(ctx, fetched)
		if err != nil {
			log.Printf("  ❌ Failed to save %s to DB: %v", t.text, err)
		} else {
			log.Printf("  ✅ Refreshed: %s (%s)", t.text, fetched.Phonetic)
		}

		// Rate limit spacing (1.2 seconds between requests)
		time.Sleep(1200 * time.Millisecond)
	}

	log.Println("Refresh task complete!")
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
