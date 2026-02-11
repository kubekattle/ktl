package main

import "testing"

func TestFormatHelpURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: ":8080", want: "http://127.0.0.1:8080/"},
		{in: "0.0.0.0:9000", want: "http://127.0.0.1:9000/"},
		{in: "127.0.0.1:9001", want: "http://127.0.0.1:9001/"},
	}
	for _, tt := range tests {
		if got := formatHelpURL(tt.in); got != tt.want {
			t.Fatalf("formatHelpURL(%q)=%q, want %q", tt.in, got, tt.want)
		}
	}
}
