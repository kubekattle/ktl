// context.go contains helpers for managing writer lifecycles and context cancellation.
package sqlitewriter

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type ContextSnapshot struct {
	Events      []EventRow
	Deployments []DeploymentRow
	ConfigMaps  []ConfigMapRow
}

type EventRow struct {
	Namespace         string
	Name              string
	Type              string
	Reason            string
	Message           string
	InvolvedKind      string
	InvolvedName      string
	InvolvedNamespace string
	Count             int
	FirstTimestamp    time.Time
	LastTimestamp     time.Time
}

type DeploymentRow struct {
	Namespace         string
	Name              string
	Replicas          int
	UpdatedReplicas   int
	ReadyReplicas     int
	AvailableReplicas int
	Strategy          string
	Selector          string
	Labels            string
	Annotations       string
	Conditions        string
	CreatedAt         time.Time
}

type ConfigMapRow struct {
	Namespace       string
	Name            string
	Data            string
	BinaryData      string
	Labels          string
	Annotations     string
	ResourceVersion string
	CreatedAt       time.Time
}

const (
	createEventsTableStmt = `
CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    namespace TEXT,
    name TEXT,
    type TEXT,
    reason TEXT,
    message TEXT,
    involved_kind TEXT,
    involved_name TEXT,
    involved_namespace TEXT,
    count INTEGER,
    first_timestamp TEXT,
    last_timestamp TEXT
);`
	createDeploymentsTableStmt = `
CREATE TABLE IF NOT EXISTS deployments (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    namespace TEXT,
    name TEXT,
    replicas INTEGER,
    updated_replicas INTEGER,
    ready_replicas INTEGER,
    available_replicas INTEGER,
    strategy TEXT,
    selector TEXT,
    labels TEXT,
    annotations TEXT,
    conditions TEXT,
    created_at TEXT
);`
	createConfigMapsTableStmt = `
CREATE TABLE IF NOT EXISTS configmaps (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    namespace TEXT,
    name TEXT,
    data TEXT,
    binary_data TEXT,
    labels TEXT,
    annotations TEXT,
    resource_version TEXT,
    created_at TEXT
);`
)

func ensureContextTables(ctx context.Context, db *sql.DB) error {
	stmts := []string{createEventsTableStmt, createDeploymentsTableStmt, createConfigMapsTableStmt}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func writeContext(ctx context.Context, db *sql.DB, snapshot ContextSnapshot) error {
	if _, err := db.ExecContext(ctx, "DELETE FROM events"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM deployments"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM configmaps"); err != nil {
		return err
	}
	if err := insertEvents(ctx, db, snapshot.Events); err != nil {
		return err
	}
	if err := insertDeployments(ctx, db, snapshot.Deployments); err != nil {
		return err
	}
	return insertConfigMaps(ctx, db, snapshot.ConfigMaps)
}

func insertEvents(ctx context.Context, db *sql.DB, rows []EventRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := db.PrepareContext(ctx, `INSERT INTO events(namespace,name,type,reason,message,involved_kind,involved_name,involved_namespace,count,first_timestamp,last_timestamp) VALUES(?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if _, err := stmt.ExecContext(ctx,
			row.Namespace,
			row.Name,
			row.Type,
			row.Reason,
			row.Message,
			row.InvolvedKind,
			row.InvolvedName,
			row.InvolvedNamespace,
			row.Count,
			row.FirstTimestamp.UTC().Format(time.RFC3339Nano),
			row.LastTimestamp.UTC().Format(time.RFC3339Nano),
		); err != nil {
			return err
		}
	}
	return nil
}

func insertDeployments(ctx context.Context, db *sql.DB, rows []DeploymentRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := db.PrepareContext(ctx, `INSERT INTO deployments(namespace,name,replicas,updated_replicas,ready_replicas,available_replicas,strategy,selector,labels,annotations,conditions,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if _, err := stmt.ExecContext(ctx,
			row.Namespace,
			row.Name,
			row.Replicas,
			row.UpdatedReplicas,
			row.ReadyReplicas,
			row.AvailableReplicas,
			row.Strategy,
			row.Selector,
			row.Labels,
			row.Annotations,
			row.Conditions,
			row.CreatedAt.UTC().Format(time.RFC3339Nano),
		); err != nil {
			return err
		}
	}
	return nil
}

func insertConfigMaps(ctx context.Context, db *sql.DB, rows []ConfigMapRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := db.PrepareContext(ctx, `INSERT INTO configmaps(namespace,name,data,binary_data,labels,annotations,resource_version,created_at) VALUES(?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if _, err := stmt.ExecContext(ctx,
			row.Namespace,
			row.Name,
			row.Data,
			row.BinaryData,
			row.Labels,
			row.Annotations,
			row.ResourceVersion,
			row.CreatedAt.UTC().Format(time.RFC3339Nano),
		); err != nil {
			return err
		}
	}
	return nil
}

// WriteContext opens the SQLite database at path and replaces context tables with the snapshot contents.
func WriteContext(path string, snapshot ContextSnapshot) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("sqlite path cannot be empty")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ensureContextTables(ctx, db); err != nil {
		return err
	}
	return writeContext(ctx, db, snapshot)
}
