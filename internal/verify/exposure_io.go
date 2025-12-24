package verify

import (
	"encoding/json"
	"io"
)

func WriteExposureJSON(w io.Writer, ex *ExposureReport) error {
	if w == nil || ex == nil {
		return nil
	}
	raw, err := json.MarshalIndent(ex, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	_, _ = w.Write(raw)
	return nil
}
