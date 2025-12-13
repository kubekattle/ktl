package main

import "testing"

func TestWindowLabel(t *testing.T) {
	tests := []struct {
		days int
		want string
	}{
		{0, "all history"},
		{1, "24h"},
		{3, "3d"},
	}
	for _, tt := range tests {
		if got := windowLabel(tt.days); got != tt.want {
			t.Fatalf("windowLabel(%d)=%s want %s", tt.days, got, tt.want)
		}
	}
}
