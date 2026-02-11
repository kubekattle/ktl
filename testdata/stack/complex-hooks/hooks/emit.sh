#!/bin/sh
set -eu

echo "emit: $*"
echo "KTL_STACK_ROOT=${KTL_STACK_ROOT:-}"
echo "KTL_STACK_RUN_ID=${KTL_STACK_RUN_ID:-}"
echo "KTL_STACK_COMMAND=${KTL_STACK_COMMAND:-}"
echo "KTL_RELEASE_NAME=${KTL_RELEASE_NAME:-}"
