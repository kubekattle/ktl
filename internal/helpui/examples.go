package helpui

// curatedExamples supplements Cobra's .Example fields with task-based golden paths.
// Keys are Cobra command paths (CommandPath()).
var curatedExamples = map[string][]string{
	"ktl logs": {
		"# Tail pods matching a regex in a namespace\nktl logs 'checkout-.*' -n prod-payments",
		"# Highlight errors\nktl logs 'checkout-.*' -n prod-payments --highlight ERROR",
	},
	"ktl build": {
		"# Build an image from a directory\nktl build --context . --tag ghcr.io/acme/app:dev",
		"# Share the build stream over WebSocket\nktl build --context . --ws-listen :9085",
	},
	"ktl apply plan": {
		"# Preview a Helm upgrade\nktl apply plan --chart ./chart --release foo -n default",
		"# Render a shareable HTML visualization\nktl apply plan --visualize --chart ./chart --release foo -n default",
	},
	"ktl apply": {
		"# Deploy a chart\nktl apply --chart ./chart --release foo -n default",
		"# Run the deploy viewer\nktl apply --chart ./chart --release foo -n default --ui",
	},
	"ktl delete": {
		"# Delete a release\nktl delete --release foo -n default",
		"# Run the destroy viewer\nktl delete --release foo -n default --ui",
	},
	"ktl revert": {
		"# Revert a release to the last known-good revision\nktl revert --release foo -n default",
	},
	"ktl env": {
		"# Show env var reference (machine-readable)\nktl env --format json",
	},
	"ktl version": {
		"# Print version information\nktl version",
	},
	"ktl verify": {
		"# Run the bundled verify showcase (includes a CRITICAL rule)\nktl verify testdata/verify/showcase/verify.yaml",
		"# Verify a live namespace via YAML config\ncat > verify-namespace.yaml <<'YAML'\nversion: v1\n\ntarget:\n  kind: namespace\n  namespace: default\n\nkube:\n  kubeconfig: $HOME/.kube/archimedes.yaml\n\nverify:\n  mode: warn\n  failOn: high\n\noutput:\n  format: table\n  report: \"-\"\nYAML\n\nktl verify verify-namespace.yaml",
		"# Verify rendered manifests from a file\ncat > verify-manifest.yaml <<'YAML'\nversion: v1\n\ntarget:\n  kind: manifest\n  manifest: ./rendered.yaml\n\nverify:\n  mode: block\n  failOn: high\n\noutput:\n  format: table\n  report: \"-\"\nYAML\n\nktl verify verify-manifest.yaml",
	},
}
