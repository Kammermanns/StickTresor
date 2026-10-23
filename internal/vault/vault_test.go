package vault

import (
	kp "github.com/tobischo/gokeepasslib/v3"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFullFlow(t *testing.T) {
	dir := t.TempDir()
	if d := os.Getenv("DM_TEST_DIR"); d != "" {
		dir = d
		os.Remove(filepath.Join(dir, FileName))
		os.Remove(filepath.Join(dir, RecoveryFileName))
	}

	start := time.Now()
	v, err := Create(dir, testPW)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Anlegen: %v", time.Since(start))
	if v.Mode() != ModeHandover {
		t.Fatal("Modus sollte Übergabe sein")
	}
	if len(v.Categories()) != len(DefaultCategories)+1 {
		t.Fatalf("Kategorien: %+v", v.Categories())
	}

	// PC erfassen, zweimal (gleiche Seriennummer => kein Duplikat)
	fields := []Field{{"Hersteller", "Dell Inc."}, {FieldSerial, "4XK8J93"}, {"Prozessor", "i5"}}
	id1, created := v.UpsertCaptured("BUERO-PC01", fields)
	if !created {
		t.Fatal("sollte neu sein")
	}
	pcs := v.PCs()
	pcs[0].Fields = append(pcs[0].Fields, Field{"Standort", "Büro 2. OG"})
	pcs[0].Notes = "Notiz äöü"
	if err := v.SavePC(pcs[0]); err != nil {
		t.Fatal(err)
	}
	id2, created := v.UpsertCaptured("BUERO-PC01", []Field{{FieldSerial, "4XK8J93"}, {"Prozessor", "i7"}})
	if created || id1 != id2 {
		t.Fatal("Duplikat statt Aktualisierung")
	}
	for _, f := range v.PCs()[0].Fields {
		if f.Key == "Hersteller" {
			t.Fatal("veraltetes Auto-Feld wurde nicht entfernt")
		}
	}

	cat := v.Categories()[0].ID
	eid, err := v.SaveEntry(Entry{CategoryID: cat, Title: "Windows-Anmeldung", UserName: "m.muster", Password: "Geheim-123!", Devices: []string{"BUERO-PC01"}})
	if err != nil {
		t.Fatal(err)
	}
	v.SetMode(ModePending)
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}

	// Kunde: mit Übergabe-Passwort öffnen, eigenes Passwort + Wiederherstellung
	v2, err := Open(dir, testPW)
	if err != nil {
		t.Fatal(err)
	}
	if v2.Mode() != ModePending {
		t.Fatal("Modus sollte ausstehend sein")
	}
	key := NewRecoveryKey()
	v2.SetPassword("Velo-Sonne-Brot-17")
	v2.SetRecoveryKey(key)
	v2.SetMode(ModeCustomer)
	start = time.Now()
	if err := v2.Save(); err != nil {
		t.Fatal(err)
	}
	t.Logf("Speichern (2 Dateien): %v", time.Since(start))

	if _, err := Open(dir, testPW); err != ErrWrongPassword {
		t.Fatalf("Übergabe-Passwort darf nicht mehr gehen: %v", err)
	}
	v3, err := Open(dir, "Velo-Sonne-Brot-17")
	if err != nil {
		t.Fatal(err)
	}
	pc := v3.PCs()[0]
	vals := map[string]string{}
	for _, f := range pc.Fields {
		vals[f.Key] = f.Value
	}
	if vals["Prozessor"] != "i7" || vals["Standort"] != "Büro 2. OG" || pc.Notes != "Notiz äöü" {
		t.Fatalf("PC-Daten falsch: %+v", pc)
	}
	es := v3.Entries()
	if len(es) != 1 || es[0].Password != "Geheim-123!" || es[0].ID != eid {
		t.Fatalf("Einträge falsch: %+v", es)
	}

	// Wiederherstellung (Eingabe in Kleinbuchstaben, ohne Bindestriche)
	r, err := OpenRecovery(dir, "rk"+NormalizeRecoveryKey(key)[2:])
	if err != nil {
		t.Fatal(err)
	}
	r.SetPassword("Neu-Passwort-2027")
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, "Neu-Passwort-2027"); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRecovery(dir, key); err != nil {
		t.Fatal("Schlüssel muss weiter gelten:", err)
	}

	// KeePass: Passwort der Wiederherstellungsdatei ist der Schlüssel wie auf dem Ausdruck
	if CanonicalRecoveryKey("rk"+NormalizeRecoveryKey(key)[2:]) != key {
		t.Fatalf("Schreibweise: %s / %s", CanonicalRecoveryKey(key), key)
	}
	if _, err := openFile(dir, filepath.Join(dir, RecoveryFileName), kp.NewPasswordCredentials(key)); err != nil {
		t.Fatal("wiederherstellung.kdbx lässt sich nicht mit dem gedruckten Schlüssel öffnen:", err)
	}
	// Eintrag verschieben und löschen
	v4, _ := Open(dir, "Neu-Passwort-2027")
	es = v4.Entries()
	es[0].CategoryID = v4.Categories()[2].ID
	if _, err := v4.SaveEntry(es[0]); err != nil {
		t.Fatal(err)
	}
	if v4.Categories()[2].Count != 1 || v4.Categories()[0].Count != 0 {
		t.Fatalf("Verschieben: %+v", v4.Categories())
	}
	if err := v4.DeleteCategory(v4.Categories()[2].ID); err == nil {
		t.Fatal("volle Kategorie darf nicht gelöscht werden")
	}
	n, sk, err := v4.Import([]ImportRow{
		{Title: "Bank", UserName: "u1", Password: "p1", Category: "Banking"},
		{Title: "Windows", UserName: "admin", Password: "w", PC: "LAGER-PC"},
		{URL: "https://www.galaxus.ch/login", UserName: "max", Password: "x"},
		{Title: "Bank", UserName: "u1", Password: "p1", Category: "Banking"},
		{},
	}, "", true)
	if err != nil || n != 3 || sk != 1 {
		t.Fatalf("Import: %d %d %v", n, sk, err)
	}
	found := false
	for _, e := range v4.Entries() {
		if e.Title == "galaxus.ch" {
			found = true
		}
	}
	if !found {
		t.Fatal("Titel aus URL fehlt")
	}
	dev := false
	for _, e := range v4.Entries() {
		if e.Title == "Windows" && len(e.Devices) == 1 && e.Devices[0] == "LAGER-PC" {
			dev = true
		}
	}
	if !dev {
		t.Fatal("Gerät aus Import fehlt")
	}
	// Eintrag mit zwei Geräten; PC umbenennen => Name im Eintrag folgt
	pc = v4.PCs()[0]
	old := pc.Name
	if _, err := v4.SaveEntry(Entry{CategoryID: v4.Categories()[1].ID, Title: "Outlook", Password: "o", Devices: []string{old, "LAGER-PC", old}}); err != nil {
		t.Fatal(err)
	}
	pc.Name = "BUERO-PC99"
	if err := v4.SavePC(pc); err != nil {
		t.Fatal(err)
	}
	okDev := false
	for _, e := range v4.Entries() {
		if e.Title == "Outlook" && len(e.Devices) == 2 && e.Devices[0] == "BUERO-PC99" && e.Devices[1] == "LAGER-PC" {
			okDev = true
		}
	}
	if !okDev {
		t.Fatalf("Geräte falsch: %+v", v4.Entries())
	}
	// Abo anlegen, ändern ohne Abo-Angabe (bleibt), dann entfernen
	aid, err := v4.SaveEntry(Entry{CategoryID: v4.Categories()[0].ID, Title: "Adobe", Abo: &Abo{Active: true, Cost: "23,9", Currency: "EUR", Interval: "monatlich", Next: "2026-11-15"}})
	if err != nil {
		t.Fatal(err)
	}
	getE := func(id string) Entry {
		for _, e := range v4.Entries() {
			if e.ID == id {
				return e
			}
		}
		return Entry{}
	}
	if a := getE(aid).Abo; a == nil || a.Cost != "23.90" || a.Status != "aktiv" || a.Currency != "EUR" {
		t.Fatalf("Abo: %+v", a)
	}
	e := getE(aid)
	e.Abo = nil
	e.Notes = "geändert"
	v4.SaveEntry(e)
	if getE(aid).Abo == nil {
		t.Fatal("Abo darf beim normalen Speichern nicht verschwinden")
	}
	e.Abo = &Abo{Active: false}
	v4.SaveEntry(e)
	if getE(aid).Abo != nil {
		t.Fatal("Abo sollte entfernt sein")
	}
	// Nachlass-Angaben pro Eintrag, normales Speichern ohne Angaben lässt sie stehen
	nid, _ := v4.SaveEntry(Entry{CategoryID: v4.Categories()[0].ID, Title: "Facebook", UserName: "max", Nachlass: &Nachlass{Include: "ja", Section: "social", Action: "In Gedenkzustand versetzen", TwoFA: "ja"}})
	ne := getE(nid)
	if ne.Nachlass == nil || ne.Nachlass.Action != "In Gedenkzustand versetzen" || ne.Nachlass.TwoFA != "ja" {
		t.Fatalf("Nachlass: %+v", ne.Nachlass)
	}
	ne.Nachlass = nil
	ne.UserName = "max2"
	v4.SaveEntry(ne)
	if getE(nid).Nachlass == nil {
		t.Fatal("Nachlass darf beim normalen Speichern nicht verschwinden")
	}
	ne.Nachlass = &Nachlass{}
	v4.SaveEntry(ne)
	if getE(nid).Nachlass != nil {
		t.Fatal("leere Nachlass-Angaben sollten entfernt sein")
	}
	v4.SetNachlass(`{"person":{"vorname":"Max"}}`)
	// Zum Schluss den Stand für die Gegenprobe mit pykeepass speichern
	v4.Save()
	t.Logf("Schlüssel: %s", key)
	os.WriteFile(filepath.Join(dir, "key.txt"), []byte(key), 0o600)
}
