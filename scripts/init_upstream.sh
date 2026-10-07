#!/bin/sh
# Check out the pinned upstream reference submodules (a shallow clone of each gitlink).
# Every fresh clone, worktree or Rift needs this once before ./scripts/gate.sh.
# Offline: EFFRA_UPSTREAM_MIRROR=/path/to/local/effect-clone scripts/init_upstream.sh
# fetches the pinned commit from that clone instead of GitHub. Extra arguments go to
# `git submodule update`; `--force` restores a checkout with modified tracked files.
set -eu
mirror=${EFFRA_UPSTREAM_MIRROR:-}
case $mirror in
  '' | *://*) ;;
  # A file:// URL keeps --depth effective; git ignores depth for plain local paths.
  *) mirror=file://$(cd "$mirror" && pwd) ;;
esac
cd "$(dirname "$0")/.."
path=conformance/upstream/effect
git submodule init -- "$path"
if [ -z "$mirror" ]; then
  exec git submodule update --depth 1 "$@" -- "$path"
fi
exec git -c protocol.file.allow=always -c "submodule.$path.url=$mirror" submodule update --depth 1 "$@" -- "$path"
