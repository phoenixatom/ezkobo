#!/bin/sh
# Pack a directory tree into a KoboRoot.tgz: regular files only, owned by root.
# Directory entries are left out on purpose: extracting "etc/" or "usr/" would
# reset permissions on the Kobo's existing system folders.
#
# Usage: scripts/pack-koboroot.sh <root-dir> <output.tgz>
set -eu
root=$1
out=$(cd "$(dirname "$2")" && pwd)/$(basename "$2")
cd "$root"
find . -type f | sed 's|^\./||' | sort | COPYFILE_DISABLE=1 tar --no-xattrs \
	--uid 0 --gid 0 --uname root --gname root -czf "$out" -T -
