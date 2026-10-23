package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kammermanns/StickTresor/internal/partner"
	"github.com/Kammermanns/StickTresor/internal/vault"
)

// partnerLocked liefert die gültigen Partner-Angaben: bei geöffnetem Tresor die geschützte
// Kopie aus der KeePass-Datei, sonst partner.json neben der Exe (nil = neutrale App).
func (a *App) partnerLocked() *partner.Partner {
	if a.v != nil {
		if js := a.v.Partner(); js != "" {
			if p, err := partner.Parse([]byte(js)); err == nil {
				return p
			}
		}
	}
	p, _ := partner.Load(a.dir)
	return p
}

func (a *App) copyPartnerToVault(v *vault.Vault) {
	if p, _ := partner.Load(a.dir); p != nil {
		v.SetPartner(string(p.JSON()))
	}
}

// restorePartnerFile stellt partner.json aus der geschützten Kopie wieder her,
// falls die Datei fehlt oder verändert wurde.
func (a *App) restorePartnerFile() {
	js := a.v.Partner()
	if js == "" {
		return
	}
	p, err := partner.Parse([]byte(js))
	if err != nil {
		return
	}
	cur, _ := os.ReadFile(filepath.Join(a.dir, partner.FileName))
	if !bytes.Equal(cur, p.JSON()) {
		partner.Save(a.dir, p)
	}
}

func (a *App) canEditPartner() error {
	if !vault.Exists(a.dir) {
		return nil
	}
	if a.v != nil && a.state == "open" && a.v.Mode() == vault.ModeHandover {
		return nil
	}
	return errors.New("Die Partner-Angaben können nur vor dem Anlegen oder im Übergabe-Modus geändert werden.")
}

// SavePartner speichert die Angaben der IT-Firma (partner.json und Kopie im Tresor).
func (a *App) SavePartner(p partner.Partner) (Status, error) {
	a.mu.Lock()
	if err := a.canEditPartner(); err != nil {
		a.mu.Unlock()
		return Status{}, err
	}
	if err := p.Clean(); err != nil {
		a.mu.Unlock()
		return Status{}, err
	}
	if err := partner.Save(a.dir, &p); err != nil {
		a.mu.Unlock()
		return Status{}, errors.New("partner.json kann nicht geschrieben werden: " + err.Error())
	}
	if a.v != nil {
		a.v.SetPartner(string(p.JSON()))
		if err := a.saveErr(); err != nil {
			a.mu.Unlock()
			return Status{}, err
		}
	}
	a.mu.Unlock()
	return a.Status()
}

// RemovePartner entfernt die Partner-Angaben (neutrale App).
func (a *App) RemovePartner() (Status, error) {
	a.mu.Lock()
	if err := a.canEditPartner(); err != nil {
		a.mu.Unlock()
		return Status{}, err
	}
	if err := os.Remove(filepath.Join(a.dir, partner.FileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		a.mu.Unlock()
		return Status{}, err
	}
	if a.v != nil {
		a.v.SetPartner("")
		if err := a.saveErr(); err != nil {
			a.mu.Unlock()
			return Status{}, err
		}
	}
	a.mu.Unlock()
	return a.Status()
}

type PrepareResult struct {
	Dir      string `json:"dir"`
	Exe      string `json:"exe"`
	HasVault bool   `json:"hasVault"` // im Ziel liegt schon ein Tresor (bleibt unverändert)
}

// PrepareStick kopiert das Programm und partner.json in einen Ordner (z.B. USB-Stick).
// Daten werden nie mitkopiert; ein vorhandener Tresor im Ziel bleibt unberührt.
func (a *App) PrepareStick() (PrepareResult, error) {
	if a.plat == nil {
		return PrepareResult{}, errors.New("Nicht verfügbar.")
	}
	dir, err := a.plat.PickFolder("USB-Stick oder Ordner für den Kunden wählen")
	if err != nil || dir == "" {
		return PrepareResult{}, err
	}
	return a.prepareTo(dir)
}

func (a *App) prepareTo(dir string) (PrepareResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	same := func(x, y string) bool {
		x, _ = filepath.Abs(x)
		y, _ = filepath.Abs(y)
		return strings.EqualFold(filepath.Clean(x), filepath.Clean(y))
	}
	if same(dir, a.exeDir) {
		return PrepareResult{}, errSameDir
	}
	exe, err := os.Executable()
	if err != nil {
		return PrepareResult{}, err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	dst := filepath.Join(dir, filepath.Base(exe))
	if err := copyFile(exe, dst); err != nil {
		return PrepareResult{}, errors.New("Das Programm konnte nicht kopiert werden (läuft es dort gerade?): " + err.Error())
	}
	if p := a.partnerLocked(); p != nil {
		target := filepath.Join(dir, DataFolder)
		if vault.Exists(dir) && !vault.Exists(target) {
			target = dir // älterer Stick: die neue Version verschiebt beim Start alles in «Daten»
		}
		if err := os.MkdirAll(target, 0o700); err != nil {
			return PrepareResult{}, err
		}
		if err := partner.Save(target, p); err != nil {
			return PrepareResult{}, err
		}
	}
	return PrepareResult{Dir: dir, Exe: filepath.Base(dst), HasVault: vaultIn(dir)}, nil
}
