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
	"ktl stack": {
		"# Plan the stack (default: read-only, like `ktl stack plan`)\nktl stack --config ./stacks/prod",
		"# Restrict selection via environment defaults\nKTL_STACK_TAG=critical KTL_STACK_CLUSTER=prod-us ktl stack --config ./stacks/prod",
		"# Emit a machine-readable plan for tooling\nktl stack --config ./stacks/prod --output json",
	},
	"ktl stack plan": {
		"# Write a reproducible plan bundle for review/CI\nktl stack plan --config ./stacks/prod --bundle ./stack-plan.tgz",
		"# Embed a live diff summary in the bundle (requires cluster access)\nktl stack plan --config ./stacks/prod --bundle ./stack-plan.tgz --bundle-diff-summary",
	},
	"ktl stack graph": {
		"# Render a Graphviz DOT graph\nktl stack graph --config ./stacks/prod > stack.dot",
		"# Render a Mermaid graph\nktl stack graph --config ./stacks/prod --format mermaid > stack.mmd",
	},
	"ktl stack explain": {
		"# Explain why a release is selected (by name)\nktl stack explain --config ./stacks/prod api",
		"# Print only selection reasons\nktl stack explain --config ./stacks/prod api --why",
	},
	"ktl stack apply": {
		"# Apply the selected releases (DAG order)\nktl stack apply --config ./stacks/prod --yes",
		"# Resume the most recent run (uses stored frozen plan unless --replan is set)\nktl stack apply --config ./stacks/prod --resume --yes",
		"# Enable manifest diffs (defaulted via env)\nKTL_STACK_APPLY_DIFF=1 ktl stack apply --config ./stacks/prod --yes",
	},
	"ktl stack delete": {
		"# Delete the selected releases (reverse DAG order)\nktl stack delete --config ./stacks/prod --yes",
		"# Prompt only when deleting 50+ releases\nktl stack delete --config ./stacks/prod --delete-confirm-threshold 50",
	},
	"ktl stack status": {
		"# Tail the most recent run\nktl stack status --config ./stacks/prod --follow",
		"# Show a specific run ID (see `ktl stack runs`)\nktl stack status --config ./stacks/prod --run-id 2025-12-30T12-34-56.000000000Z --follow",
	},
	"ktl stack runs": {
		"# List recent runs\nktl stack runs --config ./stacks/prod --limit 50",
	},
	"ktl stack audit": {
		"# Show audit table for the most recent run\nktl stack audit --config ./stacks/prod",
		"# Export a shareable HTML report\nktl stack audit --config ./stacks/prod --output html > stack-audit.html",
	},
	"ktl stack export": {
		"# Export the most recent run as a portable bundle\nktl stack export --config ./stacks/prod",
		"# Export a specific run ID\nktl stack export --config ./stacks/prod --run-id 2025-12-30T12-34-56.000000000Z --out ./exports/run.tgz",
	},
	"ktl stack seal": {
		"# Seal a plan directory for CI (plan.json + attestation.json)\nktl stack seal --config ./stacks/prod --out ./.ktl/stack/sealed --command apply",
		"# Include the inputs bundle (inputs.tar.gz) for fully offline execution\nktl stack seal --config ./stacks/prod --out ./.ktl/stack/sealed --include-bundle --command apply",
	},
	"ktl stack rerun-failed": {
		"# Resume the most recent run and schedule only failed nodes\nktl stack rerun-failed --config ./stacks/prod --yes",
	},
	"ktl verify": {
		"# Generate a starter config (chart render)\nktl verify init chart --chart ./chart --release foo -n default > verify.yaml\nktl verify verify.yaml",
		"# Generate a starter config (live namespace)\nktl verify init namespace -n default --context my-context > verify.yaml\nktl verify verify.yaml",
		"# Run the bundled verify showcase (includes a CRITICAL rule)\nktl verify testdata/verify/showcase/verify.yaml",
		"# Verify a live namespace via YAML config\ncat > verify-namespace.yaml <<'YAML'\nversion: v1\n\ntarget:\n  kind: namespace\n  namespace: default\n\nkube:\n  context: my-context\n\nverify:\n  mode: warn\n  failOn: high\n\noutput:\n  format: table\n  report: \"-\"\nYAML\n\nktl verify verify-namespace.yaml",
		"# Verify a chart render via YAML config (pure render)\ncat > verify-chart-render.yaml <<'YAML'\nversion: v1\n\ntarget:\n  kind: chart\n  chart:\n    chart: ./chart\n    release: foo\n    namespace: default\n    values:\n      - values.yaml\n    set:\n      - image.tag=dev\n    useCluster: false\n    includeCRDs: false\n\nverify:\n  mode: block\n  failOn: high\n\noutput:\n  format: table\n  report: \"-\"\nYAML\n\nktl verify verify-chart-render.yaml",
		"# Verify a chart render via YAML config (with cluster lookups)\ncat > verify-chart-cluster.yaml <<'YAML'\nversion: v1\n\ntarget:\n  kind: chart\n  chart:\n    chart: ./chart\n    release: foo\n    namespace: default\n    useCluster: true\n\nkube:\n  context: my-context\n\nverify:\n  mode: block\n  failOn: high\n\noutput:\n  format: table\n  report: \"-\"\nYAML\n\nktl verify verify-chart-cluster.yaml",
		"# Verify rendered manifests from a file\ncat > verify-manifest.yaml <<'YAML'\nversion: v1\n\ntarget:\n  kind: manifest\n  manifest: ./rendered.yaml\n\nverify:\n  mode: block\n  failOn: high\n\noutput:\n  format: table\n  report: \"-\"\nYAML\n\nktl verify verify-manifest.yaml",
	},
}
