// Package vault speichert alle Daten in einer KeePass-Datei (KDBX 4).
//
// Aufbau der Datei:
//
//	StickTresor                   (Wurzelgruppe)
//	├── PC-Datenblätter           (ein Eintrag pro PC, Werte als Zusatzfelder)
//	├── Windows-Anmeldungen       (Passwort-Kategorien = alle anderen Gruppen)
//	├── E-Mail
//	└── ...
//
// Die Datei kann jederzeit mit KeePass, KeePassXC, KeePassDX oder Strongbox
// geöffnet werden. Gruppen, Einträge und Felder, die dort angelegt werden,
// bleiben beim Speichern durch diese App erhalten.
package vault

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	kp "github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

const (
	FileName         = "daten.kdbx"
	RecoveryFileName = "wiederherstellung.kdbx"

	ModeHandover = "uebergabe"        // IT-Partner erfasst PCs mit seinem Übergabe-Passwort
	ModePending  = "kunde-ausstehend" // übergeben, Kunde muss Passwort festlegen
	ModeCustomer = "kunde"            // Kunde hat eigenes Master-Passwort

	// Schlüssel in Meta/CustomData. Ältere Dateien verwenden das Präfix "DM." (wird weiter gelesen).
	metaMode    = "ST.Modus"
	metaPCGroup = "ST.PCGruppe"
	metaDevGrp  = "ST.GeraetePasswoerter" // Gruppe mit einer Untergruppe pro Gerät (nur Migration)
	metaRecKey  = "ST.Wiederherstellung"  // Schlüssel liegt verschlüsselt in der Datei, damit die Kopie aktuell bleibt
	metaPartner = "ST.Partner"            // geschützte Kopie von partner.json
	metaLegacy  = "ST.Nachlass"           // Angaben für den digitalen Nachlass (JSON)

	rootName     = "StickTresor"
	pcGroupName  = "PC-Datenblätter"
	devGroupName = "Geräte-Passwörter"

	// Argon2d wie KeePass/KeePassXC-Standard: maximale Kompatibilität.
	kdfMemory      = 64 * 1024 * 1024
	kdfIterations  = 10
	kdfParallelism = 2
)

var DefaultCategories = []string{"Windows-Anmeldungen", "E-Mail", "WLAN & Netzwerk", "Webseiten & Geräte"}

var ErrWrongPassword = errors.New("Das Passwort ist falsch.")

type Vault struct {
	db        *kp.Database
	path      string
	recovery  *kp.DBCredentials // nil = keine Wiederherstellungsdatei schreiben
	recPath   string
	lastSaved time.Time
}

// ---------- Anlegen / Öffnen / Speichern ----------

func Exists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, FileName))
	return err == nil
}

func RecoveryExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, RecoveryFileName))
	return err == nil
}

// Create legt einen neuen Tresor im Übergabe-Modus an, verschlüsselt mit password.
func Create(dir, password string) (*Vault, error) {
	if password == "" {
		return nil, errors.New("Kein Passwort angegeben.")
	}
	db := kp.NewDatabase(kp.WithDatabaseKDBXVersion4())
	db.Content.Meta.DatabaseName = rootName
	db.Content.Meta.Generator = "StickTresor"
	db.Content.Meta.RecycleBinEnabled = w.NewBoolWrapper(false)
	db.Credentials = kp.NewPasswordCredentials(password)

	root := kp.NewGroup()
	root.Name = rootName
	pcs := kp.NewGroup()
	pcs.Name = pcGroupName
	pcs.IconID = 3 // Netzwerk/Computer-Symbol in KeePass
	root.Groups = append(root.Groups, pcs)
	for _, name := range DefaultCategories {
		g := kp.NewGroup()
		g.Name = name
		root.Groups = append(root.Groups, g)
	}
	db.Content.Root.Groups = []kp.Group{root}

	v := &Vault{db: db, path: filepath.Join(dir, FileName), recPath: filepath.Join(dir, RecoveryFileName)}
	v.setMeta(metaPCGroup, uuidStr(pcs.UUID))
	v.setMeta(metaMode, ModeHandover)
	if err := v.Save(); err != nil {
		return nil, err
	}
	return v, nil
}

// Open öffnet daten.kdbx mit dem Passwort.
func Open(dir, password string) (*Vault, error) {
	return openFile(dir, filepath.Join(dir, FileName), kp.NewPasswordCredentials(password))
}

// OpenRecovery öffnet wiederherstellung.kdbx mit dem Wiederherstellungsschlüssel.
// Danach muss mit SetPassword ein neues Master-Passwort gesetzt und gespeichert werden.
func OpenRecovery(dir, key string) (*Vault, error) {
	creds := kp.NewPasswordCredentials(CanonicalRecoveryKey(key))
	v, err := openFile(dir, filepath.Join(dir, RecoveryFileName), creds)
	if errors.Is(err, ErrWrongPassword) {
		// Dateien bis Version 4.1: Schlüssel ohne Bindestriche als Passwort
		v, err = openFile(dir, filepath.Join(dir, RecoveryFileName), kp.NewPasswordCredentials(NormalizeRecoveryKey(key)))
	}
	if err != nil {
		return nil, err
	}
	v.recovery = creds // beim nächsten Speichern mit dem Schlüssel wie auf dem Ausdruck
	return v, nil
}

func openFile(dir, path string, creds *kp.DBCredentials) (*Vault, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("Datei kann nicht geöffnet werden: %w", err)
	}
	defer f.Close()
	db := kp.NewDatabase()
	db.Credentials = creds
	if err := kp.NewDecoder(f).Decode(db); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "credentials") ||
			strings.Contains(strings.ToLower(err.Error()), "hmac") ||
			strings.Contains(strings.ToLower(err.Error()), "hash") {
			return nil, ErrWrongPassword
		}
		return nil, fmt.Errorf("Die Datei ist beschädigt oder kein KeePass-Format: %w", err)
	}
	if err := db.UnlockProtectedEntries(); err != nil {
		return nil, err
	}
	v := &Vault{db: db, path: filepath.Join(dir, FileName), recPath: filepath.Join(dir, RecoveryFileName)}
	if len(db.Content.Root.Groups) == 0 {
		g := kp.NewGroup()
		g.Name = rootName
		db.Content.Root.Groups = []kp.Group{g}
	}
	v.ensurePCGroup()
	v.migrateDevices()
	if k := v.getMeta(metaRecKey); k != "" {
		v.recovery = kp.NewPasswordCredentials(CanonicalRecoveryKey(k))
	}
	return v, nil
}

// Save schreibt daten.kdbx und, falls eingerichtet, wiederherstellung.kdbx.
func (v *Vault) Save() error {
	if err := v.writeTo(v.path, v.db.Credentials); err != nil {
		return err
	}
	if v.recovery != nil {
		if err := v.writeTo(v.recPath, v.recovery); err != nil {
			return fmt.Errorf("Wiederherstellungsdatei: %w", err)
		}
	}
	v.lastSaved = time.Now()
	return nil
}

func (v *Vault) writeTo(path string, creds *kp.DBCredentials) error {
	db := v.db
	main := db.Credentials
	db.Credentials = creds
	defer func() { db.Credentials = main }()

	// Bei jedem Speichern frische Zufallswerte (Seed, IV, Salt, innerer Schlüssel),
	// damit nie ein Schlüsselstrom wiederverwendet wird.
	fh := db.Header.FileHeaders
	fh.MasterSeed = randBytes(32)
	if bytes.Equal(fh.CipherID, kp.CipherChaCha20) {
		fh.EncryptionIV = randBytes(12)
	} else {
		fh.EncryptionIV = randBytes(16)
	}
	if fh.KdfParameters == nil {
		fh.KdfParameters = &kp.KdfParameters{}
	}
	kdf := fh.KdfParameters
	if !bytes.Equal(kdf.UUID, kp.KdfArgon2) && !bytes.Equal(kdf.UUID, kp.KdfArgon2id) {
		kdf.UUID = kp.KdfArgon2
	}
	if kdf.Memory < kdfMemory {
		kdf.Memory = kdfMemory
	}
	if kdf.Iterations < kdfIterations {
		kdf.Iterations = kdfIterations
	}
	if kdf.Parallelism == 0 {
		kdf.Parallelism = kdfParallelism
	}
	if kdf.Version == 0 {
		kdf.Version = 0x13
	}
	copy(kdf.Salt[:], randBytes(32))

	// Geschützte Werte mit neuem innerem Schlüssel verschlüsseln.
	if db.Content.InnerHeader != nil {
		db.Content.InnerHeader.InnerRandomStreamID = kp.ChaChaStreamID
		db.Content.InnerHeader.InnerRandomStreamKey = randBytes(64)
	}
	if err := db.LockProtectedEntries(); err != nil {
		return err
	}
	var buf bytes.Buffer
	encErr := kp.NewEncoder(&buf).Encode(db)
	if err := db.UnlockProtectedEntries(); err != nil && encErr == nil {
		encErr = err
	}
	if encErr != nil {
		return encErr
	}
	return atomicWrite(path, buf.Bytes())
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// ---------- Modus, Passwörter, Wiederherstellung ----------

func (v *Vault) Mode() string {
	m := v.getMeta(metaMode)
	if m == "" {
		return ModeCustomer // fremde KeePass-Datei oder ältere Version
	}
	return m
}

func (v *Vault) SetMode(m string) { v.setMeta(metaMode, m) }

func (v *Vault) LastSaved() time.Time { return v.lastSaved }

// SetPassword setzt ein neues Master-Passwort (wird erst mit Save wirksam).
func (v *Vault) SetPassword(pw string) {
	v.db.Credentials = kp.NewPasswordCredentials(pw)
	now := w.Now()
	v.db.Content.Meta.MasterKeyChanged = &now
}

// CheckPassword prüft, ob pw das aktuelle Master-Passwort ist.
func (v *Vault) CheckPassword(pw string) bool {
	c := kp.NewPasswordCredentials(pw)
	return bytes.Equal(c.Passphrase, v.db.Credentials.Passphrase)
}

// SetRecoveryKey richtet die Wiederherstellungsdatei mit diesem Schlüssel ein.
func (v *Vault) SetRecoveryKey(key string) {
	v.recovery = kp.NewPasswordCredentials(CanonicalRecoveryKey(key))
	v.setMeta(metaRecKey, key)
}

func (v *Vault) HasRecovery() bool { return v.recovery != nil }

// Nachlass liefert die allgemeinen Nachlass-Angaben (JSON) oder "".
func (v *Vault) Nachlass() string { return v.getMeta(metaLegacy) }

// SetNachlass speichert die allgemeinen Nachlass-Angaben (JSON).
func (v *Vault) SetNachlass(js string) { v.setMeta(metaLegacy, js) }

// Partner liefert die in der Datei gespeicherte Kopie der Partner-Angaben (JSON) oder "".
func (v *Vault) Partner() string { return v.getMeta(metaPartner) }

// SetPartner speichert die Partner-Angaben verschlüsselt in der Datei ("" entfernt sie).
func (v *Vault) SetPartner(js string) { v.setMeta(metaPartner, js) }

// NewRecoveryKey erzeugt einen gut lesbaren Schlüssel wie RK-7K4Q-9XWP-2MRT-H8VC-LN3B.
func NewRecoveryKey() string {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ" // ohne 0/O, 1/I
	b := randBytes(24)
	var sb strings.Builder
	sb.WriteString("RK")
	for i := 0; i < 24; i++ {
		if i%4 == 0 {
			sb.WriteByte('-')
		}
		sb.WriteByte(alphabet[int(b[i])%len(alphabet)])
	}
	return sb.String()
}

// CanonicalRecoveryKey bringt eine Eingabe in die Form des Ausdrucks (RK-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX).
// Diese Form ist auch das Passwort von wiederherstellung.kdbx, damit sich die Datei in KeePass
// mit dem Schlüssel genau so öffnen lässt, wie er auf dem Papier steht.
func CanonicalRecoveryKey(k string) string {
	n := NormalizeRecoveryKey(k)
	if len(n) != 26 {
		return n
	}
	var sb strings.Builder
	sb.WriteString(n[:2])
	for i := 2; i < len(n); i += 4 {
		sb.WriteByte('-')
		sb.WriteString(n[i : i+4])
	}
	return sb.String()
}

// NormalizeRecoveryKey macht die Eingabe unabhängig von Gross/Klein, Leerzeichen und Bindestrichen.
func NormalizeRecoveryKey(k string) string {
	k = strings.ToUpper(k)
	var sb strings.Builder
	for _, r := range k {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	s := sb.String()
	s = strings.ReplaceAll(s, "O", "0")
	s = strings.ReplaceAll(s, "I", "1")
	return s
}

// ---------- Hilfen: Gruppen ----------

func (v *Vault) root() *kp.Group { return &v.db.Content.Root.Groups[0] }

func (v *Vault) getMeta(key string) string {
	legacy := "DM." + strings.TrimPrefix(key, "ST.")
	old := ""
	for _, c := range v.db.Content.Meta.CustomData {
		if c.Key == key {
			return c.Value
		}
		if c.Key == legacy {
			old = c.Value
		}
	}
	return old
}

func (v *Vault) setMeta(key, val string) {
	// alten Schlüssel (Präfix "DM.") entfernen
	legacy := "DM." + strings.TrimPrefix(key, "ST.")
	cd := v.db.Content.Meta.CustomData[:0]
	for _, c := range v.db.Content.Meta.CustomData {
		if c.Key != legacy {
			cd = append(cd, c)
		}
	}
	v.db.Content.Meta.CustomData = cd
	for i := range cd {
		if cd[i].Key == key {
			cd[i].Value = val
			return
		}
	}
	v.db.Content.Meta.CustomData = append(cd, kp.CustomData{Key: key, Value: val})
}

func (v *Vault) pcGroup() *kp.Group {
	id := v.getMeta(metaPCGroup)
	if id != "" {
		if g := findGroup(v.root(), id); g != nil {
			return g
		}
	}
	r := v.root()
	for i := range r.Groups {
		if r.Groups[i].Name == pcGroupName {
			v.setMeta(metaPCGroup, uuidStr(r.Groups[i].UUID))
			return &r.Groups[i]
		}
	}
	return nil
}

func (v *Vault) ensurePCGroup() *kp.Group {
	if g := v.pcGroup(); g != nil {
		return g
	}
	r := v.root()
	g := kp.NewGroup()
	g.Name = pcGroupName
	g.IconID = 3
	r.Groups = append([]kp.Group{g}, r.Groups...)
	v.setMeta(metaPCGroup, uuidStr(g.UUID))
	return &r.Groups[0]
}

// devGroup ist die Gruppe «Geräte-Passwörter» (nil, wenn noch nicht angelegt).
func (v *Vault) devGroup() *kp.Group {
	if id := v.getMeta(metaDevGrp); id != "" {
		if g := findGroup(v.root(), id); g != nil {
			return g
		}
	}
	return nil
}

func (v *Vault) isDevParent(g *kp.Group) bool {
	id := v.getMeta(metaDevGrp)
	return id != "" && uuidStr(g.UUID) == id
}

// deviceGroup liefert die Passwort-Gruppe eines Geräts und legt sie bei Bedarf an.
// Achtung: Das Anlegen kann Zeiger auf andere Gruppen ungültig machen.
func (v *Vault) deviceGroup(name string, create bool) *kp.Group {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	dg := v.devGroup()
	if dg == nil {
		if !create {
			return nil
		}
		r := v.root()
		g := kp.NewGroup()
		g.Name = devGroupName
		g.IconID = 3
		r.Groups = append(r.Groups, g)
		v.setMeta(metaDevGrp, uuidStr(g.UUID))
		dg = &r.Groups[len(r.Groups)-1]
	}
	for i := range dg.Groups {
		if strings.EqualFold(dg.Groups[i].Name, name) {
			return &dg.Groups[i]
		}
	}
	if !create {
		return nil
	}
	g := kp.NewGroup()
	g.Name = name
	g.IconID = 3
	dg.Groups = append(dg.Groups, g)
	return &dg.Groups[len(dg.Groups)-1]
}

func (v *Vault) isHidden(g *kp.Group) bool {
	id := uuidStr(g.UUID)
	if id == v.getMeta(metaPCGroup) {
		return true
	}
	rb := v.db.Content.Meta.RecycleBinUUID
	return rb != (kp.UUID{}) && rb.Compare(g.UUID)
}

func findGroup(g *kp.Group, id string) *kp.Group {
	if uuidStr(g.UUID) == id {
		return g
	}
	for i := range g.Groups {
		if r := findGroup(&g.Groups[i], id); r != nil {
			return r
		}
	}
	return nil
}

// findEntry sucht einen Eintrag in der ganzen Datei.
func findEntry(g *kp.Group, id string) (*kp.Group, int) {
	for i := range g.Entries {
		if uuidStr(g.Entries[i].UUID) == id {
			return g, i
		}
	}
	for i := range g.Groups {
		if pg, idx := findEntry(&g.Groups[i], id); pg != nil {
			return pg, idx
		}
	}
	return nil, -1
}

func (v *Vault) addDeleted(u kp.UUID) {
	now := w.Now()
	v.db.Content.Root.DeletedObjects = append(v.db.Content.Root.DeletedObjects, kp.DeletedObjectData{UUID: u, DeletionTime: &now})
}

// ---------- Felder ----------

func get(e *kp.Entry, key string) string { return e.GetContent(key) }

func set(e *kp.Entry, key, val string, protected bool) {
	for i := range e.Values {
		if e.Values[i].Key == key {
			e.Values[i].Value.Content = val
			if protected {
				e.Values[i].Value.Protected = w.NewBoolWrapper(true)
			}
			return
		}
	}
	vd := kp.ValueData{Key: key, Value: kp.V{Content: val}}
	if protected {
		vd.Value.Protected = w.NewBoolWrapper(true)
	}
	e.Values = append(e.Values, vd)
}

func remove(e *kp.Entry, key string) {
	out := e.Values[:0]
	for _, vd := range e.Values {
		if vd.Key != key {
			out = append(out, vd)
		}
	}
	e.Values = out
}

func touch(e *kp.Entry) {
	now := w.Now()
	e.Times.LastModificationTime = &now
}

func uuidStr(u kp.UUID) string {
	b, _ := u.MarshalText()
	return string(b)
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

var standardKeys = map[string]bool{"Title": true, "UserName": true, "Password": true, "URL": true, "Notes": true}

func sortStrings(s []string) { sort.Strings(s) }
