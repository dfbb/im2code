package history

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// History records every user input to a SQLite database.
type History struct {
	db *sql.DB
}

// New opens (or creates) the SQLite database at dbPath and ensures the
// cmd_history table exists.
func New(dbPath string) (*History, error) {
	dsn := "file:" + dbPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("history: open db: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS cmd_history (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		ts        TEXT    NOT NULL,
		channel   TEXT    NOT NULL,
		sender_id TEXT    NOT NULL,
		text      TEXT    NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("history: create table: %w", err)
	}
	return &History{db: db}, nil
}

// Record inserts one row. It is safe to call concurrently.
func (h *History) Record(channel, senderID, text string) error {
	ts := time.Now().UTC().Format(time.RFC3339)
	_, err := h.db.Exec(
		`INSERT INTO cmd_history (ts, channel, sender_id, text) VALUES (?, ?, ?, ?)`,
		ts, channel, senderID, text,
	)
	return err
}

// Close closes the underlying database connection.
func (h *History) Close() error {
	return h.db.Close()
}
