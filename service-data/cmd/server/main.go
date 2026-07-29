// Command server runs the word data service (HTTP/JSON REST over SQLite).
package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"time"

	_ "modernc.org/sqlite"

	"worddata/internal/api"
	"worddata/internal/dict"
	"worddata/internal/store"
)

func main() {
	path := env("SQLITE_PATH", "./wordbot.db")
	addr := env("HTTP_ADDR", ":8080")
	dictBase := env("DICT_API_BASE", "https://api.dictionaryapi.dev/api/v2/entries/en")

	// WAL + busy_timeout keep readers/writers happy; foreign_keys enforces FKs.
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	// SQLite is single-writer; one connection serializes writes and avoids
	// "database is locked" at personal scale.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		log.Fatalf("open sqlite %s: %v", path, err)
	}

	st := store.New(db)
	if err := st.Migrate(context.Background()); err != nil {
		log.Fatalf("migrate schema: %v", err)
	}

	googleAPIKey := env("GOOGLE_API_KEY", "")
	modelName := env("MODEL", "gemini-3.1-flash-lite")
	srv := api.New(st, dict.New(dictBase, googleAPIKey, modelName))
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("word data service listening on %s (db=%s, dict=%s)", addr, path, dictBase)
	if err := httpSrv.ListenAndServe(); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
