// archive.go implements the writer used to create ktl .k8s application archives.
package apparchive

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS metadata (
    key TEXT PRIMARY KEY,
    value TEXT
);
CREATE TABLE IF NOT EXISTS blobs (
    digest TEXT PRIMARY KEY,
    media_type TEXT,
    size INTEGER,
    data BLOB NOT NULL,
    created_at TEXT DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE,
    parent_id INTEGER REFERENCES snapshots(id) ON DELETE SET NULL,
    metadata TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS snapshot_blobs (
    snapshot_id INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    digest TEXT NOT NULL REFERENCES blobs(digest),
    role TEXT NOT NULL,
    key TEXT,
    PRIMARY KEY(snapshot_id, digest, role, key)
);
CREATE TABLE IF NOT EXISTS manifest_index (
    snapshot_id INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    digest TEXT NOT NULL REFERENCES blobs(digest),
    manifest_id TEXT,
    api_version TEXT,
    kind TEXT,
    namespace TEXT,
    name TEXT,
    PRIMARY KEY(snapshot_id, digest)
);
CREATE TABLE IF NOT EXISTS image_index (
    snapshot_id INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    image_ref TEXT NOT NULL,
    manifest_digest TEXT NOT NULL REFERENCES blobs(digest),
    config_digest TEXT REFERENCES blobs(digest),
    PRIMARY KEY(snapshot_id, image_ref)
);
CREATE TABLE IF NOT EXISTS image_layers (
    snapshot_id INTEGER NOT NULL REFERENCES snapshots(id) ON DELETE CASCADE,
    image_ref TEXT NOT NULL,
    layer_digest TEXT NOT NULL REFERENCES blobs(digest),
    media_type TEXT,
    order_index INTEGER NOT NULL,
    PRIMARY KEY(snapshot_id, image_ref, order_index)
);
`

// Builder coordinates writing manifest/image metadata into a SQLite archive.
type Builder struct {
	db         *sql.DB
	snapshotID int64
}

// SnapshotMetadata captures identifying information for a snapshot layer.
type SnapshotMetadata struct {
	Name         string
	Parent       string
	Release      string
	Namespace    string
	Chart        string
	ChartVersion string
	KubeVersion  string
	GitCommit    string
	GitDirty     bool
	Notes        string
	Extra        map[string]string
}

// SnapshotInfo describes an existing snapshot.
type SnapshotInfo struct {
	ID        int64
	Name      string
	ParentID  *int64
	Metadata  map[string]string
	CreatedAt time.Time
}

// ManifestRecord describes a single rendered Kubernetes object.
type ManifestRecord struct {
	ID         string
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
	Body       string
	Checksum   string
}

// Attachment represents any auxiliary file to embed (reports, notes, etc).
type Attachment struct {
	Name      string
	MediaType string
	Data      []byte
}

// BlobRecord represents a content-addressed blob stored in the archive.
type BlobRecord struct {
	Digest    string
	MediaType string
	Data      []byte
}

// ImageLayerRecord stores a single OCI layer blob.
type ImageLayerRecord struct {
	Blob  BlobRecord
	Order int
}

// ImageRecord holds the manifest/config/layers for an image reference.
type ImageRecord struct {
	Reference string
	Manifest  BlobRecord
	Config    BlobRecord
	Layers    []ImageLayerRecord
}

// NewBuilder initializes the SQLite schema at path and creates a snapshot row using meta.
func NewBuilder(path string, meta SnapshotMetadata) (*Builder, error) {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	db, err := openDatabase(path)
	if err != nil {
		return nil, err
	}
	snapshotID, err := createSnapshot(db, meta)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Builder{db: db, snapshotID: snapshotID}, nil
}

// Close releases database resources.
func (b *Builder) Close() error {
	if b == nil || b.db == nil {
		return nil
	}
	return b.db.Close()
}

// SnapshotID returns the ID of the snapshot being built.
func (b *Builder) SnapshotID() int64 {
	if b == nil {
		return 0
	}
	return b.snapshotID
}

// AddManifest stores a rendered manifest referencing the snapshot layer.
func (b *Builder) AddManifest(rec ManifestRecord) error {
	if b == nil {
		return nil
	}
	if strings.TrimSpace(rec.Body) == "" {
		return nil
	}
	mediaType := "application/yaml"
	digest := rec.Checksum
	if digest == "" {
		digest = digestBytes([]byte(rec.Body))
	}
	if err := b.storeBlob(BlobRecord{Digest: digest, MediaType: mediaType, Data: []byte(rec.Body)}); err != nil {
		return err
	}
	if err := b.linkBlob(digest, "manifest", rec.ID); err != nil {
		return err
	}
	_, err := b.db.Exec(`INSERT OR REPLACE INTO manifest_index(snapshot_id, digest, manifest_id, api_version, kind, namespace, name)
        VALUES(?,?,?,?,?,?,?)`, b.snapshotID, digest, rec.ID, rec.APIVersion, rec.Kind, rec.Namespace, rec.Name)
	return err
}

// AddAttachment stores an auxiliary file blob.
func (b *Builder) AddAttachment(att Attachment) error {
	if b == nil || len(att.Data) == 0 {
		return nil
	}
	digest := digestBytes(att.Data)
	mediaType := att.MediaType
	if strings.TrimSpace(mediaType) == "" {
		mediaType = "application/octet-stream"
	}
	if err := b.storeBlob(BlobRecord{Digest: digest, MediaType: mediaType, Data: att.Data}); err != nil {
		return err
	}
	return b.linkBlob(digest, "attachment", att.Name)
}

// AddImage stores an OCI image manifest/config/layers as content-addressed blobs.
func (b *Builder) AddImage(img ImageRecord) error {
	if b == nil {
		return nil
	}
	if err := b.storeBlob(img.Manifest); err != nil {
		return err
	}
	if len(img.Config.Data) > 0 {
		if err := b.storeBlob(img.Config); err != nil {
			return err
		}
	}
	for _, layer := range img.Layers {
		if err := b.storeBlob(layer.Blob); err != nil {
			return err
		}
		if err := b.linkBlob(layer.Blob.Digest, "image-layer", img.Reference); err != nil {
			return err
		}
		if _, err := b.db.Exec(`INSERT OR REPLACE INTO image_layers(snapshot_id, image_ref, layer_digest, media_type, order_index)
            VALUES(?,?,?,?,?)`, b.snapshotID, img.Reference, layer.Blob.Digest, layer.Blob.MediaType, layer.Order); err != nil {
			return err
		}
	}
	if err := b.linkBlob(img.Manifest.Digest, "image-manifest", img.Reference); err != nil {
		return err
	}
	if img.Config.Digest != "" {
		if err := b.linkBlob(img.Config.Digest, "image-config", img.Reference); err != nil {
			return err
		}
	}
	_, err := b.db.Exec(`INSERT OR REPLACE INTO image_index(snapshot_id, image_ref, manifest_digest, config_digest)
        VALUES(?,?,?,?)`, b.snapshotID, img.Reference, img.Manifest.Digest, img.Config.Digest)
	return err
}

// SetMetadata upserts the provided key/value pairs at the archive level.
func (b *Builder) SetMetadata(values map[string]string) error {
	if b == nil || len(values) == 0 {
		return nil
	}
	tx, err := b.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO metadata(key,value) VALUES(?,?)
        ON CONFLICT(key) DO UPDATE SET value=excluded.value`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for k, v := range values {
		if _, err := stmt.Exec(k, v); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func openDatabase(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return db, nil
}

func createSnapshot(db *sql.DB, meta SnapshotMetadata) (int64, error) {
	if strings.TrimSpace(meta.Name) == "" {
		meta.Name = fmt.Sprintf("layer-%s", time.Now().UTC().Format("20060102-150405"))
	}
	var parentID interface{}
	if strings.TrimSpace(meta.Parent) != "" {
		id, err := lookupSnapshotID(db, meta.Parent)
		if err != nil {
			return 0, err
		}
		parentID = id
	}
	payload, err := json.Marshal(metaToMap(meta))
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(`INSERT INTO snapshots(name, parent_id, metadata, created_at) VALUES(?,?,?,?)`, meta.Name, parentID, string(payload), now)
	if err != nil {
		return 0, err
	}
	snapshotID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return snapshotID, nil
}

func lookupSnapshotID(db *sql.DB, name string) (int64, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM snapshots WHERE name = ?`, name).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("parent snapshot %q not found", name)
		}
		return 0, err
	}
	return id, nil
}

func (b *Builder) storeBlob(br BlobRecord) error {
	if len(br.Data) == 0 {
		return fmt.Errorf("blob %s has no data", br.Digest)
	}
	digest := br.Digest
	if strings.TrimSpace(digest) == "" {
		digest = digestBytes(br.Data)
	}
	mediaType := br.MediaType
	if strings.TrimSpace(mediaType) == "" {
		mediaType = "application/octet-stream"
	}
	_, err := b.db.Exec(`INSERT INTO blobs(digest, media_type, size, data)
        VALUES(?,?,?,?) ON CONFLICT(digest) DO NOTHING`, digest, mediaType, len(br.Data), br.Data)
	br.Digest = digest
	return err
}

func (b *Builder) linkBlob(digest, role, key string) error {
	_, err := b.db.Exec(`INSERT OR IGNORE INTO snapshot_blobs(snapshot_id, digest, role, key) VALUES(?,?,?,?)`, b.snapshotID, digest, role, key)
	return err
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func metaToMap(meta SnapshotMetadata) map[string]string {
	m := map[string]string{
		"name":         meta.Name,
		"release":      meta.Release,
		"namespace":    meta.Namespace,
		"chart":        meta.Chart,
		"chartVersion": meta.ChartVersion,
		"kubeVersion":  meta.KubeVersion,
		"gitCommit":    meta.GitCommit,
		"notes":        meta.Notes,
	}
	if meta.GitDirty {
		m["gitDirty"] = "true"
	}
	for k, v := range meta.Extra {
		if strings.TrimSpace(k) == "" {
			continue
		}
		m[k] = v
	}
	return m
}

func ensureDir(dir string) error {
	if dir == "" || dir == "." || dir == string(filepath.Separator) {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}
