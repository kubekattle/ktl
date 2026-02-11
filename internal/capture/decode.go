package capture

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"
)

// DecodePayload returns the decoded payload bytes for a captured event row.
// It supports the current `json+gzip` encoding and falls back to the stored JSON text.
func DecodePayload(payloadType string, payloadBlob []byte, payloadJSON string) ([]byte, error) {
	payloadType = strings.TrimSpace(payloadType)
	switch payloadType {
	case "json+gzip":
		if len(payloadBlob) == 0 {
			return nil, nil
		}
		zr, err := gzip.NewReader(bytes.NewReader(payloadBlob))
		if err != nil {
			return nil, fmt.Errorf("gzip reader: %w", err)
		}
		defer zr.Close()
		out, err := io.ReadAll(zr)
		if err != nil {
			return nil, fmt.Errorf("gzip read: %w", err)
		}
		return out, nil
	case "", "json":
		if strings.TrimSpace(payloadJSON) == "" {
			return nil, nil
		}
		return []byte(payloadJSON), nil
	default:
		if strings.TrimSpace(payloadJSON) != "" {
			return []byte(payloadJSON), nil
		}
		return payloadBlob, nil
	}
}
