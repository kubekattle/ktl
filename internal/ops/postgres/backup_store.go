package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const (
	defaultBackupMultipartPartSize int64 = 64 * 1024 * 1024
	backupCatalogAPIVersion              = "torque.dev/postgres-backup-catalog/v1"
	backupCatalogKind                    = "PostgresBackupCatalogRecord"
	backupUploadSessionAPIVersion        = "torque.dev/postgres-backup-upload-session/v1"
	backupUploadSessionKind              = "PostgresBackupUploadSession"
)

type BackupCatalogRecord struct {
	APIVersion  string             `json:"apiVersion"`
	Kind        string             `json:"kind"`
	ID          string             `json:"id"`
	Status      string             `json:"status"`
	Engine      string             `json:"engine"`
	Database    string             `json:"database"`
	File        string             `json:"file"`
	Manifest    string             `json:"manifestPath"`
	CatalogPath string             `json:"catalogPath"`
	Sha256      string             `json:"sha256"`
	Bytes       int64              `json:"bytes"`
	RunID       string             `json:"runId,omitempty"`
	NodeID      string             `json:"nodeId,omitempty"`
	Store       *BackupStoreResult `json:"store,omitempty"`
	CreatedAt   string             `json:"createdAt"`
	VerifiedAt  string             `json:"verifiedAt,omitempty"`
}

type s3UploadSession struct {
	APIVersion    string                `json:"apiVersion"`
	Kind          string                `json:"kind"`
	BackupID      string                `json:"backupId"`
	Bucket        string                `json:"bucket"`
	Key           string                `json:"key"`
	UploadID      string                `json:"uploadId"`
	FileSha256    string                `json:"fileSha256"`
	FileBytes     int64                 `json:"fileBytes"`
	PartSizeBytes int64                 `json:"partSizeBytes"`
	Parts         []s3UploadSessionPart `json:"parts,omitempty"`
	UpdatedAt     string                `json:"updatedAt"`
}

type s3UploadSessionPart struct {
	Number int32  `json:"number"`
	ETag   string `json:"etag"`
	Size   int64  `json:"size"`
}

type normalizedBackupStore struct {
	Type               string
	EnvFile            string
	Ref                string
	Bucket             string
	Prefix             string
	Region             string
	Endpoint           string
	PathStyle          bool
	PartSizeBytes      int64
	SessionPath        string
	AccessKeyIDEnv     string
	SecretAccessKeyEnv string
	SessionTokenEnv    string
}

func backupStoreEnabled(spec BackupStoreSpec) bool {
	typ := strings.ToLower(strings.TrimSpace(spec.Type))
	return typ != "" || strings.TrimSpace(spec.Ref) != "" || strings.TrimSpace(spec.Bucket) != ""
}

func normalizeBackupStore(spec BackupStoreSpec, envFileOpt ...string) (normalizedBackupStore, error) {
	envFile := ""
	if len(envFileOpt) > 0 {
		envFile = strings.TrimSpace(envFileOpt[0])
	}
	out := normalizedBackupStore{
		Type:               strings.ToLower(strings.TrimSpace(spec.Type)),
		EnvFile:            envFile,
		Ref:                first(strings.TrimSpace(spec.Ref), envValueFrom(envFile, spec.RefEnv)),
		Bucket:             first(strings.TrimSpace(spec.Bucket), envValueFrom(envFile, spec.BucketEnv)),
		Prefix:             first(strings.TrimSpace(spec.Prefix), envValueFrom(envFile, spec.PrefixEnv)),
		Region:             first(strings.TrimSpace(spec.Region), envValueFrom(envFile, spec.RegionEnv), envValueFrom(envFile, "AWS_REGION"), envValueFrom(envFile, "AWS_DEFAULT_REGION"), "us-east-1"),
		Endpoint:           first(strings.TrimSpace(spec.Endpoint), envValueFrom(envFile, spec.EndpointEnv)),
		PathStyle:          spec.PathStyle,
		PartSizeBytes:      spec.PartSizeBytes,
		SessionPath:        strings.TrimSpace(spec.SessionPath),
		AccessKeyIDEnv:     strings.TrimSpace(spec.AccessKeyIDEnv),
		SecretAccessKeyEnv: strings.TrimSpace(spec.SecretAccessKeyEnv),
		SessionTokenEnv:    strings.TrimSpace(spec.SessionTokenEnv),
	}
	if out.Type == "" && (out.Ref != "" || out.Bucket != "") {
		out.Type = "s3"
	}
	if out.Type == "" {
		return out, nil
	}
	if out.Type != "s3" {
		return out, fmt.Errorf("unsupported backup store type %q", out.Type)
	}
	if out.Ref != "" {
		bucket, prefix, err := parseS3Ref(out.Ref)
		if err != nil {
			return out, err
		}
		if out.Bucket == "" {
			out.Bucket = bucket
		}
		if out.Prefix == "" {
			out.Prefix = prefix
		}
	}
	if out.Bucket == "" {
		return out, fmt.Errorf("s3 backup store requires bucket or ref")
	}
	out.Prefix = normalizeS3Prefix(out.Prefix)
	if out.PartSizeBytes <= 0 {
		out.PartSizeBytes = defaultBackupMultipartPartSize
	}
	return out, nil
}

func parseS3Ref(ref string) (string, string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", fmt.Errorf("s3 ref is empty")
	}
	if strings.HasPrefix(ref, "s3://") {
		u, err := url.Parse(ref)
		if err != nil {
			return "", "", fmt.Errorf("parse s3 ref: %w", err)
		}
		bucket := strings.TrimSpace(u.Host)
		prefix := strings.TrimPrefix(u.EscapedPath(), "/")
		if decoded, derr := url.PathUnescape(prefix); derr == nil {
			prefix = decoded
		}
		return bucket, normalizeS3Prefix(prefix), nil
	}
	bucket, prefix, _ := strings.Cut(ref, "/")
	return strings.TrimSpace(bucket), normalizeS3Prefix(prefix), nil
}

func normalizeS3Prefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	prefix = strings.TrimPrefix(path.Clean("/"+prefix), "/")
	if prefix == "." {
		return ""
	}
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return prefix
}

func backupID(result *Result, spec Spec, dbName string, file string) string {
	if id := strings.TrimSpace(spec.Backup.ID); id != "" {
		return strings.Trim(id, "/")
	}
	runID := ""
	if result != nil {
		runID = strings.TrimSpace(result.RunID)
	}
	if runID == "" {
		runID = time.Now().UTC().Format("20060102T150405Z")
	}
	return strings.Trim(dbName, "/") + "/" + safeFileToken(runID)
}

func backupCatalogPath(spec Spec, manifest string) string {
	if path := strings.TrimSpace(spec.Backup.CatalogPath); path != "" {
		return path
	}
	if strings.TrimSpace(manifest) != "" {
		return strings.TrimSpace(manifest) + ".catalog.json"
	}
	if file := strings.TrimSpace(spec.Backup.File); file != "" {
		return file + ".catalog.json"
	}
	return ""
}

func backupObjectKey(store normalizedBackupStore, backupID string, file string) string {
	name := filepath.Base(strings.TrimSpace(file))
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = safeFileToken(backupID) + ".dump"
	}
	return joinS3Key(store.Prefix, "base", backupID, name)
}

func backupManifestKey(store normalizedBackupStore, backupID string, manifest string) string {
	name := filepath.Base(strings.TrimSpace(manifest))
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = safeFileToken(backupID) + ".manifest.json"
	}
	return joinS3Key(store.Prefix, "base", backupID, name)
}

func backupCatalogKey(store normalizedBackupStore, backupID string, catalog string) string {
	name := filepath.Base(strings.TrimSpace(catalog))
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = safeFileToken(backupID) + ".catalog.json"
	}
	return joinS3Key(store.Prefix, "catalog", backupID, name)
}

func joinS3Key(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(strings.TrimSpace(part), "/")
		if part != "" && part != "." {
			cleaned = append(cleaned, part)
		}
	}
	return strings.Join(cleaned, "/")
}

func s3URI(bucket string, key string) string {
	return "s3://" + strings.TrimSpace(bucket) + "/" + strings.TrimLeft(strings.TrimSpace(key), "/")
}

func newBackupS3Client(ctx context.Context, store normalizedBackupStore) (*s3.Client, error) {
	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(store.Region)}
	if store.AccessKeyIDEnv != "" || store.SecretAccessKeyEnv != "" {
		accessKey := envValueFrom(store.EnvFile, store.AccessKeyIDEnv)
		secretKey := envValueFrom(store.EnvFile, store.SecretAccessKeyEnv)
		if accessKey == "" || secretKey == "" {
			return nil, fmt.Errorf("s3 backup store credential env values are incomplete")
		}
		opts = append(opts, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, envValueFrom(store.EnvFile, store.SessionTokenEnv))))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config for s3 backup store: %w", err)
	}
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = store.PathStyle
		if store.Endpoint != "" {
			o.BaseEndpoint = aws.String(store.Endpoint)
		}
	}), nil
}

func uploadBackupArtifacts(ctx context.Context, spec BackupStoreSpec, envFile string, backupID string, file string, manifest string, catalog string, sha string, bytes int64) (*BackupStoreResult, error) {
	store, err := normalizeBackupStore(spec, envFile)
	if err != nil {
		return nil, err
	}
	if store.Type == "" {
		return nil, nil
	}
	client, err := newBackupS3Client(ctx, store)
	if err != nil {
		return nil, err
	}
	key := backupObjectKey(store, backupID, file)
	sessionPath := first(store.SessionPath, defaultS3BackupSessionPath(file, backupID))
	result := &BackupStoreResult{
		Type:          store.Type,
		Bucket:        store.Bucket,
		Key:           key,
		URI:           s3URI(store.Bucket, key),
		Region:        store.Region,
		Endpoint:      store.Endpoint,
		Bytes:         bytes,
		Sha256:        sha,
		PartSizeBytes: store.PartSizeBytes,
		SessionPath:   sessionPath,
	}
	if exists, etag, err := matchingS3ObjectExists(ctx, client, store.Bucket, key, sha, bytes); err != nil {
		return nil, err
	} else if exists {
		result.Uploaded = false
		result.Resumed = true
		result.ETag = etag
	} else if bytes >= store.PartSizeBytes {
		multipart, err := uploadS3MultipartFile(ctx, client, store, backupID, key, file, sha, bytes, sessionPath)
		if err != nil {
			return nil, err
		}
		result.Uploaded = true
		result.Resumed = multipart.Resumed
		result.Multipart = true
		result.UploadID = multipart.UploadID
		result.Parts = multipart.Parts
		result.ETag = multipart.ETag
	} else {
		etag, err := putS3File(ctx, client, store.Bucket, key, file, map[string]string{
			"backup-id": safeMetadataValue(backupID),
			"sha256":    sha,
		})
		if err != nil {
			return nil, err
		}
		result.Uploaded = true
		result.ETag = etag
	}
	if strings.TrimSpace(manifest) != "" {
		manifestKey := backupManifestKey(store, backupID, manifest)
		if _, err := putS3File(ctx, client, store.Bucket, manifestKey, manifest, map[string]string{"backup-id": safeMetadataValue(backupID), "artifact": "manifest"}); err != nil {
			return nil, err
		}
		result.ManifestKey = manifestKey
		result.ManifestURI = s3URI(store.Bucket, manifestKey)
	}
	if strings.TrimSpace(catalog) != "" && fileExists(catalog) {
		catalogKey := backupCatalogKey(store, backupID, catalog)
		if _, err := putS3File(ctx, client, store.Bucket, catalogKey, catalog, map[string]string{"backup-id": safeMetadataValue(backupID), "artifact": "catalog"}); err != nil {
			return nil, err
		}
		result.CatalogKey = catalogKey
		result.CatalogURI = s3URI(store.Bucket, catalogKey)
	}
	return result, nil
}

func ensureBackupLocalFromStore(ctx context.Context, spec Spec, file string) (bool, error) {
	if file == "" || fileExists(file) || !backupStoreEnabled(spec.Backup.Store) {
		return false, nil
	}
	store, err := normalizeBackupStore(spec.Backup.Store, spec.EnvFile)
	if err != nil {
		return false, err
	}
	if store.Type == "" {
		return false, nil
	}
	backupID := strings.Trim(strings.TrimSpace(spec.Backup.ID), "/")
	if backupID == "" {
		dbName := first(spec.Backup.Database, spec.Database)
		backupID = strings.Trim(dbName, "/") + "/" + safeFileToken(strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)))
	}
	key := backupObjectKey(store, backupID, file)
	client, err := newBackupS3Client(ctx, store)
	if err != nil {
		return false, err
	}
	return true, downloadS3Object(ctx, client, store.Bucket, key, file)
}

func matchingS3ObjectExists(ctx context.Context, client *s3.Client, bucket string, key string, sha string, bytes int64) (bool, string, error) {
	head, err := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		if isNotFound(err) {
			return false, "", nil
		}
		return false, "", fmt.Errorf("head s3 object %s: %w", s3URI(bucket, key), err)
	}
	if head.ContentLength != nil && *head.ContentLength != bytes {
		return false, aws.ToString(head.ETag), nil
	}
	if sha != "" {
		if got := strings.TrimSpace(head.Metadata["sha256"]); got != "" && got != sha {
			return false, aws.ToString(head.ETag), nil
		}
	}
	return true, aws.ToString(head.ETag), nil
}

func putS3File(ctx context.Context, client *s3.Client, bucket string, key string, file string, metadata map[string]string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	out, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		Body:          f,
		ContentLength: aws.Int64(info.Size()),
		Metadata:      metadata,
	})
	if err != nil {
		return "", fmt.Errorf("put s3 object %s: %w", s3URI(bucket, key), err)
	}
	return aws.ToString(out.ETag), nil
}

func downloadS3Object(ctx context.Context, client *s3.Client, bucket string, key string, file string) error {
	out, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("get s3 object %s: %w", s3URI(bucket, key), err)
	}
	defer out.Body.Close()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp := file + ".download.tmp." + strconv.Itoa(os.Getpid())
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, out.Body)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, file)
}

type multipartUploadResult struct {
	UploadID string
	ETag     string
	Parts    int
	Resumed  bool
}

func uploadS3MultipartFile(ctx context.Context, client *s3.Client, store normalizedBackupStore, backupID string, key string, file string, sha string, bytes int64, sessionPath string) (multipartUploadResult, error) {
	session, resumed, err := loadOrCreateS3UploadSession(ctx, client, store, backupID, key, file, sha, bytes, sessionPath)
	if err != nil {
		return multipartUploadResult{}, err
	}
	f, err := os.Open(file)
	if err != nil {
		return multipartUploadResult{}, err
	}
	defer f.Close()
	uploaded := map[int32]s3UploadSessionPart{}
	for _, part := range session.Parts {
		uploaded[part.Number] = part
	}
	for number, offset := int32(1), int64(0); offset < bytes; number, offset = number+1, offset+session.PartSizeBytes {
		size := session.PartSizeBytes
		if remaining := bytes - offset; remaining < size {
			size = remaining
		}
		if part, ok := uploaded[number]; ok && part.Size == size && strings.TrimSpace(part.ETag) != "" {
			continue
		}
		body := io.NewSectionReader(f, offset, size)
		out, err := client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket:        aws.String(store.Bucket),
			Key:           aws.String(key),
			UploadId:      aws.String(session.UploadID),
			PartNumber:    aws.Int32(number),
			Body:          body,
			ContentLength: aws.Int64(size),
		})
		if err != nil {
			_ = saveS3UploadSession(sessionPath, session)
			return multipartUploadResult{}, fmt.Errorf("upload s3 part %d for %s: %w", number, s3URI(store.Bucket, key), err)
		}
		part := s3UploadSessionPart{Number: number, ETag: aws.ToString(out.ETag), Size: size}
		uploaded[number] = part
		session.Parts = sortedSessionParts(uploaded)
		session.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := saveS3UploadSession(sessionPath, session); err != nil {
			return multipartUploadResult{}, err
		}
	}
	completed := make([]s3types.CompletedPart, 0, len(uploaded))
	for _, part := range sortedSessionParts(uploaded) {
		completed = append(completed, s3types.CompletedPart{ETag: aws.String(part.ETag), PartNumber: aws.Int32(part.Number)})
	}
	out, err := client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(store.Bucket),
		Key:      aws.String(key),
		UploadId: aws.String(session.UploadID),
		MultipartUpload: &s3types.CompletedMultipartUpload{
			Parts: completed,
		},
	})
	if err != nil {
		return multipartUploadResult{}, fmt.Errorf("complete s3 multipart upload %s: %w", s3URI(store.Bucket, key), err)
	}
	_ = os.Remove(sessionPath)
	return multipartUploadResult{UploadID: session.UploadID, ETag: aws.ToString(out.ETag), Parts: len(completed), Resumed: resumed}, nil
}

func loadOrCreateS3UploadSession(ctx context.Context, client *s3.Client, store normalizedBackupStore, backupID string, key string, file string, sha string, bytes int64, sessionPath string) (*s3UploadSession, bool, error) {
	if sessionPath != "" {
		if raw, err := os.ReadFile(sessionPath); err == nil {
			var session s3UploadSession
			if err := json.Unmarshal(raw, &session); err == nil &&
				session.BackupID == backupID &&
				session.Bucket == store.Bucket &&
				session.Key == key &&
				session.FileSha256 == sha &&
				session.FileBytes == bytes &&
				strings.TrimSpace(session.UploadID) != "" {
				parts, err := listS3UploadParts(ctx, client, store.Bucket, key, session.UploadID)
				if err == nil && len(parts) > 0 {
					session.Parts = parts
				}
				return &session, true, nil
			}
		}
	}
	out, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:   aws.String(store.Bucket),
		Key:      aws.String(key),
		Metadata: map[string]string{"backup-id": safeMetadataValue(backupID), "sha256": sha},
	})
	if err != nil {
		return nil, false, fmt.Errorf("create s3 multipart upload %s: %w", s3URI(store.Bucket, key), err)
	}
	session := &s3UploadSession{
		APIVersion:    backupUploadSessionAPIVersion,
		Kind:          backupUploadSessionKind,
		BackupID:      backupID,
		Bucket:        store.Bucket,
		Key:           key,
		UploadID:      aws.ToString(out.UploadId),
		FileSha256:    sha,
		FileBytes:     bytes,
		PartSizeBytes: store.PartSizeBytes,
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := saveS3UploadSession(sessionPath, session); err != nil {
		return nil, false, err
	}
	return session, false, nil
}

func listS3UploadParts(ctx context.Context, client *s3.Client, bucket string, key string, uploadID string) ([]s3UploadSessionPart, error) {
	var out []s3UploadSessionPart
	var marker *string
	for {
		resp, err := client.ListParts(ctx, &s3.ListPartsInput{
			Bucket:           aws.String(bucket),
			Key:              aws.String(key),
			UploadId:         aws.String(uploadID),
			PartNumberMarker: marker,
		})
		if err != nil {
			return nil, err
		}
		for _, part := range resp.Parts {
			out = append(out, s3UploadSessionPart{
				Number: aws.ToInt32(part.PartNumber),
				ETag:   aws.ToString(part.ETag),
				Size:   aws.ToInt64(part.Size),
			})
		}
		if !aws.ToBool(resp.IsTruncated) {
			break
		}
		marker = resp.NextPartNumberMarker
	}
	return out, nil
}

func sortedSessionParts(parts map[int32]s3UploadSessionPart) []s3UploadSessionPart {
	out := make([]s3UploadSessionPart, 0, len(parts))
	for _, part := range parts {
		out = append(out, part)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

func saveS3UploadSession(path string, session *s3UploadSession) error {
	if strings.TrimSpace(path) == "" || session == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

func defaultS3BackupSessionPath(file string, backupID string) string {
	dir := filepath.Dir(strings.TrimSpace(file))
	if dir == "." || dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, ".torque-"+safeFileToken(backupID)+".s3-upload-session.json")
}

func writeBackupCatalog(path string, record BackupCatalogRecord) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func backupCatalogRecord(result *Result, dbName string, backup BackupResult, status string) BackupCatalogRecord {
	runID, nodeID := "", ""
	if result != nil {
		runID = strings.TrimSpace(result.RunID)
		nodeID = strings.TrimSpace(result.NodeID)
	}
	return BackupCatalogRecord{
		APIVersion:  backupCatalogAPIVersion,
		Kind:        backupCatalogKind,
		ID:          strings.TrimSpace(backup.ID),
		Status:      first(strings.TrimSpace(status), "succeeded"),
		Engine:      "pg_dump",
		Database:    strings.TrimSpace(dbName),
		File:        strings.TrimSpace(backup.File),
		Manifest:    strings.TrimSpace(backup.ManifestPath),
		CatalogPath: strings.TrimSpace(backup.CatalogPath),
		Sha256:      strings.TrimSpace(backup.Sha256),
		Bytes:       backup.Bytes,
		RunID:       runID,
		NodeID:      nodeID,
		Store:       backup.Store,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func safeMetadataValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return value
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	value := strings.ToLower(err.Error())
	return strings.Contains(value, "notfound") ||
		strings.Contains(value, "not found") ||
		strings.Contains(value, "no such key") ||
		strings.Contains(value, "status code: 404")
}
