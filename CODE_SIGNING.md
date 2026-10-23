# Code signing policy

Free code signing provided by [SignPath.io](https://about.signpath.io), certificate by [SignPath Foundation](https://signpath.org).

## Was signiert wird

- Nur `StickTresor.exe`, gebaut von GitHub Actions aus dem Quellcode dieses Repositories (Workflow `.github/workflows/build.yml`).
- Keine Dateien von Dritten. Alle verwendeten Bibliotheken liegen als Quellcode im Repository (`third_party/`, `vendor/`).
- Jede Signatur wird einzeln von Hand freigegeben.

## Ablauf

1. Ein Release wird auf GitHub veröffentlicht (Tag `vX.Y.Z`).
2. GitHub Actions baut `StickTresor.exe` aus dem Quellcode (Produktname und Version in der Exe aus dem Tag) und lädt sie als Artefakt hoch.
3. Der Workflow sendet eine Signier-Anfrage an SignPath (`.github/workflows/build.yml`, Artefaktkonfiguration `.signpath/artifact-configuration.xml`).
4. Der Approver gibt die Anfrage in SignPath von Hand frei.
5. Die signierte Exe und ihre SHA-256-Prüfsumme werden an das Release angehängt.

## Rollen

| Rolle | Person |
|---|---|
| Committer und Reviewer | [Stefan Kammermann](https://github.com/Kammermanns) |
| Approver (Freigabe der Signatur) | [Stefan Kammermann](https://github.com/Kammermanns) |

Änderungen von anderen Personen (Pull Requests) werden vor der Übernahme von einem Committer geprüft.
Alle Mitglieder verwenden die Zwei-Faktor-Anmeldung bei GitHub und SignPath.

## Datenschutz

This program will not transfer any information to other networked systems unless specifically requested by the user
or the person installing or operating it (opening a link, «Nach Updates suchen» / «Jetzt aktualisieren»).
Siehe [DATENSCHUTZ.md](DATENSCHUTZ.md).
