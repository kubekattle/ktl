package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type sqliteStore struct {
	db   *sql.DB
	path string
}

func openSQLiteStore(path string, readOnly bool) (*sqliteStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("capture db path is required")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("open capture db: %w", err)
	}
	dsn := path
	if readOnly {
		// modernc.org/sqlite understands URI parameters in a "file:" DSN.
		u := url.URL{Scheme: "file", Path: path}
		q := u.Query()
		q.Set("mode", "ro")
		q.Set("_busy_timeout", "5000")
		u.RawQuery = q.Encode()
		dsn = u.String()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return &sqliteStore{db: db, path: path}, nil
}

func (s *sqliteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

