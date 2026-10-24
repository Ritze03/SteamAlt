#!/usr/bin/env bash
# Builds the release binaries into dist/: SteamAlt-Linux (native, cgo) and SteamAlt.exe (Windows cross-compile).
#
# The Windows icon/manifest/version live in the committed rsrc_windows_amd64.syso, which `go build`
# links automatically. Regenerate it only after changing winres/ (the group icon must keep ID "#1":
# Gio loads the window icon by that ID):
#   go run github.com/tc-hib/go-winres@v0.3.3 make --arch amd64 --in winres/winres.json
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p dist
CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o dist/SteamAlt-Linux .
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H windowsgui" -o dist/SteamAlt.exe .
