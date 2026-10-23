package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Kammermanns/StickTresor/internal/update"
)

// CheckUpdate fragt auf Knopfdruck bei GitHub nach einer neueren Version.
// Sonst baut die App keine Verbindung ins Internet auf.
func (a *App) CheckUpdate() (*update.Info, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	in, err := update.Check(ctx, Version)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.upd = in
	a.mu.Unlock()
	return in, nil
}

// InstallUpdate lädt die zuvor gefundene Version, prüft sie, ersetzt die Exe
// und startet das Programm neu. Vorher wird der Tresor gesichert.
func (a *App) InstallUpdate() error {
	a.mu.Lock()
	in := a.upd
	if err := a.needOpen(); err != nil {
		a.mu.Unlock()
		return err
	}
	a.backupFile("vor-update")
	a.mu.Unlock()
	if in == nil || !in.Available {
		return errors.New("Bitte zuerst nach Updates suchen.")
	}
	if a.plat == nil {
		return errors.New("Nicht verfügbar.")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	nw, err := update.Download(ctx, in, exe, a.plat.Signature)
	if err != nil {
		return err
	}
	if err := update.Install(exe, nw); err != nil {
		return err
	}
	a.Lock()
	if err := a.plat.Restart(exe); err != nil {
		return errors.New("Das Update ist installiert. Bitte das Programm neu starten.")
	}
	a.plat.Quit()
	return nil
}
