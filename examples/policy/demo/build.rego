package ktl.build

default deny := []
default warn := []

deny[v] {
	some base in input.bases
	not is_pinned(base)
	not allowed_unpinned(base)
	v := {"code": "UNPINNED_BASE", "message": "base image must be pinned with @sha256 digest", "subject": base, "path": "dockerfile"}
}

deny[v] {
	some base in input.bases
	blocked_registry(base)
	v := {"code": "BLOCKED_REGISTRY", "message": "base image comes from blocked registry", "subject": base, "path": "dockerfile"}
}

deny[v] {
	some required in input.data.required_labels
	not input.labels[required]
	v := {"code": "MISSING_LABEL", "message": sprintf("missing required label %q", [required]), "subject": required, "path": "dockerfile"}
}

warn[v] {
	count(input.files) == 0
	v := {"code": "NO_ATTEST_DIR", "message": "no attestation/policy artifacts found (set --attest-dir for CI evidence)", "path": "attest"}
}

is_pinned(ref) {
	contains(ref, "@sha256:")
}

allowed_unpinned(ref) {
	some allowed in input.data.allow_unpinned_bases
	startswith(ref, allowed)
}

blocked_registry(ref) {
	some blocked in input.data.blocked_registries
	startswith(ref, blocked)
}

