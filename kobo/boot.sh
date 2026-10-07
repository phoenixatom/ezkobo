#!/bin/sh
# Started by udev at boot and whenever WiFi comes up. Detaches immediately so
# udev isn't kept waiting; waits for the user storage to be mounted, then
# starts the agent (a no-op if it's already running).
#
# Uninstall: create a folder (or file) named "ezkobo-uninstall" at the top of
# the Kobo drive and restart the Kobo. EzKobo removes itself and the marker.

# Prefix for all paths; only set when testing on another machine.
R=${EZKOBO_ROOT:-}

(
	i=0
	while [ ! -e "$R/mnt/onboard/.kobo/version" ] && [ $i -lt 120 ]; do
		sleep 1
		i=$((i + 1))
	done

	log="$R/mnt/onboard/.adds/ezkobo/ezkobo.log"
	mkdir -p "${log%/*}"
	echo "$(date '+%Y/%m/%d %H:%M:%S') boot.sh: triggered (${ACTION:-?} ${KERNEL:-?})" >>"$log"

	if [ -e "$R/mnt/onboard/ezkobo-uninstall" ]; then
		"$R/usr/local/ezkobo/ezkobo" stop
		rm -f "$R/etc/udev/rules.d/99-ezkobo.rules" "$R/mnt/onboard/.adds/nm/ezkobo"
		rm -rf "$R/mnt/onboard/ezkobo-uninstall" "$R/usr/local/ezkobo"
		exit 0
	fi

	exec "$R/usr/local/ezkobo/ezkobo" start
) </dev/null >/dev/null 2>&1 &
