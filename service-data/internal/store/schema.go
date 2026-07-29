package store

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed schema.sqlite.sql
var schemaSQL string

// Migrate creates all tables/indexes if absent. Safe to call on every startup
// (every statement is IF NOT EXISTS).
func (s *Store) Migrate(ctx context.Context) error {
	for _, stmt := range splitStatements(schemaSQL) {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("schema statement failed: %w\n%s", err, stmt)
		}
	}
	return nil
}

// splitStatements splits a SQL script on ';' into individual, non-empty
// statements. SQLite ignores leading `--` comment lines, so we keep them.
func splitStatements(script string) []string {
	var out []string
	for _, part := range strings.Split(script, ";") {
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return out
}
