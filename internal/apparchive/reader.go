// reader.go opens and iterates ktl .k8s archives for downstream commands.
package apparchive

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Reader provides read-only access to an app archive.
type Reader struct {
	db *sql.DB
}

// NewReader opens the archive located at path.
func NewReader(path string) (*Reader, error) {
	db, err := openDatabase(path)
	if err != nil {
		return nil, err
	}
	return &Reader{db: db}, nil
}

// Close releases any resources associated with the reader.
func (r *Reader) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

// ListSnapshots returns metadata for every snapshot stored in the archive.
func (r *Reader) ListSnapshots() ([]SnapshotInfo, error) {
	if r == nil {
		return nil, fmt.Errorf("reader is nil")
	}
	rows, err := r.db.Query(`SELECT id, name, parent_id, metadata, created_at FROM snapshots ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var infos []SnapshotInfo
	for rows.Next() {
		var (
			id        int64
			name      string
			parent    sql.NullInt64
			metaJSON  string
			createdAt string
		)
		if err := rows.Scan(&id, &name, &parent, &metaJSON, &createdAt); err != nil {
			return nil, err
		}
		meta := map[string]string{}
		if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
			meta = map[string]string{"raw": metaJSON}
		}
		var parentPtr *int64
		if parent.Valid {
			parentPtr = &parent.Int64
		}
		ts, _ := time.Parse(time.RFC3339, createdAt)
		infos = append(infos, SnapshotInfo{ID: id, Name: name, ParentID: parentPtr, Metadata: meta, CreatedAt: ts})
	}
	return infos, rows.Err()
}

// ResolveSnapshot returns information about the requested snapshot name. When
// name is empty, the most recent snapshot is returned.
func (r *Reader) ResolveSnapshot(name string) (SnapshotInfo, error) {
	if r == nil {
		return SnapshotInfo{}, fmt.Errorf("reader is nil")
	}
	query := `SELECT id, name, parent_id, metadata, created_at FROM snapshots`
	var rows *sql.Rows
	var err error
	if strings.TrimSpace(name) == "" {
		query += ` ORDER BY id DESC LIMIT 1`
		rows, err = r.db.Query(query)
	} else {
		query += ` WHERE name = ? LIMIT 1`
		rows, err = r.db.Query(query, name)
	}
	if err != nil {
		return SnapshotInfo{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if strings.TrimSpace(name) == "" {
			return SnapshotInfo{}, fmt.Errorf("archive contains no snapshots")
		}
		return SnapshotInfo{}, fmt.Errorf("snapshot %q not found", name)
	}
	var (
		id        int64
		resolved  string
		parent    sql.NullInt64
		metaJSON  string
		createdAt string
	)
	if err := rows.Scan(&id, &resolved, &parent, &metaJSON, &createdAt); err != nil {
		return SnapshotInfo{}, err
	}
	meta := map[string]string{}
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		meta = map[string]string{"raw": metaJSON}
	}
	var parentPtr *int64
	if parent.Valid {
		parentPtr = &parent.Int64
	}
	ts, _ := time.Parse(time.RFC3339, createdAt)
	return SnapshotInfo{ID: id, Name: resolved, ParentID: parentPtr, Metadata: meta, CreatedAt: ts}, nil
}

// ReadManifests returns all manifest records for the given snapshot.
func (r *Reader) ReadManifests(snapshot SnapshotInfo) ([]ManifestRecord, error) {
	rows, err := r.db.Query(`SELECT mi.manifest_id, mi.api_version, mi.kind, mi.namespace, mi.name, b.data
		FROM manifest_index mi JOIN blobs b ON mi.digest = b.digest
		WHERE mi.snapshot_id = ? ORDER BY mi.manifest_id`, snapshot.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var manifests []ManifestRecord
	for rows.Next() {
		var rec ManifestRecord
		var body []byte
		if err := rows.Scan(&rec.ID, &rec.APIVersion, &rec.Kind, &rec.Namespace, &rec.Name, &body); err != nil {
			return nil, err
		}
		rec.Body = string(body)
		manifests = append(manifests, rec)
	}
	return manifests, rows.Err()
}

// AttachmentRecord represents a stored attachment blob for a snapshot.
type AttachmentRecord struct {
	Name      string
	MediaType string
	Data      []byte
}

// ReadAttachments returns every attachment associated with the snapshot.
func (r *Reader) ReadAttachments(snapshot SnapshotInfo) ([]AttachmentRecord, error) {
	rows, err := r.db.Query(`SELECT sb.key, b.media_type, b.data
		FROM snapshot_blobs sb JOIN blobs b ON sb.digest = b.digest
		WHERE sb.snapshot_id = ? AND sb.role = ?
		ORDER BY sb.key`, snapshot.ID, "attachment")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var attachments []AttachmentRecord
	for rows.Next() {
		var rec AttachmentRecord
		if err := rows.Scan(&rec.Name, &rec.MediaType, &rec.Data); err != nil {
			return nil, err
		}
		attachments = append(attachments, rec)
	}
	return attachments, rows.Err()
}

// ListSnapshots reads snapshot metadata from a file on disk.
func ListSnapshots(path string) ([]SnapshotInfo, error) {
	reader, err := NewReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return reader.ListSnapshots()
}
