#!/bin/sh
set -eu
echo "fixture script ran at $(date -u +%H:%M:%S)" > /run.log
cat /run.log
