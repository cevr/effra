#!/bin/sh
# The merge gate. The runner (scripts/gate) owns the check graph; see its
# package comment. Flags: --list, --no-cache, --record-durations.
set -eu
cd "$(dirname "$0")/.."
exec go run ./scripts/gate "$@"
