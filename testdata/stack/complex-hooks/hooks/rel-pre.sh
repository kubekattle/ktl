#!/bin/sh
set -eu

state_dir="${HOOK_STATE_DIR:?HOOK_STATE_DIR is required}"
mkdir -p "$state_dir"
touch "$state_dir/rel-pre-${KTL_RELEASE_NAME:-unknown}"
echo "rel-pre ok release=${KTL_RELEASE_NAME:-}"
