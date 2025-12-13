// dumper_test.go checks pg_dump command construction and flag handling.
package pgdump

import "testing"

func TestWithPassword(t *testing.T) {
	base := []string{"pg_dump", "-U", "postgres"}
	got := withPassword("s3cr3t value", base)
	if len(got) != len(base)+2 {
		t.Fatalf("expected %d args, got %d", len(base)+2, len(got))
	}
	if got[0] != "env" {
		t.Fatalf("expected env prefix, got %s", got[0])
	}
	if got[1] != "PGPASSWORD=s3cr3t value" {
		t.Fatalf("password assignment mismatch: %s", got[1])
	}
	for i, v := range base {
		if got[i+2] != v {
			t.Fatalf("base command mutated at %d: %s vs %s", i, got[i+2], v)
		}
	}
	if base[0] != "pg_dump" {
		t.Fatalf("base slice was modified")
	}
}

func TestWithPasswordEmpty(t *testing.T) {
	base := []string{"psql"}
	got := withPassword("", base)
	if len(got) != len(base) {
		t.Fatalf("expected same length, got %d", len(got))
	}
	if got[0] != "psql" {
		t.Fatalf("expected command to remain unchanged")
	}
}

func TestDedupe(t *testing.T) {
	values := []string{"", "db", "b", "db"}
	got := dedupe(values)
	expected := []string{"b", "db"}
	if len(got) != len(expected) {
		t.Fatalf("expected %d results, got %d", len(expected), len(got))
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("value mismatch at %d: got %s want %s", i, got[i], expected[i])
		}
	}
}
