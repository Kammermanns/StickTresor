#!/bin/sh
# Baut StickTresor für alle Systeme in den Ordner dist/.
# Aufruf: sh build/package.sh 4.2.0
set -eu
V="${1:-0.0.0-dev}"
LD="-s -w -X github.com/Kammermanns/StickTresor/internal/app.Version=$V"
rm -rf dist && mkdir -p dist
export CGO_ENABLED=0
# Windows (Icon, Manifest und Versionsangaben aus rsrc_windows_amd64.syso)
GOOS=windows GOARCH=amd64 go build -mod=vendor -trimpath -ldflags "$LD -H windowsgui" -o dist/StickTresor.exe .
# Linux
GOOS=linux GOARCH=amd64 go build -mod=vendor -trimpath -ldflags "$LD" -o dist/StickTresor-linux-amd64 .
GOOS=linux GOARCH=arm64 go build -mod=vendor -trimpath -ldflags "$LD" -o dist/StickTresor-linux-arm64 .
# macOS (Apple Silicon und Intel); der Go-Linker signiert ad hoc
GOOS=darwin GOARCH=arm64 go build -mod=vendor -trimpath -ldflags "$LD" -o dist/StickTresor-macos-arm64 .
GOOS=darwin GOARCH=amd64 go build -mod=vendor -trimpath -ldflags "$LD" -o dist/StickTresor-macos-amd64 .
APP=dist/StickTresor.app/Contents
mkdir -p "$APP/MacOS" "$APP/Resources"
sed "s/VERSION/${V%%-*}/g" build/macos/Info.plist > "$APP/Info.plist"
cp build/macos/StickTresor.icns "$APP/Resources/StickTresor.icns"
cp build/macos/launcher.sh "$APP/MacOS/StickTresor"
cp dist/StickTresor-macos-arm64 "$APP/MacOS/StickTresor-arm64"
cp dist/StickTresor-macos-amd64 "$APP/MacOS/StickTresor-amd64"
chmod 755 "$APP/MacOS/StickTresor" "$APP/MacOS/StickTresor-arm64" "$APP/MacOS/StickTresor-amd64" dist/StickTresor-linux-* dist/StickTresor-macos-*
(cd dist && zip -qry StickTresor-macos.zip StickTresor.app)
(cd dist && for f in StickTresor.exe StickTresor-linux-amd64 StickTresor-linux-arm64 StickTresor-macos-arm64 StickTresor-macos-amd64 StickTresor-macos.zip; do sha256sum "$f" > "$f.sha256"; done)
ls -la dist
