#!/bin/sh
# Back up every Kobo plugged into this Mac: books, reading progress,
# highlights and collections (everything on the drive, hidden folders too).
#
# Usage: scripts/backup-kobo.sh [kobo-drive ...]
#   With no arguments, backs up every mounted Kobo found in /Volumes.
#   Backups go to ~/Kobo Backups/<Model> <serial> <date>/ (or $KOBO_BACKUP_DIR).

set -eu

DEST_ROOT="${KOBO_BACKUP_DIR:-$HOME/Kobo Backups}"

model_name() {
	# Last 3 digits of the device ID in .kobo/version (see device.go).
	case "$1" in
	390) echo "Libra Colour" ;;
	393) echo "Clara Colour" ;;
	391 | 395) echo "Clara BW" ;;
	388) echo "Libra 2" ;;
	386) echo "Clara 2E" ;;
	383) echo "Sage" ;;
	387) echo "Elipsa" ;;
	389) echo "Elipsa 2E" ;;
	376) echo "Clara HD" ;;
	*) echo "Kobo" ;;
	esac
}

backup() {
	drive=$1
	version="$drive/.kobo/version"
	if [ ! -f "$version" ]; then
		echo "Skipping $drive: not a Kobo (no .kobo/version)." >&2
		return 1
	fi

	line=$(head -n 1 "$version")
	serial=$(echo "$line" | cut -d, -f1)
	device_id=$(echo "$line" | awk -F, '{print $NF}')
	model=$(model_name "$(echo "$device_id" | tail -c 4 | tr -d '\n')")
	suffix=$(echo "$serial" | tail -c 5 | tr -d '\n' | tr '[:lower:]' '[:upper:]')
	dest="$DEST_ROOT/$model $suffix $(date +%Y-%m-%d_%H%M)"

	echo "Backing up $model ($suffix) from $drive"
	echo "  to $dest"
	mkdir -p "$dest"
	# -a keeps folder structure and timestamps; hidden folders (.kobo, .adds)
	# are included. macOS metadata clutter is skipped.
	rsync -a --exclude '.Spotlight-V100' --exclude '.Trashes' --exclude '.fseventsd' \
		"$drive/" "$dest/"

	if [ ! -f "$dest/.kobo/KoboReader.sqlite" ]; then
		echo "  Warning: no .kobo/KoboReader.sqlite in the backup (reading progress database)." >&2
	fi
	echo "  Done: $(du -sh "$dest" | cut -f1 | tr -d " "), $(find "$dest" -type f | wc -l | tr -d ' ') files"
}

if [ $# -gt 0 ]; then
	for drive in "$@"; do backup "${drive%/}"; done
	exit 0
fi

found=0
for drive in /Volumes/*; do
	if [ -f "$drive/.kobo/version" ]; then
		backup "$drive"
		found=$((found + 1))
	fi
done

if [ "$found" -eq 0 ]; then
	echo "No Kobo found. Plug it in, wait for it to appear in Finder, and try again." >&2
	exit 1
fi
