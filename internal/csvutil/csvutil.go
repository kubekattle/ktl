// File: internal/csvutil/csvutil.go
// Brief: Internal csvutil package implementation for 'csvutil'.

// Package csvutil provides csvutil helpers.

package csvutil

import (
	"encoding/csv"
	"io"
	"strings"
)

// SplitFields parses a comma-separated key/value list using CSV semantics.
func SplitFields(raw string) ([]string, error) {
	reader := csv.NewReader(strings.NewReader(raw))
	reader.Comma = ','
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	record, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, err
	}
	return record, nil
}
