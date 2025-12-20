// File: internal/caststream/capture_upload.go
// Brief: Internal caststream package implementation for 'capture upload'.

// Package caststream provides caststream helpers.

package caststream

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxCaptureUploadBytes = int64(1 << 30) // 1GiB

func (s *Server) handleCaptureUpload(w http.ResponseWriter, r *http.Request) {
	if s == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxCaptureUploadBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, fmt.Sprintf("read multipart: %v", err), http.StatusBadRequest)
		return
	}
	var uploadPart io.ReadCloser
	name := ""
	for {
		part, err := mr.NextPart()
		if err != nil {
			if err == io.EOF {
				break
			}
			http.Error(w, fmt.Sprintf("read multipart: %v", err), http.StatusBadRequest)
			return
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		uploadPart = part
		name = part.FileName()
		break
	}
	if uploadPart == nil {
		http.Error(w, "missing form file 'file'", http.StatusBadRequest)
		return
	}
	defer uploadPart.Close()
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		ext = ".bin"
	}
	tmp, err := os.CreateTemp("", "ktl-capture-upload-*"+ext)
	if err != nil {
		http.Error(w, fmt.Sprintf("create temp file: %v", err), http.StatusInternalServerError)
		return
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	if _, err := io.Copy(tmp, uploadPart); err != nil {
		http.Error(w, fmt.Sprintf("save upload: %v", err), http.StatusInternalServerError)
		return
	}
	if err := tmp.Close(); err != nil {
		http.Error(w, fmt.Sprintf("close upload: %v", err), http.StatusInternalServerError)
		return
	}

	id := randomID()
	if err := s.importCaptureArtifact(r.Context(), id, tmpPath); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"viewerURL": "/capture/view/" + id,
	})
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
