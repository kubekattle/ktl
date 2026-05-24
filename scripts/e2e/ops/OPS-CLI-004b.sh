#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export OPS_HOST_001_TASK_ID="OPS-CLI-004b"
exec "${script_dir}/OPS-HOST-001.sh" "$@"
