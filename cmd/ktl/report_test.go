package main

import (
	"bytes"
	"testing"
)

func TestWriteReportLine_StableOrder(t *testing.T) {
	var buf bytes.Buffer
	writeReportTable(&buf, reportLine{
		Kind:      "apply",
		Result:    "success",
		Release:   "monitoring",
		Namespace: "default",
		Chart:     "tempo",
		Version:   "1.24.1",
		Revision:  9,
		ElapsedMS: 4958,
		DryRun:    false,
	})

	got := buf.String()
	want := "" +
		"Resource                                  Action   Status      Message\n" +
		"------------------------------------------------------------------------\n" +
		"Release default/monitoring                apply    success     chart=tempo elapsed_ms=4958 revision=9 version=1.24.1\n"
	if got != want {
		t.Fatalf("unexpected report line.\nwant: %q\ngot:  %q", want, got)
	}
}
