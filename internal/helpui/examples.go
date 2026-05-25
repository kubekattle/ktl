package helpui

// curatedExamples supplements Cobra's .Example fields with task-based golden paths.
// Keys are Cobra command paths (CommandPath()).
var curatedExamples = map[string][]string{
	"torque": {
		"# Launch the built-in interactive help UI\ntorque --help --ui",
		"# Run the MCP bridge over stdio for an agent host\ntorque-mcp --stdio",
		"# Bridge MCP calls to a remote torque-agent over gRPC\ntorque-mcp --stdio --remote-agent 127.0.0.1:7443 --remote-token \"$TORQUE_REMOTE_TOKEN\"",
		"# Bridge MCP calls to an mTLS-protected torque-agent\ntorque-mcp --stdio --remote-agent torque-agent.prod.internal:7443 --remote-tls --remote-tls-ca /etc/torque/tls/ca.crt --remote-tls-client-cert /etc/torque/tls/client.crt --remote-tls-client-key /etc/torque/tls/client.key --remote-tls-server-name torque-agent.prod.internal --enable-write",
		"# Ask MCP for a structured S3 cache plan instead of scraping BuildKit logs\nprintf '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"torque.cache.plan\",\"arguments\":{\"contextDir\":\".\",\"tags\":[\"ghcr.io/acme/app:dev\"],\"changedPaths\":[\"go.mod\"],\"s3Cache\":\"s3://acme-build-cache/torque/main\",\"s3CacheRegion\":\"us-east-1\"}}}\\n' | torque-mcp --stdio",
		"# Install durable gRPC and MCP services on a Linux systemd host\ncurl -fsSL https://ingresslabs.github.io/torque/install.sh | sh -s -- --mode systemd-daemon",
	},
	"torque logs": {
		"# Tail pods matching a regex in a namespace\ntorque logs 'checkout-.*' -n prod-payments",
		"# Highlight errors\ntorque logs 'checkout-.*' -n prod-payments --highlight ERROR",
	},
	"torque init": {
		"# Create a repo-local .torque.yaml\ntorque init",
		"# Preview the config without writing\ntorque init --dry-run",
		"# Run the interactive wizard\ntorque init --interactive",
		"# Use an opinionated preset\ntorque init --preset prod",
		"# Apply a built-in template\ntorque init --template platform",
		"# Apply a template from a URL\ntorque init --template https://example.com/torque-init.yaml",
		"# Merge defaults into an existing config\ntorque init --merge",
		"# Scaffold chart/ and values/ plus gitignore\ntorque init --layout --gitignore",
		"# Scaffold Vault-backed secrets\ntorque init --secrets-provider vault",
		"# Emit JSON for automation\ntorque init --output json --dry-run",
		"# Write a replayable init plan\ntorque init --plan --plan-output .torque/init-plan.json",
		"# Apply a saved init plan\ntorque init --apply-plan .torque/init-plan.json",
		"# Initialize another path\ntorque init ./services/api",
		"# Overwrite existing config\ntorque init --force",
	},
	"torque init from-cluster": {
		"# Generate stack.yaml from the active namespace\ntorque init from-cluster",
		"# Preview adoption across all non-system namespaces\ntorque init from-cluster --all-namespaces --dry-run",
		"# Export installed charts and current Helm values\ntorque init from-cluster --all-namespaces --write-values",
		"# Adopt a specific namespace into another file\ntorque init from-cluster -n prod --output stacks/prod.yaml",
	},
	"torque build": {
		"# Build an image from a directory\ntorque build . --tag ghcr.io/acme/app:dev",
		"# Build with a shared S3 cache\ntorque build . --tag ghcr.io/acme/app:dev --s3-cache s3://acme-build-cache/torque/main --s3-cache-region us-east-1",
		"# Capture build provenance for apply plan\ntorque build . --tag ghcr.io/acme/app:dev --capture ./build.sqlite",
		"# Share the build stream over WebSocket\ntorque build . --ws-listen :9085",
	},
	"torque explain": {
		"# Explain a deploy capture\ntorque explain ./apply.sqlite",
		"# Explain a stack capture as JSON\ntorque explain ./stack.sqlite --format json",
		"# Write a Markdown explanation for CI logs or PR comments\ntorque explain ./apply.sqlite --format markdown",
		"# Explain a specific captured session\ntorque explain ./apply.sqlite --session <session-id>",
	},
	"torque help": {
		"# Launch the interactive help UI\ntorque --help --ui",
		"# Launch the help subcommand UI directly\ntorque help --ui",
		"# Show help for a specific command\ntorque help apply",
	},
	"torque ship": {
		"# Build, verify, plan, apply, capture, and explain one release\ntorque ship --chart ./chart --release api -n prod --build . --tag ghcr.io/acme/api:dev --yes",
		"# Ship with a shared S3 BuildKit cache\ntorque ship --chart ./chart --release api -n prod --build . --tag ghcr.io/acme/api:dev --s3-cache s3://acme-build-cache/torque/main --s3-cache-region us-east-1 --yes",
		"# Ship through a remote torque-agent gRPC endpoint\ntorque --remote-agent 127.0.0.1:7443 --remote-token \"$TORQUE_REMOTE_TOKEN\" ship --chart ./chart --release api -n prod --build . --tag ghcr.io/acme/api:dev --yes",
		"# Ship through an mTLS-protected torque-agent\ntorque --remote-agent torque-agent.prod.internal:7443 --remote-token \"$TORQUE_REMOTE_TOKEN\" --remote-tls --remote-tls-ca /etc/torque/tls/ca.crt --remote-tls-client-cert /etc/torque/tls/client.crt --remote-tls-client-key /etc/torque/tls/client.key ship --chart ./chart --release api -n prod --build . --tag ghcr.io/acme/api:dev --yes",
	},
	"torque apply plan": {
		"# Preview a Helm upgrade\ntorque apply plan --chart ./chart --release foo -n default",
		"# Write a GitHub PR comment summary\ntorque apply plan --chart ./chart --release foo -n default --github-comment --output plan.md",
		"# Attach verifier and build evidence\ntorque apply plan --chart ./chart --release foo -n default --verify-report verify.json --build-capture ./build.sqlite --github-comment",
		"# Render a shareable HTML visualization\ntorque apply plan --visualize --chart ./chart --release foo -n default",
		"# Preview with secret references\ntorque apply plan --chart ./chart --release foo -n default --secret-provider local",
		"# Preview with Vault-backed secrets\ntorque apply plan --chart ./chart --release foo -n default --secret-provider vault",
		"# Compare against a saved baseline\ntorque apply plan --chart ./chart --release foo -n default --compare-to ./plan.json",
		"# Write a baseline snapshot\ntorque apply plan --chart ./chart --release foo -n default --baseline ./plan.json",
	},
	"torque apply simulate": {
		"# Simulate a Helm apply and write a proof bundle\ntorque apply simulate --chart ./chart --release foo -n default --out ./torque-sim-proof",
		"# Attach security evidence and SLO gates\ntorque apply simulate --chart ./chart --release foo -n default --security-evidence ./torque-security-evidence --slo ./slo.yaml --out ./torque-sim-proof",
		"# Fail CI when the API dry-run or quota proof blocks the release\ntorque apply simulate --chart ./chart --release foo -n default --out ./torque-sim-proof --fail-on-blocked",
	},
	"torque apply": {
		"# Deploy a chart\ntorque apply --chart ./chart --release foo -n default",
		"# Run the deploy viewer\ntorque apply --chart ./chart --release foo -n default --ui",
		"# Predict rollout risk and write a proof bundle\ntorque apply --chart ./chart --release foo -n default --predict --proof-bundle ./apply-proof.json --capture ./apply.sqlite --yes",
		"# Roll back automatically and write proof if apply or SLO gates fail\ntorque apply --chart ./chart --release foo -n default --auto-rollback --slo ./slo.yaml --capture ./apply.sqlite --yes",
		"# Deploy with secret references\ntorque apply --chart ./chart --release foo -n default --secret-provider local",
		"# Deploy with Vault-backed secrets\ntorque apply --chart ./chart --release foo -n default --secret-provider vault",
	},
	"torque delete": {
		"# Delete a release\ntorque delete --release foo -n default",
		"# Run the destroy viewer\ntorque delete --release foo -n default --ui",
	},
	"torque revert": {
		"# Revert a release to the last known-good revision\ntorque revert --release foo -n default",
	},
	"torque repair": {
		"# Diagnose a failed apply proof bundle\ntorque repair --from ./apply-proof.json --chart ./chart",
		"# Diagnose a simulation proof bundle through the fix alias\ntorque fix --from ./torque-sim-proof --chart ./chart",
		"# Write chart repair files and a PR body\ntorque repair --from ./apply-proof.json --chart ./chart --branch fix/foo-rollout --apply --pr-body ./repair-pr.md --yes",
	},
	"torque replay": {
		"# Validate a simulation proof bundle in the k3s lab profile\ntorque replay ./torque-sim-proof --lab k3s",
		"# Emit replay validation as JSON\ntorque replay ./torque-sim-proof --lab k3s --format json",
		"# Fail CI when the replayed proof is blocked\ntorque replay ./torque-sim-proof --lab k3s --fail-on-blocked",
	},
	"torque proof": {
		"# Build a release proof graph and browser report\ntorque proof graph ./apply-proof.json --out proof.graph.json --html proof.html",
		"# Sign a proof graph with an ed25519 key\ntorque proof graph ./apply-proof.json --out proof.graph.json --key .torque/stack/keys/ed25519.json",
		"# Verify a signed graph in CI\ntorque proof verify proof.graph.json --require-signature",
		"# Block promotion when release evidence is incomplete\ntorque proof gate proof.graph.json --out proof.gate.json",
		"# Sign a compact release verdict for PRs and release notes\ntorque proof attest proof.graph.json --release v1.0.9 --key .torque/stack/keys/ed25519.json --out release.attestation.json",
	},
	"torque proof graph": {
		"# Link apply proof, Guardian drift, and repair evidence\ntorque proof graph ./apply-proof.json --attach drift-proof.json --attach repair-pr.md --out proof.graph.json --html proof.html",
		"# Sign the graph using a key generated by torque stack keygen\ntorque proof graph ./torque-sim-proof --out proof.graph.json --key .torque/stack/keys/ed25519.json",
	},
	"torque proof verify": {
		"# Verify hashes and the embedded signature\ntorque proof verify proof.graph.json --require-signature",
		"# Verify with an explicit trusted public key\ntorque proof verify proof.graph.json --pub .torque/stack/keys/ed25519.json --require-signature",
	},
	"torque proof diff": {
		"# Compare evidence between two releases\ntorque proof diff previous-proof.graph.json current-proof.graph.json",
		"# Emit diff as JSON for CI\ntorque proof diff previous-proof.graph.json current-proof.graph.json --format json",
	},
	"torque proof gate": {
		"# Evaluate the built-in release gate\ntorque proof gate proof.graph.json --out proof.gate.json",
		"# Use a custom release policy and emit JSON\ntorque proof gate proof.graph.json --policy release-policy.yaml --format json",
	},
	"torque proof attest": {
		"# Sign a pasteable release verdict\ntorque proof attest proof.graph.json --release v1.0.9 --key .torque/stack/keys/ed25519.json --out release.attestation.json",
		"# Emit the signed verdict as JSON for CI\ntorque proof attest proof.graph.json --release v1.0.9 --key .torque/stack/keys/ed25519.json --format json",
	},
	"torque agent": {
		"# Check whether an agent may run a mutating operation\ntorque agent policy check agent-request.json --proof proof.graph.json --allow apply --require-gate",
		"# Write a proof-backed authorization record\ntorque agent run agent-request.json --proof proof.graph.json --allow apply --require-gate --out agent-run.json",
	},
	"torque agent policy": {
		"# Evaluate proof-backed operation policy\ntorque agent policy check agent-request.json --proof proof.graph.json --allow apply --require-gate --out agent-policy.json",
	},
	"torque agent policy check": {
		"# Authorize an apply request only when the release gate passes\ntorque agent policy check agent-request.json --proof proof.graph.json --allow apply --require-gate --format json",
		"# Use a custom release policy for agent authorization\ntorque agent policy check agent-request.json --proof proof.graph.json --policy release-policy.yaml --allow apply --require-gate",
	},
	"torque agent run": {
		"# Produce a non-mutating authorization record for the caller\ntorque agent run agent-request.json --proof proof.graph.json --allow apply --require-gate --out agent-run.json",
		"# Build the request from flags when no request file exists\ntorque agent run --actor codex --operation apply --command 'torque apply --chart ./chart --release api -n prod' --proof proof.graph.json --allow apply --require-gate",
	},
	"torque ops": {
		"# Show a proof-safe target inventory\ntorque ops inventory show --targets ./targetgraph.yaml",
		"# Collect cached host facts for selected targets\ntorque ops facts collect --targets ./targetgraph.yaml --selector role=web --cache-dir ./.torque/ops/facts",
		"# Check guarded mutation policy before adding an adapter\ntorque ops policy check --mode guarded --operation host.command.run --mutating --format json",
		"# Discover adapter capability contracts\ntorque ops adapter capabilities --format json",
	},
	"torque ops inventory": {
		"# Show targets selected by a label\ntorque ops inventory show --targets ./targetgraph.yaml --selector role=db",
		"# Export an HTML inventory graph\ntorque ops inventory graph --targets ./targetgraph.yaml --output inventory.html",
		"# Snapshot a Git-backed TargetGraph source\ntorque ops inventory snapshot --source ./repo --type git --path targetgraph.yaml --ref HEAD --format json",
	},
	"torque ops inventory show": {
		"# Show a proof-safe target inventory\ntorque ops inventory show --targets ./targetgraph.yaml",
		"# Emit selected targets as JSON\ntorque ops inventory show --targets ./targetgraph.yaml --selector role=db --format json",
		"# Limit a group-expanded inventory view\ntorque ops inventory show --targets ./targetgraph.yaml --group web --limit 10",
	},
	"torque ops inventory graph": {
		"# Export an HTML target graph view\ntorque ops inventory graph --targets ./targetgraph.yaml --output inventory.html",
		"# Emit graph data as JSON for automation\ntorque ops inventory graph --targets ./targetgraph.yaml --selector role=db --format json",
		"# Mark a limited group expansion in the graph\ntorque ops inventory graph --targets ./targetgraph.yaml --group web --limit 10 --output web.html",
	},
	"torque ops inventory snapshot": {
		"# Snapshot a local TargetGraph file\ntorque ops inventory snapshot --source ./targetgraph.yaml --type file --format json",
		"# Snapshot a generated TargetGraph without leaking source contents\ntorque ops inventory snapshot --source ./render-inventory.sh --type script --format json",
		"# Snapshot an HTTP or Git inventory source\ntorque ops inventory snapshot --source https://example.invalid/targetgraph.yaml --type http --format json",
	},
	"torque ops facts": {
		"# Collect host or Kubernetes facts from selected targets\ntorque ops facts collect --targets ./targetgraph.yaml --selector role=web --format json",
		"# Compare two fact collections\ntorque ops facts diff --from ./facts-before.json --to ./facts-after.json",
	},
	"torque ops facts collect": {
		"# Collect and cache selected target facts with evidence\ntorque ops facts collect --targets ./targetgraph.yaml --group web --cache-dir ./.torque/ops/facts --out-dir ./ops-facts-evidence",
		"# Collect namespace-scoped Kubernetes facts\ntorque ops facts collect --targets ./targetgraph.yaml --selector role=cluster --namespace prod --format json",
		"# Inspect cached facts without refreshing\ntorque ops facts collect --targets ./targetgraph.yaml --cache-dir ./.torque/ops/facts --cache-only",
	},
	"torque ops facts diff": {
		"# Show changed fact categories between two runs\ntorque ops facts diff --from ./facts-before.json --to ./facts-after.json",
		"# Emit a machine-readable fact diff\ntorque ops facts diff --from ./facts-before.json --to ./facts-after.json --format json",
	},
	"torque ops lock": {
		"# Acquire a target lock before a planned mutation\ntorque ops lock acquire --scope target/host-01 --holder operator --operation host.command.run",
		"# Inspect an existing lock\ntorque ops lock status --scope target/host-01 --format json",
	},
	"torque ops lock acquire": {
		"# Acquire a target lock with a TTL\ntorque ops lock acquire --scope target/host-01 --holder operator --operation host.command.run --ttl 15m --format json",
		"# Wait briefly for an existing target lock\ntorque ops lock acquire --scope target/host-01 --holder operator --wait 5s",
	},
	"torque ops lock release": {
		"# Release a target lock by token\ntorque ops lock release --scope target/host-01 --token <token>",
	},
	"torque ops lock status": {
		"# Show a target lock as JSON\ntorque ops lock status --scope target/host-01 --format json",
	},
	"torque ops policy": {
		"# Check an observe-only policy blocks mutation\ntorque ops policy check --mode observe-only --operation host.command.run --mutating --format json",
		"# Check an approved guarded mutation\ntorque ops policy check --mode guarded --operation host.command.run --mutating --approved",
	},
	"torque ops policy check": {
		"# Block an unsafe automatic operation\ntorque ops policy check --mode automatic --operation host.command.run --mutating --unsafe --format json",
		"# Allow an explicit unsafe local experiment\ntorque ops policy check --mode unsafe --operation host.command.run --mutating --unsafe --allow-unsafe --local-experiment",
	},
	"torque ops adapter": {
		"# List adapter capability contracts\ntorque ops adapter capabilities",
		"# Probe the implemented host command adapter locally\ntorque ops adapter capabilities host.command.run --target local://localhost --format json",
	},
	"torque ops adapter capabilities": {
		"# List implemented and planned adapter contracts\ntorque ops adapter capabilities --format json",
		"# Show a single adapter contract\ntorque ops adapter capabilities host.command.run",
		"# Probe an SSH target and emit redacted JSON evidence\ntorque ops adapter capabilities host.command.run --target ssh://root@lab-host --format json",
	},
	"torque release": {
		"# Run the proof-backed release autopilot over existing evidence\ntorque release autopilot proof.graph.json --key .torque/stack/keys/ed25519.json --out-dir release-autopilot",
		"# Promote through a proof-backed canary ladder\ntorque release promote proof.graph.json --strategy canary --steps 5,25,50,100 --slo slo.yaml --rollback-on-fail",
		"# Promote through proof-backed blue/green preview and switch\ntorque release promote proof.graph.json --strategy blue-green --preview --smoke smoke.json --switch-traffic",
		"# Score release readiness from proof evidence\ntorque release score proof.graph.json --out release-score.json",
	},
	"torque release autopilot": {
		"# Compose graph, gate, score, flight, agent authorization, and attestation artifacts\ntorque release autopilot proof.graph.json --key .torque/stack/keys/ed25519.json --fail-below 90 --out-dir release-autopilot",
		"# Run torque apply first, then collect proof-backed release artifacts\ntorque release autopilot --execute --yes --chart ./chart --release api -n prod --auto-rollback --slo slo.yaml --key .torque/stack/keys/ed25519.json",
	},
	"torque release promote": {
		"# Canary promotion with signed proof evidence\ntorque release promote proof.graph.json --strategy canary --steps 5,25,50,100 --slo slo.yaml --rollback-on-fail --key .torque/stack/keys/ed25519.json --out-dir release-promote-canary",
		"# Blue/green promotion with smoke evidence\ntorque release promote proof.graph.json --strategy blue-green --preview --smoke smoke.json --switch-traffic --key .torque/stack/keys/ed25519.json --out-dir release-promote-blue-green",
		"# Deterministic E2E traffic-state provider\ntorque release promote proof.graph.json --strategy blue-green --preview --smoke smoke.json --switch-traffic --provider file --state-out traffic-state.json --execute --yes",
		"# Real Argo Rollouts canary provider\ntorque release promote proof.graph.json --strategy canary --steps 5,25,50,100 --provider argo-rollouts --rollout api --execute --yes",
		"# Native Kubernetes blue/green provider\ntorque release promote proof.graph.json --strategy blue-green --preview --switch-traffic --provider kubernetes --active-service api --preview-service api-preview --blue-deployment api-blue --green-deployment api-green --execute --yes",
	},
	"torque release score": {
		"# Fail CI when a release score is below the promotion threshold\ntorque release score proof.graph.json --fail-below 90",
		"# Score with an explicit trusted proof key\ntorque release score proof.graph.json --pub .torque/stack/keys/ed25519.json --out release-score.json --format json",
	},
	"torque flight": {
		"# Record, replay, and explain a release flight\ntorque flight record proof.graph.json --out release.flight.torque && torque flight replay release.flight.torque && torque flight explain release.flight.torque",
	},
	"torque flight record": {
		"# Record a portable release timeline from the proof graph\ntorque flight record proof.graph.json --out release.flight.torque",
		"# Record a flight with a custom release gate policy\ntorque flight record proof.graph.json --policy release-policy.yaml --out release.flight.torque --format json",
	},
	"torque flight replay": {
		"# Validate that a release flight is replayable\ntorque flight replay release.flight.torque --format json",
	},
	"torque flight explain": {
		"# Summarize the evidence phases in a recorded release flight\ntorque flight explain release.flight.torque",
	},
	"torque guardian": {
		"# Install observe-only Guardian RBAC and config\ntorque guardian install --namespace torque-system --mode observe",
		"# Compare a simulation proof against live runtime state\ntorque guardian diff --source ./torque-sim-proof --live --out drift-proof.json",
		"# Generate PR artifacts from a drift proof\ntorque guardian pr --from drift-proof.json --branch fix/runtime-drift",
	},
	"torque guardian install": {
		"# Install observe-only Guardian RBAC\ntorque guardian install --namespace torque-system --mode observe",
		"# Review the manifest without applying it\ntorque guardian install --namespace torque-system --mode observe --dry-run",
	},
	"torque guardian report": {
		"# Write a 24-hour runtime event proof\ntorque guardian report --since 24h --out runtime-proof.json",
		"# Inspect all namespaces and print JSON\ntorque guardian report --since 30m --all-namespaces --format json",
	},
	"torque guardian diff": {
		"# Prove drift from simulation proof to live objects\ntorque guardian diff --source ./torque-sim-proof --live --out drift-proof.json",
		"# Write the full runtime proof bundle directory\ntorque guardian diff --source ./torque-sim-proof --live --out ./torque-runtime-proof",
	},
	"torque guardian pr": {
		"# Generate patch and PR body from a drift proof\ntorque guardian pr --from drift-proof.json --branch fix/runtime-drift",
		"# Generate fix artifacts beside a runtime proof bundle\ntorque guardian pr --from ./torque-runtime-proof",
	},
	"torque contract": {
		"# Synthesize recurrence rules from Incident and Guardian proof\ntorque contract synthesize --from incident-replay-proof --guardian drift-proof.json --out torque-contract.yaml",
		"# Test fresh proof against the RuntimeContract\ntorque contract test --contract torque-contract.yaml --from incident-replay-proof --guardian drift-proof.json --out contract-proof.json",
		"# Generate PR artifacts from contract proof\ntorque contract pr --contract torque-contract.yaml --proof contract-proof.json --branch add/api-runtime-contract",
	},
	"torque contract synthesize": {
		"# Write a RuntimeContract from combined proof\ntorque contract synthesize --from incident-replay-proof --guardian drift-proof.json --out torque-contract.yaml",
		"# Print the synthesized contract as YAML\ntorque contract synthesize --from incident-replay-proof --guardian drift-proof.json --format yaml",
	},
	"torque contract test": {
		"# Write machine-checkable contract test proof\ntorque contract test --contract torque-contract.yaml --from incident-replay-proof --guardian drift-proof.json --out contract-proof.json",
		"# Fail CI when recurrence rules are violated\ntorque contract test --contract torque-contract.yaml --from incident-replay-proof --guardian drift-proof.json --fail-on-blocked",
	},
	"torque contract pr": {
		"# Generate patch and PR body from contract proof\ntorque contract pr --contract torque-contract.yaml --proof contract-proof.json --branch add/api-runtime-contract",
	},
	"torque incident": {
		"# Capture observe-only incident evidence\ntorque incident capture --release api -n prod --since 1h --out incident.torque",
		"# Replay the capture as a lab proof\ntorque incident replay incident.torque --lab k3s --out incident-replay-proof",
		"# Generate PR artifacts from root cause\ntorque incident pr --from incident-replay-proof --branch fix/api-incident",
	},
	"torque incident capture": {
		"# Capture a release incident bundle\ntorque incident capture --release api -n prod --since 1h --out incident.torque",
		"# Print capture JSON and skip pod logs\ntorque incident capture --release api -n prod --log-tail 0 --format json",
	},
	"torque incident replay": {
		"# Validate an incident bundle in the k3s lab profile\ntorque incident replay incident.torque --lab k3s --out incident-replay-proof",
		"# Fail CI when replayed evidence remains blocked\ntorque incident replay incident.torque --lab k3s --fail-on-blocked",
	},
	"torque incident explain": {
		"# Write root-cause JSON from replay proof\ntorque incident explain --from incident-replay-proof --out root-cause.json",
	},
	"torque incident pr": {
		"# Generate patch and PR body from root cause\ntorque incident pr --from root-cause.json --branch fix/api-incident",
		"# Generate fix artifacts beside replay proof\ntorque incident pr --from incident-replay-proof",
	},
	"torque env": {
		"# Show env var reference (machine-readable)\ntorque env --format json",
	},
	"torque secrets": {
		"# Validate a secret reference\ntorque secrets test --secret-provider vault --ref secret://vault/app/db#password",
		"# List secrets under a provider prefix\ntorque secrets list --secret-provider local --path app --format json",
		"# Discover secret refs across the repo\ntorque secrets discover --scope repo",
		"# Discover secret refs for a chart\ntorque secrets discover --scope chart --chart ./chart --values values/dev.yaml",
		"# Discover secret refs for a stack\ntorque secrets discover --scope stack --config ./stacks/prod",
		"# Scan source files for secret-like values\ntorque secrets scan --scope repo --report secrets.json --mode block",
		"# Scan source files and include a redacted secret flow graph\ntorque secrets scan --scope repo --report secrets.json --mode block --flow-graph",
		"# Scan rendered manifests and allow Kubernetes Secret boundaries\ntorque secrets scan --scope render --manifest ./rendered.yaml --report render-secrets.json --mode block --flow-graph",
	},
	"torque security": {
		"# Benchmark secret detection, redaction, flow graph, and boundary evidence\ntorque security benchmark --corpus ./testdata/security --report benchmark.json",
	},
	"torque security benchmark": {
		"# Publish corpus-backed security metrics\ntorque security benchmark --corpus ./testdata/security --report benchmark.json",
		"# Include the live k3s boundary matrix probe\ntorque security benchmark --corpus ./testdata/security --report benchmark.json --live-k3s-boundary-matrix --live-confirm --kubeconfig ~/.kube/k3s.yaml",
	},
	"torque version": {
		"# Print version information\ntorque version",
	},
	"torque stack": {
		"# Plan the stack (default: read-only, like `torque stack plan`)\ntorque stack --config ./stacks/prod",
		"# Restrict selection via environment defaults\nTORQUE_STACK_TAG=critical TORQUE_STACK_CLUSTER=prod-us torque stack --config ./stacks/prod",
		"# Emit a machine-readable plan for tooling\ntorque stack --config ./stacks/prod --output json",
	},
	"torque stack plan": {
		"# Write a reproducible plan bundle for review/CI\ntorque stack plan --config ./stacks/prod --bundle ./stack-plan.tgz",
		"# Embed a live diff summary in the bundle (requires cluster access)\ntorque stack plan --config ./stacks/prod --bundle ./stack-plan.tgz --bundle-diff-summary",
		"# Attach read-only ops TargetGraph, facts, locks, and policy inputs\ntorque stack plan --config ./stacks/prod --ops-targets ./targetgraph.yaml --ops-facts ./ops-facts-evidence --ops-lock-dir ./.torque/ops/locks --ops-policy-decision ./policy.json --output json",
		"# Seal a guarded host.command.run plan with ops evidence\ntorque stack plan --config ./stacks/host-command --ops-targets ./targetgraph.yaml --ops-facts ./ops-facts --ops-lock-dir ./.torque/ops/locks --ops-policy-decision ./policy.json --bundle ./host-command-plan.tgz",
		"# Plan a host.file.render node with exact digest diff evidence\ntorque stack plan --config ./stacks/host-file-render --output json",
		"# Plan a host.file.copy node with checksum and backup evidence\ntorque stack plan --config ./stacks/host-file-copy --output json",
		"# Plan a host.package.install node with package version evidence\ntorque stack plan --config ./stacks/host-package --output json",
		"# Plan a mixed graph with Helm, scripts, and DB cutover nodes\ntorque stack plan --config ./stacks/db-cutover --output json",
		"# Plan typed adapter nodes that behave like evidence-backed automation modules\ntorque stack plan --config ./stacks/ops-program --output json",
		"# Plan Kubernetes lifecycle inspect, policy-gated maintenance, verification, and summary evidence nodes\ntorque stack plan --config ./stacks/cluster-lifecycle --output json",
		"# Plan a full DB change program with restore/backfill/cutover nodes\ntorque stack plan --config ./stacks/db-program --output json",
		"# Plan the Oracle/APEX -> PostgreSQL target cutover showcase\ntorque stack plan --config ./docs/showcase/oracle-postgres-k8s/stack.postgres.yaml --output json",
	},
	"torque stack graph": {
		"# Render a Graphviz DOT graph\ntorque stack graph --config ./stacks/prod > stack.dot",
		"# Render a Mermaid graph\ntorque stack graph --config ./stacks/prod --format mermaid > stack.mmd",
	},
	"torque stack explain": {
		"# Explain why a stack node is selected (by name)\ntorque stack explain --config ./stacks/prod api",
		"# Print only selection reasons\ntorque stack explain --config ./stacks/prod api --why",
	},
	"torque stack apply": {
		"# Apply the selected stack nodes (DAG order)\ntorque stack apply --config ./stacks/prod --yes",
		"# Capture a stack run evidence bundle\ntorque stack apply --config ./stacks/prod --yes --capture ./stack.sqlite",
		"# Apply a sealed ops plan only after the ops preflight gate passes\ntorque stack apply --from-bundle ./stack-plan.tgz --yes",
		"# Replay an approved ops plan and re-check frozen evidence before mutation\ntorque stack apply --from-bundle ./host-command-plan.tgz --yes",
		"# Execute a guarded host.command.run from a sealed ops plan\ntorque stack apply --from-bundle ./host-command-plan.tgz --yes",
		"# Render a host-side config file with validation and no-op evidence\ntorque stack apply --config ./stacks/host-file-render --yes",
		"# Copy a host-side file with checksum, backup, and restore evidence\ntorque stack apply --config ./stacks/host-file-copy --yes",
		"# Install a host package with before/after version receipts\ntorque stack apply --config ./stacks/host-package --yes",
		"# Resume the most recent run (uses stored frozen plan unless --replan is set)\ntorque stack apply --config ./stacks/prod --resume --yes",
		"# Apply a mixed graph with PostgreSQL or MariaDB cutover nodes\ntorque stack apply --config ./stacks/db-cutover --yes",
		"# Run evidence-backed action plugins inside the stack DAG\ntorque stack apply --config ./stacks/ops-program --yes",
		"# Inspect topology, apply lifecycle policy gates, derive cert targets, verify, and export summary evidence\ntorque stack apply --config ./stacks/cluster-lifecycle --yes",
		"# Apply an approved lifecycle policy override with scoped approval evidence\ntorque stack apply --config ./stacks/cluster-lifecycle --yes --policy-override",
		"# Run the Oracle/APEX -> PostgreSQL local proof harness\ntorque stack apply --config ./docs/showcase/oracle-postgres-k8s/stack.sqlite.yaml --yes",
		"# Enable manifest diffs (defaulted via env)\nTORQUE_STACK_APPLY_DIFF=1 torque stack apply --config ./stacks/prod --yes",
		"# Apply with secret references\ntorque stack apply --config ./stacks/prod --secret-provider vault --yes",
	},
	"torque stack delete": {
		"# Delete the selected stack nodes (reverse DAG order)\ntorque stack delete --config ./stacks/prod --yes",
		"# Prompt only when deleting 50+ stack nodes\ntorque stack delete --config ./stacks/prod --delete-confirm-threshold 50",
	},
	"torque stack status": {
		"# Tail the most recent run\ntorque stack status --config ./stacks/prod --follow",
		"# Show a specific run ID (see `torque stack runs`)\ntorque stack status --config ./stacks/prod --run-id 2025-12-30T12-34-56.000000000Z --follow",
	},
	"torque stack runs": {
		"# List recent runs\ntorque stack runs --config ./stacks/prod --limit 50",
	},
	"torque stack audit": {
		"# Show audit table for the most recent run\ntorque stack audit --config ./stacks/prod",
		"# Export a shareable HTML report\ntorque stack audit --config ./stacks/prod --output html > stack-audit.html",
		"# Verify a portable ops run bundle without mutating\ntorque stack audit --from-bundle ./host-command-run.tgz --output json --include-artifacts",
		"# Inspect stored DB node artifacts from the run ledger\ntorque stack audit --config ./stacks/db-program --output json --include-artifacts > audit.json",
		"# Inspect Oracle/APEX -> PostgreSQL receipts and cutover artifacts\ntorque stack audit --config ./docs/showcase/oracle-postgres-k8s/stack.sqlite.yaml --output json --include-artifacts > oracle-postgres-audit.json",
	},
	"torque stack export": {
		"# Export the most recent run as a portable bundle\ntorque stack export --config ./stacks/prod",
		"# Export a specific run ID\ntorque stack export --config ./stacks/prod --run-id 2025-12-30T12-34-56.000000000Z --out ./exports/run.tgz",
		"# Audit an exported bundle on another machine without local stack state\ntorque stack audit --from-bundle ./exports/run.tgz --output json --include-artifacts",
		"# Export the DB program run ledger and artifacts as a portable bundle\ntorque stack export --config ./stacks/db-program --out ./db-program-export.tgz",
		"# Export the Oracle/APEX -> PostgreSQL showcase run ledger\ntorque stack export --config ./docs/showcase/oracle-postgres-k8s/stack.sqlite.yaml --out ./oracle-postgres-run.tgz",
	},
	"torque stack seal": {
		"# Seal a plan directory for CI (includes inputs bundle by default)\ntorque stack seal --config ./stacks/prod --out ./.torque/stack/sealed --command apply",
		"# Seal without bundling inputs\ntorque stack seal --config ./stacks/prod --out ./.torque/stack/sealed --bundle=false --command apply",
	},
	"torque stack rerun-failed": {
		"# Resume the most recent run and schedule only failed nodes\ntorque stack rerun-failed --config ./stacks/prod --yes",
	},
	"verifier": {
		"# Verify a chart render (inline)\nverifier --chart ./chart --release foo -n default",
		"# Verify with evidence-first secret flow checks\nverifier --chart ./chart --release foo -n default --security-profile enterprise --security-boundary-matrix --secret-flow-graph --secrets-report secrets.json --security-evidence ./torque-security-evidence --format json --report verify.json",
		"# Verify a chart render with cluster lookups\nverifier --chart ./chart --release foo -n default --use-cluster --context my-context",
		"# Verify a live namespace\nverifier --namespace default --context my-context",
		"# Discover builtin rules\nverifier rules list\nverifier rules show k8s/container_is_privileged",
		"# Generate a starter config for scripting\nverifier init --chart ./chart --release foo -n default --write verify.yaml\nverifier verify.yaml",
		"# Run the bundled verify showcase (includes a CRITICAL rule)\nverifier testdata/verify/showcase/verify.yaml",
		"# Compare against a baseline report\nverifier verify.yaml --compare-to ./baseline.json",
		"# Write a baseline report\nverifier verify.yaml --baseline ./baseline.json",
		"# HTML report\nverifier --manifest ./rendered.yaml --format html --report ./verify-report.html --open",
		"# Print a suggested fix plan (table output only)\nverifier verify.yaml --fix",
	},
	"torque-package": {
		"# Package a chart directory\ntorque-package ./chart --output dist/chart.sqlite",
		"# Verify an existing archive\ntorque-package --verify dist/chart.sqlite",
		"# Package then verify (quiet with SHA)\ntorque-package ./chart --output dist/chart.sqlite --print-sha --quiet && torque-package --verify dist/chart.sqlite",
		"# Stream an archive over ssh\ntorque-package ./chart --output - | ssh host \"cat > chart.sqlite\"",
		"# Unpack an archive into a directory\ntorque-package --unpack dist/chart.sqlite --destination ./chart-unpacked",
	},
}
