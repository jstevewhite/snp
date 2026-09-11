# snp build tooling.

GO  ?= go
NPM ?= npm

.PHONY: web build test dev desktop desktop-install run-desktop app run-app clean web-install

# Install web dependencies when missing (a fresh clone has none, and
# `npm test` cannot run without them). `web` also rebuilds web/dist.
web-install:
	@if [ -f web/package.json ]; then \
		cd web && [ -d node_modules ] || $(NPM) ci; \
	fi

# Build the Svelte app into web/dist. Skipped (using the checked-in
# stub) until web/package.json exists in phase 6.
web:
	@if [ -f web/package.json ]; then \
		cd web && $(NPM) ci && $(NPM) run build; \
	else \
		echo "web: no package.json yet (phase 6); using stub dist"; \
	fi

build: web
	$(GO) build -o bin/snp ./cmd/snp

# The Wails desktop app (spec §12). Built on macOS and Linux; Windows is
# a follow-on. The `production` build tag is required on every platform —
# without it wails runs a stub that refuses to start.
#
# macOS additionally needs the UniformTypeIdentifiers framework linked:
# wails v2.15.0's darwin code references UTType but does not link it
# itself, which fails against the macOS 26 SDK
# ("_OBJC_CLASS_$_UTType"). There is no -framework equivalent on Linux,
# so the flag must not be passed there.
#
# Linux needs the GTK/WebKit dev packages (build-essential pkg-config
# libgtk-3-dev, plus libwebkit2gtk-4.1-dev or libwebkit2gtk-4.0-dev).
# WEBKIT2 selects the wails tag for the WebKit2GTK API: `webkit2_41` for
# the 4.1 API (Ubuntu 24.04+, Debian 13, Fedora 40+ — 4.0 is gone
# there), empty for the older 4.0 API (Debian 12, Ubuntu 22.04). It is
# detected from pkg-config, so `make desktop` works on both; override
# with WEBKIT2= (force 4.0) or WEBKIT2=webkit2_41 (force 4.1).
WEBKIT2 ?= $(shell pkg-config --exists webkit2gtk-4.1 2>/dev/null && echo webkit2_41)

UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
DESKTOP_CGO_LDFLAGS := -framework UniformTypeIdentifiers
DESKTOP_TAGS        := production
else
DESKTOP_CGO_LDFLAGS :=
DESKTOP_TAGS        := production $(WEBKIT2)
endif

desktop: web
	CGO_LDFLAGS="$(DESKTOP_CGO_LDFLAGS)" $(GO) build -tags "$(DESKTOP_TAGS)" -o bin/snp-desktop ./cmd/snp-desktop

# Install the desktop app for the current user (Linux): binary, .desktop
# entry, and icons under ~/.local. Override the prefix with
# PREFIX=/some/dir. macOS packaging is `make app` instead.
PREFIX ?= $(HOME)/.local
desktop-install: desktop
	./deploy/install-desktop.sh -p "$(PREFIX)"

# Launch the desktop app window against the default state dir
# (~/.local/share/snp, shared with the CLI and server variants).
run-desktop: desktop
	./bin/snp-desktop

# Codesign identity for the .app bundle (Developer ID Application).
# Empty by default: make-app.sh then produces an ad-hoc, un-notarized
# bundle, so a fresh clone can `make app` with no Apple credentials. Set
# your own Developer ID (or "-" for an explicit ad-hoc bundle) to sign.
SIGN_IDENTITY ?=

# notarytool keychain profile used to notarize the bundle. Create it with
#   xcrun notarytool store-credentials snp-notary --apple-id … --team-id … --password …
# Empty by default (skip notarization); set it to ship to other machines.
NOTARY_PROFILE ?=

# Package the desktop binary into a signed, notarized macOS .app bundle
# (deploy/make-app.sh → build/snp.app, plus build/snp.zip when
# notarized). Override with make app SIGN_IDENTITY=… NOTARY_PROFILE=… .
app: desktop
	./deploy/make-app.sh "$(SIGN_IDENTITY)" "$(NOTARY_PROFILE)"

# Launch the bundled app from Finder/Launchpad.
run-app: app
	open build/snp.app

test: web-install
	$(GO) vet ./...
	$(GO) test ./...
	@if [ -f web/package.json ]; then \
		cd web && $(NPM) test && $(NPM) run check; \
	fi

dev: build
	./bin/snp serve --dev-listen :8080

clean:
	rm -rf bin
