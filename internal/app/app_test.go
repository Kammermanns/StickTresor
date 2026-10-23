package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kammermanns/StickTresor/internal/partner"
	"github.com/Kammermanns/StickTresor/internal/vault"
)

const partnerPW = "Partner-Test-1"

// Ablauf mit IT-Partner: Partner-Angaben, Übergabe, Kunde legt Passwort fest.
// Danach darf weder das Übergabe-Passwort noch eine Sicherung damit übrig bleiben.
func TestHandoverFlow(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, nil)
	st, _ := a.Status()
	if st.State != "new" || st.Partner != nil || st.AppTitle != partner.DefaultAppName || !st.CanPartner {
		t.Fatalf("Start: %+v", st)
	}
	st, err := a.SavePartner(partner.Partner{AppName: "[D]er Passwort[M]anager", Firma: "Muster IT GmbH", Tel: "031 000 00 00",
		Helpdesk: "0900 00 00 00", HelpdeskInfo: "CHF 1.00/Min.", Web: "https://www.muster.ch/", Color: "#e6007e"})
	if err != nil {
		t.Fatal(err)
	}
	if st.Partner == nil || st.Partner.Web != "www.muster.ch" || st.Partner.Color != "#E6007E" || st.AppTitle != "Der PasswortManager" {
		t.Fatalf("Partner: %+v", st.Partner)
	}
	if err := a.Create("kurz", false); err == nil {
		t.Fatal("zu kurzes Übergabe-Passwort akzeptiert")
	}
	if err := a.Create(partnerPW, false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CapturePC(); err != nil { // erzeugt automatische Sicherung
		t.Fatal(err)
	}
	if _, err := a.Backup(); err != nil {
		t.Fatal(err)
	}
	// Neustart im Übergabe-Modus: Anmeldehinweis für den Partner
	a.Lock()
	if st, _ := a.Status(); st.State != "handover-locked" {
		t.Fatalf("erwartet handover-locked: %s", st.State)
	}
	if err := a.Unlock(partnerPW); err != nil {
		t.Fatal(err)
	}
	if err := a.Handover(); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Open(dir, partnerPW); err == nil {
		t.Fatal("Übergabe-Passwort gilt nach der Übergabe noch")
	}

	// Jemand ändert partner.json auf dem Stick
	os.WriteFile(filepath.Join(dir, partner.FileName), []byte(`{"firma":"Fremd AG"}`), 0o644)

	b := New(dir, nil) // nächster Start beim Kunden
	st, _ = b.Status()
	if st.State != "setpw" || st.RecoveryKey == "" {
		t.Fatalf("Status: %+v", st)
	}
	if st.CanPartner {
		t.Fatal("Kunde darf Partner-Angaben nicht ändern")
	}
	if err := b.SetMaster("kunde1"); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "Backup", "*.kdbx"))
	for _, f := range files {
		t.Errorf("Sicherung übrig: %s", filepath.Base(f))
	}
	if _, err := os.Stat(filepath.Join(dir, handoverFile)); err == nil {
		t.Fatal("uebergabe.status sollte gelöscht sein")
	}
	if _, err := os.Stat(filepath.Join(dir, vault.RecoveryFileName)); err != nil {
		t.Fatal("Wiederherstellungsdatei fehlt")
	}
	if _, err := b.SavePartner(partner.Partner{Firma: "X"}); err == nil {
		t.Fatal("SavePartner im Kundenmodus erlaubt")
	}

	// Neustart: Sperrbildschirm zeigt die (veränderte) Datei, nach dem Entsperren gilt die geschützte Kopie
	c := New(dir, nil)
	if st, _ := c.Status(); st.State != "locked" {
		t.Fatalf("erwartet locked: %s", st.State)
	}
	if err := c.Unlock("kunde1"); err != nil {
		t.Fatal(err)
	}
	st, _ = c.Status()
	if st.Partner == nil || st.Partner.Firma != "Muster IT GmbH" {
		t.Fatalf("geschützte Kopie nicht verwendet: %+v", st.Partner)
	}
	if p, _ := partner.Load(dir); p == nil || p.Firma != "Muster IT GmbH" {
		t.Fatal("partner.json nicht wiederhergestellt")
	}
}

// Eigene Nutzung ohne Partner, auch wenn die App vor dem Passwort geschlossen wird.
func TestSelfUse(t *testing.T) {
	dir := t.TempDir()
	a := New(dir, nil)
	if err := a.Create("", true); err != nil {
		t.Fatal(err)
	}
	a.Lock() // App geschlossen, bevor das Passwort festgelegt wurde
	b := New(dir, nil)
	st, _ := b.Status()
	if st.State != "setpw" {
		t.Fatalf("Status: %+v", st)
	}
	if err := b.SetMaster("meinpasswort"); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Open(dir, "meinpasswort"); err != nil {
		t.Fatal(err)
	}
	if st, _ := New(dir, nil).Status(); st.State != "locked" || st.Partner != nil {
		t.Fatalf("Status: %+v", st)
	}
}

func TestPrepareStick(t *testing.T) {
	dir, stick := t.TempDir(), t.TempDir()
	a := New(dir, nil)
	if _, err := a.SavePartner(partner.Partner{Firma: "Muster IT GmbH"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.prepareTo(dir); err == nil {
		t.Fatal("gleicher Ordner erlaubt")
	}
	r, err := a.prepareTo(stick)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stick, r.Exe)); err != nil {
		t.Fatal("Programm fehlt")
	}
	b, _ := os.ReadFile(filepath.Join(stick, partner.FileName))
	if !strings.Contains(string(b), "Muster IT GmbH") || r.HasVault {
		t.Fatalf("partner.json: %s %+v", b, r)
	}
}
