// Command migrate is a one-off tool that copies all wordbot data from the
// existing MySQL/MariaDB into a fresh SQLite file, preserving primary keys so
// foreign-key references stay intact.
//
// Usage:
//
//	SRC_MYSQL_DSN='penny:pass@tcp(127.0.0.1:3306)/wordbot?parseTime=false&charset=utf8mb4' \
//	SQLITE_PATH=./wordbot.db \
//	go run ./cmd/migrate
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"

	"worddata/internal/store"
)

// Copied in FK-safe order; foreign_keys is off on the destination during the
// load anyway, so partial inconsistencies won't block the copy.
var tables = []string{"words", "word_senses", "user_words", "review_logs", "readings"}

func main() {
	src := env("SRC_MYSQL_DSN", "penny:wordbot123@tcp(127.0.0.1:3306)/wordbot?parseTime=false&charset=utf8mb4")
	dstPath := env("SQLITE_PATH", "./wordbot.db")
	ctx := context.Background()

	srcDB, err := sql.Open("mysql", src)
	if err != nil {
		log.Fatalf("open mysql: %v", err)
	}
	defer srcDB.Close()
	if err := srcDB.Ping(); err != nil {
		log.Fatalf("ping mysql (source): %v", err)
	}

	dstDB, err := sql.Open("sqlite", "file:"+dstPath+"?_pragma=foreign_keys(0)")
	if err != nil {
		log.Fatalf("open sqlite: %v", err)
	}
	defer dstDB.Close()

	if err := store.New(dstDB).Migrate(ctx); err != nil {
		log.Fatalf("create sqlite schema: %v", err)
	}

	for _, t := range tables {
		n, err := copyTable(ctx, srcDB, dstDB, t)
		if err != nil {
			log.Fatalf("copy %s: %v", t, err)
		}
		log.Printf("copied %-12s %d rows", t, n)
	}
	log.Printf("done -> %s", dstPath)
}

// copyTable copies every row of table from src to dst. It reads all columns
// generically (as nullable strings) so it needs no per-table struct; SQLite's
// type affinity converts the text back into INTEGER/REAL where appropriate.
func copyTable(ctx context.Context, src, dst *sql.DB, table string) (int, error) {
	rows, err := src.QueryContext(ctx, "SELECT * FROM "+table)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(cols)), ",")
	insert := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		table, strings.Join(cols, ","), placeholders)

	tx, err := dst.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	count := 0
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return count, err
		}
		args := make([]any, len(cols))
		for i, v := range vals {
			if v.Valid {
				args[i] = v.String
			} else {
				args[i] = nil
			}
		}
		if _, err := tx.ExecContext(ctx, insert, args...); err != nil {
			return count, err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return count, err
	}
	return count, tx.Commit()
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
