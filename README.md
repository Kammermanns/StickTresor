# StickTresor

> **English summary:** StickTresor is a free, open-source portable Windows app (single exe, no installation) that runs from a USB stick.
> It stores PC data sheets, passwords, subscriptions and a "digital estate" (what relatives should do with accounts and devices) in one
> encrypted KeePass file (KDBX 4, Argon2 + ChaCha20), which can also be opened with KeePass, KeePassXC, KeePassDX or Strongbox.
> IT service companies can add their own branding (logo, address, support) in a separate `partner.json` file and hand the stick over to
> their customers, who then set their own master password. No telemetry, no accounts; the app only goes online when the user clicks
> "check for updates" (GitHub Releases). Downloads: [Releases](https://github.com/Kammermanns/StickTresor/releases). License: MIT.

Portable App für **PC-Datenblätter, Passwörter, Abos und den digitalen Nachlass** – keine Installation, läuft direkt vom USB-Stick auf **Windows, macOS und Linux**.
Alle Daten liegen verschlüsselt in `daten.kdbx` im offenen **KeePass-Format (KDBX 4)** und lassen sich jederzeit auch mit KeePass, KeePassXC, KeePassDX oder Strongbox öffnen.

## Für wen?

- **Privat / selbst:** Programm starten, «Selbst verwenden», eigenes Master-Passwort festlegen.
- **IT-Firmen für ihre Kunden:** Firmenangaben (Logo, Adresse, Support, Helpdesk, Farbe) einmal erfassen, mit «Auf USB-Stick kopieren» Programm und Angaben auf den Stick bringen, beim Kunden die PCs erfassen und mit «Passwort setzen» übergeben. Beim nächsten Start legt der Kunde sein eigenes Master-Passwort fest; ab dann hat nur noch er Zugriff.

Im Programm stehen **keine Firmendaten und keine Passwörter**. Die Angaben der IT-Firma liegen in `Daten\partner.json`
(und geschützt im Tresor). Das Übergabe-Passwort wählt die IT-Firma selbst; es wird nirgends gespeichert.

## Windows, macOS und Linux

| System | Programm | Oberfläche |
|---|---|---|
| Windows 10/11 | `StickTresor.exe` | eigenes Fenster (WebView2) |
| macOS 11+ (Apple Silicon und Intel) | `StickTresor.app` | Fenster im Browser (Chrome/Edge im App-Modus, sonst Safari) |
| Linux (x86-64, ARM64) | `StickTresor-linux-amd64` / `-arm64` | Fenster im Browser (Chrome/Chromium/Edge im App-Modus, sonst Standardbrowser) |

Alle Versionen verwenden denselben Ordner `Daten` – ein Stick für alle Systeme.

Auf macOS und Linux startet das Programm einen lokalen Server nur auf `127.0.0.1` mit zufälligem Port. Jeder Zugriff braucht einen
Einmal-Schlüssel, den nur das von der App geöffnete Fenster kennt; Host- und Origin-Prüfung schützen vor fremden Webseiten.
Wird das Fenster geschlossen, beendet sich das Programm.

Hinweise:
- **macOS:** Aus dem Internet geladen, blockiert Gatekeeper die App beim ersten Start (Rechtsklick → «Öffnen»). Eine Beglaubigung durch Apple ist (noch) nicht vorhanden.
- **Linux:** Auf FAT32-Sticks sind Programme oft nicht ausführbar; exFAT verwenden oder das Programm in den Benutzerordner kopieren.
- **PC erfassen:** macOS über `system_profiler`, Linux über `/sys`, `/proc`, `lsblk`, `lpstat`, `nmcli`. Was ohne Administratorrechte nicht lesbar ist (z.B. die Seriennummer unter Linux), bleibt leer.

## Updates

Zahnrad → «Nach Updates suchen» fragt bei GitHub nach dem neuesten Release. «Jetzt aktualisieren» lädt `StickTresor.exe`,
prüft die SHA-256-Prüfsumme (`StickTresor.exe.sha256` im Release) und – sobald die Versionen signiert sind – Signatur und Herausgeber,
ersetzt die Exe auf dem Stick und startet neu. Daten und Firmenangaben bleiben unverändert, vorher wird gesichert.

Neue Version veröffentlichen: auf GitHub unter **Releases → Draft a new release** einen Tag wie `v4.1.0` anlegen und **Publish release** drücken. GitHub Actions baut die Exe und hängt `StickTresor.exe` und `StickTresor.exe.sha256` an das Release an.

## Dateien auf dem Stick

Auf der obersten Ebene liegt nur das Programm, alle Daten liegen im Ordner `Daten`
(Sticks der Version 4.0 werden beim ersten Start automatisch umgestellt).

| Datei | Inhalt |
|---|---|
| `StickTresor.exe` | das Programm für Windows |
| `StickTresor.app` | das Programm für macOS |
| `StickTresor-linux-amd64` | das Programm für Linux |
| `Daten\daten.kdbx` | der verschlüsselte Tresor (KeePass-Format) |
| `Daten\wiederherstellung.kdbx` | Kopie, mit dem Wiederherstellungsschlüssel verschlüsselt |
| `Daten\partner.json` | Firmenangaben der IT-Firma (optional) |
| `Daten\uebergabe.status` | nur bis der Kunde sein Passwort festgelegt hat |
| `Daten\Backup\` | automatische Sicherungen (die letzten 20) |

## Aufbau

| Ordner / Datei | Inhalt |
|---|---|
| `main_windows.go` | Windows: Start, Fenster (WebView2), nur eine Instanz pro Ordner |
| `main_unix.go`, `platform_unix.go` | macOS/Linux: Start, Browserfenster, Zwischenablage, Ordnerwahl |
| `platform_windows.go` | Zwischenablage (Auto-Löschen, kein Verlauf), Ordner öffnen/wählen |
| `internal/partner` | Firmenangaben der IT-Firma (`partner.json`) |
| `internal/web` | Browser-Oberfläche für macOS und Linux (lokaler Server mit Schlüssel) |
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

Alle Systeme auf einmal (inkl. `StickTresor.app`): `sh build/package.sh 4.2.0` → Ordner `dist/`.

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
