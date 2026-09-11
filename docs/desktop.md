# Desktop build and packaging

The macOS/Linux desktop app uses Wails v2 and the same store and UI as the
server. Start with the [README requirements](../README.md#requirements).
Run all commands below from the repository root on the target OS; the
desktop binary does not cross-compile between macOS and Linux.

## Build and run

```sh
make desktop              # builds web/dist and bin/snp-desktop
make run-desktop          # builds, then opens the window
```

Both targets build the web app before embedding it. Install the platform's
native compiler and libraries first; make does not install them.

## Linux

On Ubuntu 24.04, the native build prerequisites are:

```sh
sudo apt-get install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev
```

For a system using WebKitGTK 4.0, such as Ubuntu 22.04 or Debian 12, use
`libwebkit2gtk-4.0-dev` instead. Other distributions use their own package
names for the compiler, pkg-config, GTK3, and WebKitGTK development files.
The binary also needs the matching GTK/WebKit shared libraries at runtime.

The Makefile detects WebKitGTK 4.1 with pkg-config and selects Wails'
`webkit2_41` build tag. Override detection when needed:

```sh
make desktop WEBKIT2=              # WebKitGTK 4.0
make desktop WEBKIT2=webkit2_41    # WebKitGTK 4.1
```

Install for the current user:

```sh
make desktop-install
```

This puts the binary in `~/.local/bin`, the launcher in
`~/.local/share/applications`, and icons in the hicolor theme under
`~/.local/share/icons`. No root is needed. Ensure `~/.local/bin` is on your
`PATH`; override the installation root with `PREFIX=/some/path`.

## macOS

Install Xcode Command Line Tools for the CGO toolchain before building.
The raw binary is `bin/snp-desktop`; package it as an app with:

```sh
make app                  # builds build/snp.app
make run-app              # builds the bundle, then opens it
```

`deploy/make-app.sh` adds an Info.plist and an app icon from
`deploy/appicon.png`, falling back to the PWA icon. `SIGN_IDENTITY` and
`NOTARY_PROFILE` default to empty: the bundle is ad-hoc signed and
un-notarized, suitable for local use without Apple signing credentials.
`SIGN_IDENTITY=-` explicitly selects ad-hoc signing.

### Distribution signing and notarization

To distribute a signed, notarized bundle, configure your own Developer ID
Application identity and a notarytool keychain profile. Replace the example
credentials below with your own:

```sh
xcrun notarytool store-credentials snp-notary \
  --apple-id "your-apple-id" --team-id "your-team-id" \
  --password "your-app-specific-password"
```

Then build:

```sh
make app SIGN_IDENTITY="Developer ID Application: Your Name (TEAMID)" \
         NOTARY_PROFILE=snp-notary
```

With a Developer ID, the script enables hardened-runtime signing, a secure
timestamp, and the network-client entitlement used for AI provider calls.
With both a real signing identity and a notary profile, it submits the
bundle, staples the accepted ticket, and writes `build/snp.zip` for
distribution. Without a profile, it signs but skips notarization.

## Build wiring

The Makefile includes Wails' `production` tag on both platforms; without
it, Wails builds a stub that refuses to start. On macOS it also sets
`CGO_LDFLAGS="-framework UniformTypeIdentifiers"` to satisfy Wails v2.15.0
when linking against the macOS 26 SDK. Linux builds omit this framework
flag and select the WebKitGTK tag described above.

Wails is imported only by `cmd/snp-desktop`. It stays out of `cmd/snp` and
`internal/desktop`, preserving the server binary's headless cross-builds.
