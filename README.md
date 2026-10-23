# StickTresor

Portable Windows-App für **PC-Datenblätter, Passwörter und Abos** – eine einzige Exe, keine Installation, läuft direkt vom USB-Stick.
Alle Daten liegen verschlüsselt in `daten.kdbx` im offenen **KeePass-Format (KDBX 4)** und lassen sich jederzeit auch mit KeePass, KeePassXC, KeePassDX oder Strongbox öffnen.

## Für wen?

- **Privat / selbst:** Programm starten, «Selbst verwenden», eigenes Master-Passwort festlegen.
- **IT-Firmen für ihre Kunden:** Firmenangaben (Logo, Adresse, Support, Helpdesk, Farbe) einmal erfassen, mit «Auf USB-Stick kopieren» Programm und Angaben auf den Stick bringen, beim Kunden die PCs erfassen und mit «Passwort setzen» übergeben. Beim nächsten Start legt der Kunde sein eigenes Master-Passwort fest; ab dann hat nur noch er Zugriff.

Im Programm stehen **keine Firmendaten und keine Passwörter**. Die Angaben der IT-Firma liegen in `partner.json` neben der Exe
(und geschützt im Tresor). Das Übergabe-Passwort wählt die IT-Firma selbst; es wird nirgends gespeichert.

## Updates

Zahnrad → «Nach Updates suchen» fragt bei GitHub nach dem neuesten Release. «Jetzt aktualisieren» lädt `StickTresor.exe`,
prüft die SHA-256-Prüfsumme (`StickTresor.exe.sha256` im Release) und – sobald die Versionen signiert sind – Signatur und Herausgeber,
ersetzt die Exe auf dem Stick und startet neu. Daten und Firmenangaben bleiben unverändert, vorher wird gesichert.

Neue Version veröffentlichen: auf GitHub unter **Releases → Draft a new release** einen Tag wie `v4.1.0` anlegen und **Publish release** drücken. GitHub Actions baut die Exe und hängt `StickTresor.exe` und `StickTresor.exe.sha256` an das Release an.

## Dateien auf dem Stick

| Datei | Inhalt |
|---|---|
| `StickTresor.exe` | das Programm |
| `partner.json` | Firmenangaben der IT-Firma (optional) |
| `daten.kdbx` | der verschlüsselte Tresor |
| `wiederherstellung.kdbx` | Kopie, mit dem Wiederherstellungsschlüssel verschlüsselt |
| `uebergabe.status` | nur bis der Kunde sein Passwort festgelegt hat |
| `Backup\` | automatische Sicherungen (die letzten 20) |

## Aufbau

| Ordner / Datei | Inhalt |
|---|---|
| `main_windows.go` | Start, Fenster (WebView2), nur eine Instanz pro Ordner |
| `platform_windows.go` | Zwischenablage (Auto-Löschen, kein Verlauf), Ordner öffnen/wählen |
| `internal/partner` | Firmenangaben der IT-Firma (`partner.json`) |
| `internal/update` | Update-Prüfung und Austausch der Exe |
| `internal/vault` | KeePass-Datei: Lesen, Schreiben, Übergabe-Modus, Wiederherstellung |
| `internal/capture` | PC-Erfassung per PowerShell/CIM (keine Adminrechte nötig) |
| `internal/app` | Funktionen, die die Oberfläche aufruft |
| `ui/index.html` | Oberfläche (HTML/CSS/JS, wird in die Exe eingebettet) |
| `third_party/` | gokeepasslib (MIT, um Argon2id ergänzt), argon2, go-webview2 (MIT, Aufrufe laufen im Hintergrund) |
| `vendor/` | golang.org/x/crypto und x/sys (BSD) – Build ohne Internet möglich |
| `cmd/devserver` | Oberfläche im Browser testen (Entwicklung) |

## Wichtige Konstanten

- Neutraler Name und Farbe ohne `partner.json`: `DefaultAppName`/`DefaultColor` in `internal/partner/partner.go` und `DEFAULT_NAME`/`DEFAULT_COLOR` in `ui/index.html`
- Felder und Reihenfolge des Datenblatts: `Sections` in `internal/capture/capture.go`

## Bauen (Windows oder Linux, Go 1.24+)

```
go build -mod=vendor -trimpath -ldflags "-s -w -H windowsgui" -o StickTresor.exe .
```

Version setzen: zusätzlich `-X github.com/Kammermanns/StickTresor/internal/app.Version=4.1.0` in `-ldflags`.

Unter Linux/macOS vorher `GOOS=windows GOARCH=amd64 CGO_ENABLED=0` setzen.
Icon, Manifest und Versionsangaben stecken in `rsrc_windows_amd64.syso`, erzeugt mit
[goversioninfo](https://github.com/josephspurrier/goversioninfo) aus `build/versioninfo.json`
(`goversioninfo -64 -o rsrc_windows_amd64.syso build/versioninfo.json`; der GitHub-Build setzt dabei die Version aus dem Release-Tag).

Tests: `go test -mod=vendor ./internal/...`

Oberfläche im Browser testen: `go run -mod=vendor ./cmd/devserver -dir /tmp/stick` und http://127.0.0.1:8099 öffnen.

## Code signing policy

Free code signing provided by [SignPath.io](https://about.signpath.io), certificate by [SignPath Foundation](https://signpath.org).
Kostenlose Code-Signatur durch SignPath.io, Zertifikat der SignPath Foundation (sobald das Projekt aufgenommen ist).

- Signiert werden nur Dateien, die GitHub Actions aus diesem Repository baut (`StickTresor.exe` in den Releases).
- Jede Signatur muss von Hand freigegeben werden.
- Rollen: siehe [CODE_SIGNING.md](CODE_SIGNING.md).

**Privacy / Datenschutz:** This program will not transfer any information to other networked systems unless specifically
requested by the user or the person installing or operating it. Details: [DATENSCHUTZ.md](DATENSCHUTZ.md).

## Lizenz

MIT (siehe `LICENSE`). Datenschutz: siehe `DATENSCHUTZ.md`.

Verwendete Bibliotheken:

gokeepasslib, argon2, go-webview2, go-winloader: MIT. golang.org/x/*: BSD-3-Clause.
Die Lizenztexte liegen in den jeweiligen Ordnern.
