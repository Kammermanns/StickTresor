package app

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/Kammermanns/StickTresor/internal/partner"
	"github.com/Kammermanns/StickTresor/internal/vault"
)

// DataFolder ist der Unterordner neben der Exe, in dem alle Daten liegen.
// So liegt auf der obersten Ebene des Sticks nur das Programm.
const DataFolder = "Daten"

// Dateien und Ordner, die zum Tresor gehören (werden bei älteren Sticks verschoben).
var dataFiles = []string{vault.FileName, vault.RecoveryFileName, handoverFile, partner.FileName, "Backup"}

// DataDir liefert den Datenordner zu einem Programmordner und legt ihn bei Bedarf an.
// Liegen die Daten noch direkt neben der Exe (Version 4.0 und älter), werden sie verschoben.
// Lässt sich der Ordner nicht anlegen (z.B. schreibgeschützter Stick), bleibt es beim Programmordner.
func DataDir(exeDir string) string {
	d := filepath.Join(exeDir, DataFolder)
	if err := os.MkdirAll(d, 0o700); err != nil {
		if vault.Exists(exeDir) {
			return exeDir
		}
		return d
	}
	for _, name := range dataFiles {
		src, dst := filepath.Join(exeDir, name), filepath.Join(d, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if _, err := os.Stat(dst); err == nil {
			continue // im Datenordner gibt es die Datei schon: nichts überschreiben
		}
		if err := os.Rename(src, dst); err != nil && name == vault.FileName {
			return exeDir // Tresor liess sich nicht verschieben: am alten Ort weiterarbeiten
		}
	}
	return d
}

// vaultIn meldet, ob in einem Programmordner (neu oder alt) ein Tresor liegt.
func vaultIn(exeDir string) bool {
	return vault.Exists(filepath.Join(exeDir, DataFolder)) || vault.Exists(exeDir)
}

var errSameDir = errors.New("Bitte einen anderen Ordner wählen als den, in dem die App gerade läuft.")
