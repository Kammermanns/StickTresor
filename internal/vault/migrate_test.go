package vault

import (
	"path/filepath"
	"strings"
	"testing"

	kp "github.com/tobischo/gokeepasslib/v3"
)

const testPW = "Partner-Test-1"

// Version 2.1 legte Gerätepasswörter in «Geräte-Passwörter/<Gerät>» ab.
func TestMigrateDeviceGroups(t *testing.T) {
	dir := t.TempDir()
	v, err := Create(dir, testPW)
	if err != nil {
		t.Fatal(err)
	}
	g := v.deviceGroup("SURFACE", true)
	e := kp.NewEntry()
	set(&e, "Title", "BIOS", false)
	set(&e, "Password", "b", true)
	g.Entries = append(g.Entries, e)
	cat := v.Categories()[1].ID
	g2 := findGroup(v.root(), cat)
	e2 := kp.NewEntry()
	set(&e2, "Title", "Alt", false)
	set(&e2, fieldPC, "LAGER", false)
	g2.Entries = append(g2.Entries, e2)
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}
	v2, err := Open(dir, testPW)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Entry{}
	for _, e := range v2.Entries() {
		got[e.Title] = e
	}
	if b := got["BIOS"]; len(b.Devices) != 1 || b.Devices[0] != "SURFACE" {
		t.Fatalf("BIOS: %+v", b)
	}
	if a := got["Alt"]; len(a.Devices) != 1 || a.Devices[0] != "LAGER" || a.CategoryID != cat {
		t.Fatalf("Alt: %+v", a)
	}
	if v2.devGroup() != nil {
		t.Fatal("Gerätegruppe sollte weg sein")
	}
	if err := v2.Save(); err != nil {
		t.Fatal(err)
	}
}

// Dateien älterer Versionen verwenden Meta-Schlüssel mit dem Präfix "DM.".
func TestLegacyMetaKeys(t *testing.T) {
	dir := t.TempDir()
	v, err := Create(dir, testPW)
	if err != nil {
		t.Fatal(err)
	}
	cd := v.db.Content.Meta.CustomData
	for i := range cd {
		cd[i].Key = strings.Replace(cd[i].Key, "ST.", "DM.", 1)
	}
	if v.Mode() != ModeHandover {
		t.Fatalf("Modus aus DM.Modus nicht gelesen: %s", v.Mode())
	}
	v.SetMode(ModeCustomer)
	n := 0
	for _, c := range v.db.Content.Meta.CustomData {
		if strings.HasSuffix(c.Key, ".Modus") {
			n++
			if c.Key != "ST.Modus" {
				t.Fatalf("Schlüssel: %s", c.Key)
			}
		}
	}
	if n != 1 || v.Mode() != ModeCustomer {
		t.Fatal("Modus doppelt oder falsch")
	}
}

// Wiederherstellungsdateien bis Version 4.1 hatten den Schlüssel ohne Bindestriche als Passwort.
func TestLegacyRecoveryPassword(t *testing.T) {
	dir := t.TempDir()
	v, err := Create(dir, testPW)
	if err != nil {
		t.Fatal(err)
	}
	key := NewRecoveryKey()
	v.SetRecoveryKey(key)
	v.recovery = kp.NewPasswordCredentials(NormalizeRecoveryKey(key)) // altes Verfahren
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}
	r, err := OpenRecovery(dir, key)
	if err != nil {
		t.Fatal("alte Wiederherstellungsdatei:", err)
	}
	r.SetPassword("neu-123456")
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	// jetzt mit dem Schlüssel wie auf dem Ausdruck (für KeePass)
	if _, err := openFile(dir, filepath.Join(dir, RecoveryFileName), kp.NewPasswordCredentials(key)); err != nil {
		t.Fatal("nach dem Speichern nicht umgestellt:", err)
	}
}
