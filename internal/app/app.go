// Package app verbindet Oberfläche, Tresor und PC-Erfassung.
// Jede exportierte Methode wird der Oberfläche als JavaScript-Funktion bereitgestellt.
package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Kammermanns/StickTresor/internal/capture"
	"github.com/Kammermanns/StickTresor/internal/partner"
	"github.com/Kammermanns/StickTresor/internal/update"
	"github.com/Kammermanns/StickTresor/internal/vault"
)

// Version wird beim Bauen aus dem Git-Tag gesetzt (-ldflags "-X .../internal/app.Version=4.1.0").
var Version = "4.1.0"

// handoverFile merkt sich ohne Passwort, in welchem Übergabe-Schritt der Stick ist:
//
//	"uebergabe"            Partner erfasst noch (Anmeldung mit seinem Übergabe-Passwort)
//	"ausstehend <Schlüssel>" übergeben: der Tresor ist mit diesem Einmal-Schlüssel verschlüsselt,
//	                       der Kunde legt beim nächsten Start sein Passwort fest; danach wird die Datei gelöscht.
const handoverFile = "uebergabe.status"

// Platform kapselt, was nur unter Windows geht.
type Platform interface {
	CopyToClipboard(text string, clearAfter time.Duration) error
	ClearClipboardIfOurs()
	OpenFolder(path string) error
	PickFolder(title string) (string, error)
	Quit()
	OpenURL(url string) error
	// Signature prüft die Authenticode-Signatur einer Datei (gültig, Herausgeber).
	Signature(path string) (bool, string, error)
	// Restart startet das (aktualisierte) Programm neu.
	Restart(exe string) error
}

type App struct {
	mu     sync.Mutex
	exeDir string // Ordner des Programms
	dir    string // Datenordner (exeDir\Daten)
	v      *vault.Vault
	plat   Platform

	// Zustand nach dem Öffnen:
	// "" = gesperrt, "open" = geöffnet,
	// "setpw" = Kunde muss eigenes Passwort festlegen (nach Übergabe oder Wiederherstellung)
	state       string
	recoveryKey string // nur während "setpw" nach der Übergabe
	recovered   bool   // "setpw" nach Wiederherstellung
	backedUp    bool   // automatische Sicherung in dieser Sitzung erfolgt
	handedOver  bool   // in dieser Sitzung an den Kunden übergeben
	upd         *update.Info
}

// New erzeugt die App für den Programmordner exeDir; die Daten liegen in exeDir\Daten.
func New(exeDir string, p Platform) *App {
	return &App{exeDir: exeDir, dir: DataDir(exeDir), plat: p}
}

// ---------- Status ----------

type Status struct {
	Exists      bool             `json:"exists"`
	State       string           `json:"state"` // "new", "locked", "handover-locked", "setpw", "open", "handed-over"
	Mode        string           `json:"mode"`
	HasRecovery bool             `json:"hasRecovery"`
	Recovered   bool             `json:"recovered"`
	RecoveryKey string           `json:"recoveryKey"`
	Dir         string           `json:"dir"`
	File        string           `json:"file"`
	Version     string           `json:"version"`
	LastSaved   string           `json:"lastSaved"`
	Writable    bool             `json:"writable"`
	Partner     *partner.Partner `json:"partner"`
	AppTitle    string           `json:"appTitle"`
	CanPartner  bool             `json:"canPartner"` // Partner-Angaben dürfen bearbeitet werden
}

// Status sagt der Oberfläche, welcher Bildschirm zu zeigen ist.
// Beim ersten Aufruf wird geprüft, ob die Datei noch im Übergabe-Modus ist.
func (a *App) Status() (s Status, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s = Status{Dir: a.dir, File: filepath.Join(a.dir, vault.FileName), Version: Version,
		HasRecovery: vault.RecoveryExists(a.dir), Writable: writable(a.dir)}
	s.Exists = vault.Exists(a.dir)
	defer func() {
		s.Partner = a.partnerLocked()
		s.AppTitle = partner.Title(s.Partner)
		s.CanPartner = !s.Exists || (a.v != nil && a.state == "open" && a.v.Mode() == vault.ModeHandover)
	}()
	switch {
	case !s.Exists:
		s.State = "new"
	case a.handedOver && a.v == nil:
		// Gerade übergeben: nicht sofort nach dem Kundenpasswort fragen,
		// das macht der Kunde beim nächsten Start.
		s.State = "handed-over"
	case a.v != nil && a.state == "setpw":
		s.State = "setpw"
		s.RecoveryKey = a.recoveryKey
		s.Recovered = a.recovered
	case a.v != nil:
		s.State = "open"
		s.Mode = a.v.Mode()
		if t := a.v.LastSaved(); !t.IsZero() {
			s.LastSaved = t.Format("15:04")
		}
	default:
		// Gesperrt. Übergeben und noch ohne Kunden-Passwort?
		step, key := a.handoverStep()
		if step == "ausstehend" && key != "" {
			if v, err := vault.Open(a.dir, key); err == nil && v.Mode() == vault.ModePending {
				// Erster Start beim Kunden: direkt eigenes Passwort festlegen
				a.v = v
				a.state = "setpw"
				a.recovered = false
				a.recoveryKey = vault.NewRecoveryKey()
				s.State = "setpw"
				s.RecoveryKey = a.recoveryKey
				return s, nil
			}
		}
		if step == "uebergabe" {
			s.State = "handover-locked"
			return s, nil
		}
		s.State = "locked"
	}
	return s, nil
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".st-schreibtest-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// ---------- Anlegen, Entsperren, Passwörter ----------

// Create legt einen neuen Tresor an (nur wenn noch keiner existiert).
//
//   - self = false: Übergabe-Modus. Der IT-Partner wählt sein Übergabe-Passwort selbst;
//     es wird nirgends gespeichert und gilt nur bis «Passwort setzen».
//   - self = true:  Eigene Nutzung ohne Partner. Danach folgt direkt «Master-Passwort festlegen».
func (a *App) Create(password string, self bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if vault.Exists(a.dir) {
		return errors.New("Es gibt hier schon eine Datei daten.kdbx.")
	}
	if self {
		// Vorläufig mit Einmal-Schlüssel anlegen (wie nach einer Übergabe),
		// damit ein Abbruch vor dem Festlegen des Passworts nichts verliert.
		key := strings.ReplaceAll(vault.NewRecoveryKey(), "-", "")
		v, err := vault.Create(a.dir, key)
		if err != nil {
			return fmt.Errorf("Anlegen fehlgeschlagen: %v", err)
		}
		v.SetMode(vault.ModePending)
		a.copyPartnerToVault(v)
		if err := v.Save(); err != nil {
			return fmt.Errorf("Anlegen fehlgeschlagen: %v", err)
		}
		a.writeHandoverStep("ausstehend " + key)
		a.v, a.state, a.recovered, a.recoveryKey = v, "setpw", false, vault.NewRecoveryKey()
		return nil
	}
	if utf8.RuneCountInString(password) < 6 {
		return errors.New("Das Übergabe-Passwort muss mindestens 6 Zeichen lang sein.")
	}
	v, err := vault.Create(a.dir, password)
	if err != nil {
		return fmt.Errorf("Anlegen fehlgeschlagen: %v", err)
	}
	a.copyPartnerToVault(v)
	if err := v.Save(); err != nil {
		return fmt.Errorf("Anlegen fehlgeschlagen: %v", err)
	}
	a.writeHandoverStep("uebergabe")
	a.v, a.state = v, "open"
	return nil
}

func (a *App) handoverStep() (step, key string) {
	b, err := os.ReadFile(filepath.Join(a.dir, handoverFile))
	if err != nil {
		return "", ""
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return "", ""
	}
	if len(f) > 1 {
		key = f[1]
	}
	return f[0], key
}

func (a *App) writeHandoverStep(content string) error {
	return os.WriteFile(filepath.Join(a.dir, handoverFile), []byte(content+"\n"), 0o600)
}

func (a *App) Unlock(password string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	v, err := vault.Open(a.dir, password)
	if err != nil {
		return err
	}
	if v.Mode() == vault.ModePending {
		a.v, a.state, a.recovered, a.recoveryKey = v, "setpw", false, vault.NewRecoveryKey()
		return nil
	}
	a.v, a.state = v, "open"
	a.backedUp = false
	a.restorePartnerFile()
	return nil
}

// Recover öffnet die Wiederherstellungsdatei. Danach muss SetMaster folgen.
func (a *App) Recover(key string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !vault.RecoveryExists(a.dir) {
		return errors.New("Es gibt keine Wiederherstellungsdatei (wiederherstellung.kdbx).")
	}
	v, err := vault.OpenRecovery(a.dir, key)
	if err != nil {
		if errors.Is(err, vault.ErrWrongPassword) {
			return errors.New("Der Wiederherstellungsschlüssel ist falsch.")
		}
		return err
	}
	a.v, a.state, a.recovered, a.recoveryKey = v, "setpw", true, ""
	return nil
}

func checkStrength(pw string) error {
	if utf8.RuneCountInString(pw) < 6 {
		return errors.New("Das Passwort muss mindestens 6 Zeichen lang sein.")
	}
	return nil
}

// SetMaster legt das eigene Master-Passwort des Kunden fest
// (nach der Übergabe oder nach einer Wiederherstellung).
func (a *App) SetMaster(password string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.v == nil || a.state != "setpw" {
		return errors.New("Nicht möglich.")
	}
	if err := checkStrength(password); err != nil {
		return err
	}
	handover := !a.recovered
	if !handover {
		a.backupFile("vor-passwortwechsel")
	}
	a.v.SetPassword(password)
	if handover {
		a.v.SetRecoveryKey(a.recoveryKey)
	}
	a.v.SetMode(vault.ModeCustomer)
	if err := a.v.Save(); err != nil {
		return fmt.Errorf("Speichern fehlgeschlagen: %v", err)
	}
	if handover {
		// Alle bisherigen Sicherungen sind mit dem Übergabe-Passwort des Partners verschlüsselt
		// und wären für jeden mit diesem Passwort lesbar: entfernen.
		a.purgeBackups()
		os.Remove(filepath.Join(a.dir, handoverFile))
	}
	a.state, a.recoveryKey, a.recovered = "open", "", false
	a.restorePartnerFile()
	return nil
}

// ChangeMaster ändert das Master-Passwort (Kunde, eingeloggt).
func (a *App) ChangeMaster(oldPw, newPw string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.needOpen(); err != nil {
		return err
	}
	if !a.v.CheckPassword(oldPw) {
		return errors.New("Das bisherige Passwort ist falsch.")
	}
	if err := checkStrength(newPw); err != nil {
		return err
	}
	if a.v.Mode() == vault.ModeHandover {
		return errors.New("Im Übergabe-Modus bitte «Passwort setzen» verwenden.")
	}
	a.backupFile("vor-passwortwechsel")
	a.v.SetPassword(newPw)
	return a.saveErr()
}

// NewRecoveryKey erzeugt einen neuen Wiederherstellungsschlüssel und schreibt die Datei neu.
func (a *App) NewRecoveryKey(password string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.needOpen(); err != nil {
		return "", err
	}
	if a.v.Mode() == vault.ModeHandover {
		return "", errors.New("Erst nach der Übergabe möglich.")
	}
	if !a.v.CheckPassword(password) {
		return "", errors.New("Das Master-Passwort ist falsch.")
	}
	key := vault.NewRecoveryKey()
	a.v.SetRecoveryKey(key)
	if err := a.v.Save(); err != nil {
		return "", err
	}
	return key, nil
}

// Handover schliesst die Erfassung ab: Beim nächsten Start legt der Kunde sein Passwort fest.
func (a *App) Handover() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.needOpen(); err != nil {
		return err
	}
	if a.v.Mode() != vault.ModeHandover {
		return errors.New("Der Tresor wurde bereits übergeben.")
	}
	// Mit einem Einmal-Schlüssel neu verschlüsseln: Das Übergabe-Passwort des Partners
	// gilt ab jetzt nicht mehr, der Kunde legt beim nächsten Start sein eigenes fest.
	key := strings.ReplaceAll(vault.NewRecoveryKey(), "-", "")
	a.v.SetPassword(key)
	a.v.SetMode(vault.ModePending)
	if err := a.v.Save(); err != nil {
		return err
	}
	if err := a.writeHandoverStep("ausstehend " + key); err != nil {
		return err
	}
	a.lockLocked()
	a.handedOver = true
	return nil
}

func (a *App) Lock() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lockLocked()
}

func (a *App) lockLocked() {
	a.v, a.state, a.recoveryKey, a.recovered = nil, "", "", false
	if a.plat != nil {
		a.plat.ClearClipboardIfOurs()
	}
}

func (a *App) needOpen() error {
	if a.v == nil || a.state != "open" {
		return errors.New("Der Tresor ist gesperrt.")
	}
	return nil
}

// ---------- Daten ----------

type Data struct {
	Mode         string            `json:"mode"`
	PCs          []vault.PC        `json:"pcs"`
	Categories   []vault.Category  `json:"categories"`
	Entries      []vault.Entry     `json:"entries"`
	Sections     []capture.Section `json:"sections"`
	CustomerKeys []string          `json:"customerKeys"`
	Nachlass     string            `json:"nachlass"` // allgemeine Nachlass-Angaben (JSON)
	LastSaved    string            `json:"lastSaved"`
}

func (a *App) Data() (Data, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.needOpen(); err != nil {
		return Data{}, err
	}
	return a.dataLocked(), nil
}

func (a *App) dataLocked() Data {
	pcs := a.v.PCs()
	sort.SliceStable(pcs, func(i, j int) bool { return strings.ToLower(pcs[i].Name) < strings.ToLower(pcs[j].Name) })
	d := Data{Mode: a.v.Mode(), PCs: pcs, Categories: a.v.Categories(), Entries: a.v.Entries(), Sections: capture.Sections, CustomerKeys: capture.CustomerKeys, Nachlass: a.v.Nachlass()}
	if t := a.v.LastSaved(); !t.IsZero() {
		d.LastSaved = t.Format("15:04")
	}
	return d
}

type CaptureResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Created bool   `json:"created"`
	Wifi    string `json:"wifi"` // WLAN-Eintrag angelegt/aktualisiert
	Data    Data   `json:"data"`
}

// CapturePC liest diesen PC aus und speichert ihn (neu oder aktualisiert).
func (a *App) CapturePC() (CaptureResult, error) {
	a.mu.Lock()
	if err := a.needOpen(); err != nil {
		a.mu.Unlock()
		return CaptureResult{}, err
	}
	a.mu.Unlock()

	r, err := capture.Collect() // dauert einige Sekunden, ohne Sperre
	if err != nil {
		return CaptureResult{}, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.needOpen(); err != nil {
		return CaptureResult{}, err
	}
	id, created := a.v.UpsertCaptured(r.Name, r.Fields)
	wifi := ""
	if r.Wifi != nil {
		if changed, err := a.v.UpsertWifi(r.Wifi.SSID, r.Wifi.Password, r.Name); err == nil && changed {
			wifi = r.Wifi.SSID
		}
	}
	if err := a.saveErr(); err != nil {
		return CaptureResult{}, err
	}
	return CaptureResult{ID: id, Name: r.Name, Created: created, Wifi: wifi, Data: a.dataLocked()}, nil
}

func (a *App) SavePC(pc vault.PC) (Data, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.needOpen(); err != nil {
		return Data{}, err
	}
	if err := a.v.SavePC(pc); err != nil {
		return Data{}, err
	}
	return a.dataLocked(), a.saveErr()
}

func (a *App) DeletePC(id string) (Data, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.needOpen(); err != nil {
		return Data{}, err
	}
	if err := a.v.DeletePC(id); err != nil {
		return Data{}, err
	}
	return a.dataLocked(), a.saveErr()
}

type SaveEntryResult struct {
	ID   string `json:"id"`
	Data Data   `json:"data"`
}

func (a *App) SaveEntry(e vault.Entry) (SaveEntryResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.needOpen(); err != nil {
		return SaveEntryResult{}, err
	}
	id, err := a.v.SaveEntry(e)
	if err != nil {
		return SaveEntryResult{}, err
	}
	return SaveEntryResult{ID: id, Data: a.dataLocked()}, a.saveErr()
}

func (a *App) DeleteEntry(id string) (Data, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.needOpen(); err != nil {
		return Data{}, err
	}
	if err := a.v.DeleteEntry(id); err != nil {
		return Data{}, err
	}
	return a.dataLocked(), a.saveErr()
}

type ImportResult struct {
	Added   int  `json:"added"`
	Skipped int  `json:"skipped"`
	Data    Data `json:"data"`
}

// Import übernimmt Zeilen aus Excel oder einer CSV-Datei (ein einziges Speichern).
func (a *App) Import(rows []vault.ImportRow, categoryID string, skipDup bool) (ImportResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.needOpen(); err != nil {
		return ImportResult{}, err
	}
	added, skipped, err := a.v.Import(rows, categoryID, skipDup)
	if err != nil {
		return ImportResult{}, err
	}
	if added > 0 {
		if err := a.saveErr(); err != nil {
			return ImportResult{}, err
		}
	}
	return ImportResult{Added: added, Skipped: skipped, Data: a.dataLocked()}, nil
}

func (a *App) AddCategory(name string) (SaveEntryResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.needOpen(); err != nil {
		return SaveEntryResult{}, err
	}
	id, err := a.v.AddCategory(name)
	if err != nil {
		return SaveEntryResult{}, err
	}
	return SaveEntryResult{ID: id, Data: a.dataLocked()}, a.saveErr()
}

func (a *App) RenameCategory(id, name string) (Data, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.needOpen(); err != nil {
		return Data{}, err
	}
	if err := a.v.RenameCategory(id, name); err != nil {
		return Data{}, err
	}
	return a.dataLocked(), a.saveErr()
}

func (a *App) DeleteCategory(id string) (Data, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.needOpen(); err != nil {
		return Data{}, err
	}
	if err := a.v.DeleteCategory(id); err != nil {
		return Data{}, err
	}
	return a.dataLocked(), a.saveErr()
}

// ---------- Zwischenablage, Ordner, Backup ----------

func (a *App) Copy(text string) error {
	if a.plat == nil {
		return nil
	}
	return a.plat.CopyToClipboard(text, 30*time.Second)
}

// Quit schliesst die App.
func (a *App) Quit() {
	a.Lock()
	if a.plat != nil {
		a.plat.Quit()
	}
}

// OpenURL öffnet eine Webseite oder E-Mail-Adresse im Standardprogramm.
func (a *App) OpenURL(u string) error {
	if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "mailto:") {
		return errors.New("Ungültige Adresse")
	}
	if a.plat == nil {
		return nil
	}
	return a.plat.OpenURL(u)
}

func (a *App) OpenFolder() error {
	if a.plat == nil {
		return nil
	}
	return a.plat.OpenFolder(a.dir)
}

// Backup kopiert die (verschlüsselte) Datei in den Ordner Backup auf dem Stick.
func (a *App) Backup() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.needOpen(); err != nil {
		return "", err
	}
	p, err := a.backupFile("")
	if err != nil {
		return "", err
	}
	return filepath.Base(p), nil
}

// BackupTo kopiert daten.kdbx (und die Wiederherstellungsdatei) in einen frei wählbaren Ordner.
func (a *App) BackupTo() (string, error) {
	a.mu.Lock()
	if err := a.needOpen(); err != nil {
		a.mu.Unlock()
		return "", err
	}
	a.mu.Unlock()
	if a.plat == nil {
		return "", errors.New("Nicht verfügbar.")
	}
	dir, err := a.plat.PickFolder("Ordner für die Sicherung wählen")
	if err != nil || dir == "" {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	stamp := time.Now().Format("2006-01-02_1504")
	dst := filepath.Join(dir, "Sicherung_"+stamp+".kdbx")
	if err := copyFile(filepath.Join(a.dir, vault.FileName), dst); err != nil {
		return "", err
	}
	if vault.RecoveryExists(a.dir) {
		copyFile(filepath.Join(a.dir, vault.RecoveryFileName), filepath.Join(dir, "Sicherung_"+stamp+"_wiederherstellung.kdbx"))
	}
	return dst, nil
}

// saveErr speichert und legt einmal pro Sitzung vorher automatisch eine Sicherung an.
func (a *App) saveErr() error {
	if !a.backedUp {
		a.backupFile("auto")
		a.backedUp = true
	}
	if err := a.v.Save(); err != nil {
		return fmt.Errorf("Speichern fehlgeschlagen: %v", err)
	}
	return nil
}

const keepBackups = 20

func (a *App) backupFile(tag string) (string, error) {
	src := filepath.Join(a.dir, vault.FileName)
	if _, err := os.Stat(src); err != nil {
		return "", nil
	}
	bdir := filepath.Join(a.dir, "Backup")
	if err := os.MkdirAll(bdir, 0o700); err != nil {
		return "", err
	}
	name := "daten_" + time.Now().Format("2006-01-02_150405")
	if tag != "" {
		name += "_" + tag
	}
	dst := filepath.Join(bdir, name+".kdbx")
	if err := copyFile(src, dst); err != nil {
		return "", err
	}
	// Alte Sicherungen aufräumen
	files, _ := filepath.Glob(filepath.Join(bdir, "daten_*.kdbx"))
	sort.Strings(files)
	for len(files) > keepBackups {
		os.Remove(files[0])
		files = files[1:]
	}
	return dst, nil
}

// purgeBackups löscht alle Sicherungen im Ordner Backup (nach der Übergabe).
func (a *App) purgeBackups() {
	files, _ := filepath.Glob(filepath.Join(a.dir, "Backup", "daten_*.kdbx"))
	for _, f := range files {
		os.Remove(f)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Bindings liefert alle Funktionen, die die Oberfläche aufrufen darf (Name ohne Präfix).
func (a *App) Bindings() map[string]any {
	return map[string]any{
		"status":         a.Status,
		"create":         a.Create,
		"unlock":         a.Unlock,
		"recover":        a.Recover,
		"setMaster":      a.SetMaster,
		"changeMaster":   a.ChangeMaster,
		"newRecoveryKey": a.NewRecoveryKey,
		"handover":       a.Handover,
		"lock":           a.Lock,
		"data":           a.Data,
		"capturePC":      a.CapturePC,
		"savePC":         a.SavePC,
		"deletePC":       a.DeletePC,
		"saveEntry":      a.SaveEntry,
		"deleteEntry":    a.DeleteEntry,
		"addCategory":    a.AddCategory,
		"renameCategory": a.RenameCategory,
		"deleteCategory": a.DeleteCategory,
		"copy":           a.Copy,
		"openFolder":     a.OpenFolder,
		"backup":         a.Backup,
		"backupTo":       a.BackupTo,
		"quit":           a.Quit,
		"openURL":        a.OpenURL,
		"import":         a.Import,
		"savePartner":    a.SavePartner,
		"removePartner":  a.RemovePartner,
		"prepareStick":   a.PrepareStick,
		"checkUpdate":    a.CheckUpdate,
		"saveNachlass":   a.SaveNachlass,
		"installUpdate":  a.InstallUpdate,
	}
}

// SaveNachlass speichert die allgemeinen Angaben für den digitalen Nachlass
// (Person, Vertrauensperson, Aufbewahrung, eigene Zeilen, Geräte) verschlüsselt im Tresor.
func (a *App) SaveNachlass(js string) (Data, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.needOpen(); err != nil {
		return Data{}, err
	}
	if len(js) > 1<<20 || !json.Valid([]byte(js)) {
		return Data{}, errors.New("Die Nachlass-Angaben sind ungültig.")
	}
	a.v.SetNachlass(js)
	return a.dataLocked(), a.saveErr()
}
