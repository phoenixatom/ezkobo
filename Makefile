# Kobo e-readers are ARMv7 Linux. A static binary needs nothing on the device.
ROOT := build/root

# NickelMenu (https://github.com/pgaskin/NickelMenu) provides the Kobo menu
# items, including "Import new books". Bundled for Kobos that don't have it.
NM_VERSION := v0.6.0
NM_SHA256 := 322ff9aa863860e8f5f7e0b55cae561c54bf95983b9bce1d19819d1225d064af
NM_TGZ := .cache/NickelMenu-$(NM_VERSION)-KoboRoot.tgz

# NickelDBus (https://github.com/shermp/NickelDBus) lets EzKobo import books
# into the library as soon as they arrive. Tested on Clara and Libra Colour.
NDB_VERSION := 0.2.0
NDB_SHA256 := 9fdb3d16d0f43c1ea6f2f1264b10fcde1d72a674e55f12955de812d476eb5dc5
NDB_TGZ := .cache/NickelDBus-$(NDB_VERSION)-KoboRoot.tgz

# dist/KoboRoot.tgz: copy into the Kobo's .kobo folder and eject; the Kobo
# installs it on reboot. The same file works on every Kobo.
# dist/KoboRoot.tgz                 EzKobo + NickelDBus (Kobos with NickelMenu)
# dist/KoboRoot-with-NickelMenu.tgz EzKobo + NickelDBus + NickelMenu
# build/KoboRoot-ezkobo-only.tgz    EzKobo alone (firmware 5.x, unsupported by the mods)
kobo: $(NM_TGZ) $(NDB_TGZ)
	rm -rf build dist
	mkdir -p $(ROOT)/usr/local/ezkobo $(ROOT)/etc/udev/rules.d $(ROOT)/mnt/onboard/.adds/nm dist
	GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(ROOT)/usr/local/ezkobo/ezkobo ./cmd/ezkobo
	install -m 755 kobo/boot.sh $(ROOT)/usr/local/ezkobo/boot.sh
	install -m 644 kobo/99-ezkobo.rules $(ROOT)/etc/udev/rules.d/99-ezkobo.rules
	install -m 644 nickelmenu/ezkobo $(ROOT)/mnt/onboard/.adds/nm/ezkobo
	scripts/pack-koboroot.sh $(ROOT) build/KoboRoot-ezkobo-only.tgz
	cp -R $(ROOT) build/root-ndb
	tar -xzf $(NDB_TGZ) -C build/root-ndb
	scripts/pack-koboroot.sh build/root-ndb dist/KoboRoot.tgz
	cp -R build/root-ndb build/root-nm
	tar -xzf $(NM_TGZ) -C build/root-nm
	scripts/pack-koboroot.sh build/root-nm dist/KoboRoot-with-NickelMenu.tgz
	@echo "Built dist/KoboRoot.tgz and dist/KoboRoot-with-NickelMenu.tgz"

$(NDB_TGZ):
	mkdir -p .cache
	curl -fsSL -o $@.part https://github.com/shermp/NickelDBus/releases/download/$(NDB_VERSION)/KoboRoot.tgz
	echo "$(NDB_SHA256)  $@.part" | shasum -a 256 -c -
	mv $@.part $@

$(NM_TGZ):
	mkdir -p .cache
	curl -fsSL -o $@.part https://github.com/pgaskin/NickelMenu/releases/download/$(NM_VERSION)/KoboRoot.tgz
	echo "$(NM_SHA256)  $@.part" | shasum -a 256 -c -
	mv $@.part $@

# Run the agent locally: web UI at http://localhost:8080, discoverable by the app.
dev:
	mkdir -p tmp/books
	go run ./cmd/ezkobo serve -addr :8080 -dir tmp/books -library tmp -state tmp/state -pidfile tmp/ezkobo.pid -rescan off -name "Dev Kobo"

# Back up, copy KoboRoot.tgz onto, and eject every plugged-in Kobo.
install-kobo: kobo
	scripts/install-kobo.sh

# Back up every plugged-in Kobo to ~/Kobo Backups.
backup:
	scripts/backup-kobo.sh

# Mark every plugged-in Kobo for EzKobo removal; it uninstalls on next restart.
uninstall-kobo:
	@found=0; for d in /Volumes/*; do \
		if [ -f "$$d/.kobo/version" ]; then mkdir -p "$$d/ezkobo-uninstall"; echo "Marked $$d"; found=1; fi; \
	done; \
	if [ $$found = 1 ]; then echo "Eject the Kobo and restart it to remove EzKobo."; \
	else echo "No Kobo found. Plug it in and try again."; exit 1; fi

# Generate the Xcode project. Put your Apple team ID in ios/Team.local
# (git-ignored) to have it filled in, or choose a team in Xcode.
app:
	cd ios && EZKOBO_TEAM="$$(cat Team.local 2>/dev/null)" xcodegen generate

test:
	go vet ./...
	go test ./...

.PHONY: kobo dev app test backup uninstall-kobo install-kobo
