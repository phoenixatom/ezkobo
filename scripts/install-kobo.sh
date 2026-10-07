#!/bin/sh
# Install EzKobo on every Kobo plugged into this Mac: back it up, copy
# KoboRoot.tgz into its .kobo folder, and eject it. The Kobo installs
# EzKobo when it restarts. Kobos without NickelMenu get it too (it provides
# the "Import new books" menu item), except on firmware 5.x, which
# NickelMenu doesn't support yet.
#
# Usage: scripts/install-kobo.sh [--no-backup] [--no-eject] [kobo-drive ...]

set -eu

cd "$(dirname "$0")/.."
PKG=dist/KoboRoot.tgz
PKG_NM=dist/KoboRoot-with-NickelMenu.tgz
backup=1
eject=1

while [ $# -gt 0 ]; do
	case "$1" in
	--no-backup) backup=0 ;;
	--no-eject) eject=0 ;;
	-*) echo "Unknown option $1" >&2; exit 2 ;;
	*) break ;;
	esac
	shift
done

if [ ! -f "$PKG" ] || [ ! -f "$PKG_NM" ]; then
	echo "Building packages…"
	make kobo >/dev/null
fi

install_on() {
	drive=${1%/}
	if [ ! -f "$drive/.kobo/version" ]; then
		echo "Skipping $drive: not a Kobo." >&2
		return 1
	fi
	echo "== $drive"

	# Kobo deletes KoboRoot.tgz once installed, so an existing one is an
	# update still waiting to install (e.g. a NickelMenu update). Don't
	# overwrite it.
	pending="$drive/.kobo/KoboRoot.tgz"
	if [ -f "$pending" ]; then
		if cmp -s "$pending" "$PKG" || cmp -s "$pending" "$PKG_NM"; then
			echo "  EzKobo is already copied and waiting to install. Eject and restart the Kobo."
			return 0
		fi
		echo "  Stopped: $pending is already there — another update is waiting to install." >&2
		echo "  Eject the Kobo, let it restart and install that first, then run this again." >&2
		return 1
	fi

	# NickelMenu installs .adds/nm/doc; EzKobo's own .adds/nm/ezkobo doesn't count.
	firmware=$(head -n 1 "$drive/.kobo/version" | cut -d, -f3)
	pkg=$PKG
	if [ -f "$drive/.adds/nm/doc" ]; then
		echo "  NickelMenu found: EzKobo adds two menu items (EzKobo status, Import new books)."
	elif [ "${firmware%%.*}" -ge 5 ] 2>/dev/null; then
		echo "  NickelMenu not installed, and it doesn't support firmware $firmware yet." >&2
		echo "  Installing EzKobo alone: books will arrive, but there's no Import new books button." >&2
	else
		echo "  NickelMenu not installed: installing it too (for the Import new books button)."
		pkg=$PKG_NM
	fi
	if [ -e "$drive/ezkobo-uninstall" ]; then
		echo "  Removing a leftover ezkobo-uninstall marker."
		rm -rf "$drive/ezkobo-uninstall"
	fi

	if [ "$backup" = 1 ]; then
		scripts/backup-kobo.sh "$drive" | sed 's/^/  /'
	fi

	cp "$pkg" "$pending.part"
	mv "$pending.part" "$pending"
	sync
	echo "  Copied KoboRoot.tgz."

	if [ "$eject" = 1 ]; then
		if diskutil eject "$drive" >/dev/null 2>&1; then
			echo "  Ejected. Unplug the Kobo; it installs EzKobo and restarts."
		else
			echo "  Couldn't eject automatically. Eject it in Finder, then unplug it." >&2
		fi
	fi
}

failed=0
if [ $# -gt 0 ]; then
	for drive in "$@"; do install_on "$drive" || failed=1; done
else
	found=0
	for drive in /Volumes/*; do
		if [ -f "$drive/.kobo/version" ]; then
			found=1
			install_on "$drive" || failed=1
		fi
	done
	if [ "$found" = 0 ]; then
		echo "No Kobo found. Plug it in, wait for it to appear in Finder, and try again." >&2
		exit 1
	fi
fi
exit "$failed"
