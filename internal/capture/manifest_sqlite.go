// File: internal/capture/manifest_sqlite.go
// Brief: Internal capture package implementation for 'manifest sqlite'.

// Package capture provides capture helpers.

package capture

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type manifestIndex struct {
	GeneratedAt time.Time `json:"generatedAt"`
	Resources   []struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Namespace  string `json:"namespace,omitempty"`
		Name       string `json:"name"`
		Owners     []struct {
			Kind      string `json:"kind"`
			Name      string `json:"name"`
			Namespace string `json:"namespace,omitempty"`
			UID       string `json:"uid,omitempty"`
		} `json:"owners,omitempty"`
		Path string `json:"path"`
	} `json:"resources"`
}

type manifestKey struct {
	Kind      string
	Namespace string
	Name      string
}

// IngestManifestsToSQLite loads manifests/index.json + YAML files from the capture
// directory and stores them in manifest_resources/manifest_edges for fast viewer access.
func IngestManifestsToSQLite(ctx context.Context, db *sql.DB, captureDir string) error {
	if db == nil {
		return fmt.Errorf("sqlite db is required")
	}
	base := strings.TrimSpace(captureDir)
	if base == "" {
		return fmt.Errorf("capture dir is required")
	}
	indexPath := filepath.Join(base, "manifests", "index.json")
	indexBytes, err := os.ReadFile(indexPath)
	if err != nil {
		// Manifests are optional in captures.
		return nil
	}
	var index manifestIndex
	if err := json.Unmarshal(indexBytes, &index); err != nil {
		return nil
	}
	if len(index.Resources) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM manifest_edges`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM manifest_resources`); err != nil {
		return err
	}

	insertRes, err := tx.PrepareContext(ctx, `INSERT INTO manifest_resources(api_version, kind, namespace, name, yaml, path, uid) VALUES(?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer insertRes.Close()

	selectID, err := tx.PrepareContext(ctx, `SELECT id FROM manifest_resources WHERE kind = ? AND COALESCE(namespace, '') = ? AND name = ? LIMIT 1`)
	if err != nil {
		return err
	}
	defer selectID.Close()

	ids := make(map[manifestKey]int64, len(index.Resources))
	owners := make(map[manifestKey][]manifestKey, len(index.Resources))

	for _, r := range index.Resources {
		kind := strings.TrimSpace(r.Kind)
		name := strings.TrimSpace(r.Name)
		ns := strings.TrimSpace(r.Namespace)
		if kind == "" || name == "" || r.Path == "" {
			continue
		}
		yamlPath := filepath.Join(base, "manifests", filepath.FromSlash(r.Path))
		yamlBytes, err := os.ReadFile(yamlPath)
		if err != nil {
			continue
		}
		if _, err := insertRes.ExecContext(ctx, strings.TrimSpace(r.APIVersion), kind, ns, name, string(yamlBytes), r.Path, ""); err != nil {
			return err
		}
		var id int64
		if err := selectID.QueryRowContext(ctx, kind, ns, name).Scan(&id); err != nil {
			return err
		}
		key := manifestKey{Kind: kind, Namespace: ns, Name: name}
		ids[key] = id
		if len(r.Owners) > 0 {
			var refs []manifestKey
			for _, o := range r.Owners {
				ok := strings.TrimSpace(o.Kind)
				on := strings.TrimSpace(o.Name)
				if ok == "" || on == "" {
					continue
				}
				refs = append(refs, manifestKey{
					Kind:      ok,
					Namespace: strings.TrimSpace(o.Namespace),
					Name:      on,
				})
			}
			if len(refs) > 0 {
				owners[key] = refs
			}
		}
	}

	insertEdge, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO manifest_edges(parent_id, child_id) VALUES(?, ?)`)
	if err != nil {
		return err
	}
	defer insertEdge.Close()

	for child, refs := range owners {
		childID := ids[child]
		if childID == 0 {
			continue
		}
		for _, ref := range refs {
			parentID := ids[ref]
			if parentID == 0 {
				continue
			}
			if _, err := insertEdge.ExecContext(ctx, parentID, childID); err != nil {
				return err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}
