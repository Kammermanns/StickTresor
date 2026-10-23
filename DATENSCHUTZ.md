# Datenschutz

StickTresor überträgt **keine Daten** an andere Computer oder Dienste.

- Alle Angaben (PC-Datenblätter, Passwörter, Abos) liegen verschlüsselt in `daten.kdbx` im Ordner des Programms.
- Die Firmenangaben des IT-Partners liegen in `partner.json` (nicht geheim) und geschützt im Tresor.
- Die PC-Erfassung liest nur den eigenen PC aus (PowerShell/CIM) und speichert das Ergebnis im Tresor.
- Es gibt keine Telemetrie, keine Werbung und keine Konten.
- Verbindungen ins Internet entstehen nur, wenn Sie es selbst auslösen:
  - beim Öffnen eines Links (z.B. der Webseite eines Eintrags) im Browser,
  - bei **«Nach Updates suchen»**: Die App fragt bei GitHub (api.github.com) nach der neuesten Version dieses Projekts
    und lädt bei «Jetzt aktualisieren» die Programmdatei von github.com. Dabei werden keine Daten aus dem Tresor übertragen.
    Es gilt die [Datenschutzerklärung von GitHub](https://docs.github.com/de/site-policy/privacy-policies/github-general-privacy-statement).
