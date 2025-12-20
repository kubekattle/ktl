package main

import "testing"

func TestRedactorIncidentPreset(t *testing.T) {
	r, err := newRedactor("incident", nil)
	if err != nil {
		t.Fatalf("newRedactor: %v", err)
	}
	in := "Authorization: Bearer abcdefghijklmnopqrstuvwxyz\npassword=supersecret\nAKIA0123456789ABCDEF"
	out := r.apply(in)
	if out == in {
		t.Fatalf("expected redaction to change input")
	}
	rep := r.report()
	if rep.Preset != "incident" {
		t.Fatalf("expected preset incident, got %q", rep.Preset)
	}
	if len(rep.Rules) == 0 {
		t.Fatalf("expected non-empty report rules")
	}
}

func TestRedactorCustomRegex(t *testing.T) {
	r, err := newRedactor("", []string{`foo\d+`})
	if err != nil {
		t.Fatalf("newRedactor: %v", err)
	}
	out := r.apply("hello foo123 world")
	if out != "hello <redacted> world" {
		t.Fatalf("unexpected output: %q", out)
	}
}
