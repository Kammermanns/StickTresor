# Code signing policy

Free code signing provided by [SignPath.io](https://about.signpath.io), certificate by [SignPath Foundation](https://signpath.org).

## Was signiert wird

- Nur `StickTresor.exe`, gebaut von GitHub Actions aus dem Quellcode dieses Repositories (Workflow `.github/workflows/build.yml`).
- Keine Dateien von Dritten. Alle verwendeten Bibliotheken liegen als Quellcode im Repository (`third_party/`, `vendor/`).
- Jede Signatur wird einzeln von Hand freigegeben.

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
