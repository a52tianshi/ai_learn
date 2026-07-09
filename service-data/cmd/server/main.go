// Command server runs the word data service (HTTP/JSON REST over MySQL).
package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"worddata/internal/api"
	"worddata/internal/dict"
	"worddata/internal/store"
)

func main() {
	dsn := env("MYSQL_DSN", "root@tcp(127.0.0.1:3306)/wordbot?parseTime=false&charset=utf8mb4")
	addr := env("HTTP_ADDR", ":8080")
	dictBase := env("DICT_API_BASE", "https://api.dictionaryapi.dev/api/v2/entries/en")

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("open mysql: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(10)
	db.SetConnMaxLifetime(3 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatalf("ping mysql (check MYSQL_DSN and that MySQL is running): %v", err)
	}

	googleAPIKey := env("GOOGLE_API_KEY", "")
	modelName := env("MODEL", "gemini-2.5-flash")
	srv := api.New(store.New(db), dict.New(dictBase, googleAPIKey, modelName))
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("word data service listening on %s (dict=%s)", addr, dictBase)
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
