package helpui

// curatedExamples supplements Cobra's .Example fields with task-based golden paths.
// Keys are Cobra command paths (CommandPath()).
var curatedExamples = map[string][]string{
	"ktl logs": {
		"# Tail pods matching a regex in a namespace\nktl logs 'checkout-.*' -n prod-payments",
		"# Highlight errors\nktl logs 'checkout-.*' -n prod-payments --highlight ERROR",
	},
	"ktl init": {
		"# Create a repo-local .ktl.yaml\nktl init",
		"# Preview the config without writing\nktl init --dry-run",
		"# Run the interactive wizard\nktl init --interactive",
		"# Use an opinionated preset\nktl init --preset prod",
		"# Merge defaults into an existing config\nktl init --merge",
		"# Scaffold chart/ and values/ plus gitignore\nktl init --layout --gitignore",
		"# Scaffold Vault-backed secrets\nktl init --secrets-provider vault",
		"# Emit JSON for automation\nktl init --output json --dry-run",
		"# Initialize another path\nktl init ./services/api",
		"# Overwrite existing config\nktl init --force",
	},
	"ktl build": {
		"# Build an image from a directory\nktl build --context . --tag ghcr.io/acme/app:dev",
		"# Share the build stream over WebSocket\nktl build --context . --ws-listen :9085",
	},
	"ktl help": {
		"# Launch the interactive help UI\nktl help --ui",
		"# Show help for a specific command\nktl help apply",
	},
	"ktl apply plan": {
		"# Preview a Helm upgrade\nktl apply plan --chart ./chart --release foo -n default",
		"# Render a shareable HTML visualization\nktl apply plan --visualize --chart ./chart --release foo -n default",
		"# Preview with secret references\nktl apply plan --chart ./chart --release foo -n default --secret-provider local",
		"# Preview with Vault-backed secrets\nktl apply plan --chart ./chart --release foo -n default --secret-provider vault",
		"# Compare against a saved baseline\nktl apply plan --chart ./chart --release foo -n default --compare-to ./plan.json",
		"# Write a baseline snapshot\nktl apply plan --chart ./chart --release foo -n default --baseline ./plan.json",
	},
	"ktl apply": {
		"# Deploy a chart\nktl apply --chart ./chart --release foo -n default",
		"# Run the deploy viewer\nktl apply --chart ./chart --release foo -n default --ui",
		"# Deploy with secret references\nktl apply --chart ./chart --release foo -n default --secret-provider local",
		"# Deploy with Vault-backed secrets\nktl apply --chart ./chart --release foo -n default --secret-provider vault",
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
	"ktl secrets": {
		"# Validate a secret reference\nktl secrets test --secret-provider vault --ref secret://vault/app/db#password",
		"# List secrets under a provider prefix\nktl secrets list --secret-provider local --path app --format json",
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
		"# Apply with secret references\nktl stack apply --config ./stacks/prod --secret-provider vault --yes",
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
	"verify": {
		"# Verify a chart render (inline)\nverify --chart ./chart --release foo -n default",
		"# Verify a chart render with cluster lookups\nverify --chart ./chart --release foo -n default --use-cluster --context my-context",
		"# Verify a live namespace\nverify --namespace default --context my-context",
		"# Generate a starter config for scripting\nverify init --chart ./chart --release foo -n default --write verify.yaml\nverify verify.yaml",
		"# Run the bundled verify showcase (includes a CRITICAL rule)\nverify testdata/verify/showcase/verify.yaml",
		"# Compare against a baseline report\nverify verify.yaml --compare-to ./baseline.json",
		"# Write a baseline report\nverify verify.yaml --baseline ./baseline.json",
	},
	"package": {
		"# Package a chart directory\npackage ./chart --output dist/chart.sqlite",
		"# Verify an existing archive\npackage --verify dist/chart.sqlite",
		"# Package then verify (quiet with SHA)\npackage ./chart --output dist/chart.sqlite --print-sha --quiet && package --verify dist/chart.sqlite",
		"# Stream an archive over ssh\npackage ./chart --output - | ssh host \"cat > chart.sqlite\"",
		"# Unpack an archive into a directory\npackage --unpack dist/chart.sqlite --destination ./chart-unpacked",
	},
}
