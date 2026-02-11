#!/bin/sh
set -eu

state_dir="${HOOK_STATE_DIR:?HOOK_STATE_DIR is required}"
mkdir -p "$state_dir"
marker="$state_dir/stack-pre-flaky.marker"

if [ ! -f "$marker" ]; then
  echo "flaky: first attempt failing"
  touch "$marker"
  exit 1
fi

echo "flaky: second attempt ok"
