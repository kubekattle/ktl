package main

import "testing"

func TestResolveDeployPlanFormat(t *testing.T) {
	cases := []struct {
		name      string
		format    string
		visualize bool
		want      string
	}{
		{name: "default text", format: "", visualize: false, want: "text"},
		{name: "explicit text", format: "text", visualize: false, want: "text"},
		{name: "visualize defaults to html", format: "html", visualize: true, want: "visualize-html"},
		{name: "visualize with empty format stays html", format: "", visualize: true, want: "text"},
		{name: "visualize with text stays text", format: "text", visualize: true, want: "text"},
		{name: "visualize with json", format: "json", visualize: true, want: "visualize-json"},
		{name: "visualize with yaml", format: "yaml", visualize: true, want: "visualize-yaml"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveDeployPlanFormat(tc.format, tc.visualize)
			if got != tc.want {
				t.Fatalf("resolveDeployPlanFormat(%q, %v)=%q, want %q", tc.format, tc.visualize, got, tc.want)
			}
		})
	}
}
