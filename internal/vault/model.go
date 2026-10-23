package vault

import (
	"errors"
	"strconv"
	"strings"
	"time"

	kp "github.com/tobischo/gokeepasslib/v3"
)

// Field ist ein Feld eines PC-Datenblatts.
type Field struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type PC struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Notes  string  `json:"notes"`
	Fields []Field `json:"fields"`
}

type Category struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
	Root  bool   `json:"root"`
}

type Entry struct {
	ID         string   `json:"id"`
	CategoryID string   `json:"categoryId"`
	Title      string   `json:"title"`
	UserName   string   `json:"userName"`
	Password   string   `json:"password"`
	URL        string   `json:"url"`
	Notes      string   `json:"notes"`
	Devices    []string `json:"devices"` // Geräte, zu denen der Eintrag gehört
	Abo        *Abo     `json:"abo"`     // nil = kein Abo (beim Speichern: nil = unverändert)
	Modified   string   `json:"modified"`
}

// Abo sind die Angaben zu einem kostenpflichtigen Abo.
type Abo struct {
	Active   bool   `json:"active"`
	Cost     string `json:"cost"`     // z.B. "12.90"
	Currency string `json:"currency"` // CHF, EUR, USD
	Interval string `json:"interval"` // monatlich, vierteljährlich, halbjährlich, jährlich, alle 2 Jahre
	Next     string `json:"next"`     // nächste Verlängerung, JJJJ-MM-TT
	Notice   string `json:"notice"`   // Kündigungsfrist, z.B. "1 Monat"
	Payment  string `json:"payment"`  // Zahlungsart, z.B. "Visa …4411"
	Status   string `json:"status"`   // aktiv, gekündigt
	Start    string `json:"start"`    // Vertragsbeginn, JJJJ-MM-TT
	Notes    string `json:"notes"`
}

// Felder in der KeePass-Datei
var aboFields = []struct {
	key string
	get func(a *Abo) *string
}{
	{"Abo-Kosten", func(a *Abo) *string { return &a.Cost }},
	{"Abo-Währung", func(a *Abo) *string { return &a.Currency }},
	{"Abo-Intervall", func(a *Abo) *string { return &a.Interval }},
	{"Abo-Verlängerung", func(a *Abo) *string { return &a.Next }},
	{"Abo-Kündigungsfrist", func(a *Abo) *string { return &a.Notice }},
	{"Abo-Zahlungsart", func(a *Abo) *string { return &a.Payment }},
	{"Abo-Status", func(a *Abo) *string { return &a.Status }},
	{"Abo-Beginn", func(a *Abo) *string { return &a.Start }},
	{"Abo-Notizen", func(a *Abo) *string { return &a.Notes }},
}

const fieldAbo = "Abo"

func getAbo(e *kp.Entry) *Abo {
	if get(e, fieldAbo) != "ja" {
		return nil
	}
	a := &Abo{Active: true}
	for _, f := range aboFields {
		*f.get(a) = get(e, f.key)
	}
	return a
}

// setAbo speichert die Abo-Felder (oder entfernt sie) und pflegt den Tag «Abo».
func setAbo(e *kp.Entry, a *Abo) {
	tags := []string{}
	for _, t := range strings.FieldsFunc(e.Tags, func(r rune) bool { return r == ';' || r == ',' }) {
		if t = strings.TrimSpace(t); t != "" && !strings.EqualFold(t, "Abo") {
			tags = append(tags, t)
		}
	}
	if a == nil || !a.Active {
		remove(e, fieldAbo)
		for _, f := range aboFields {
			remove(e, f.key)
		}
		e.Tags = strings.Join(tags, ";")
		return
	}
	if a.Currency == "" {
		a.Currency = "CHF"
	}
	if a.Status == "" {
		a.Status = "aktiv"
	}
	a.Cost = normAmount(a.Cost)
	set(e, fieldAbo, "ja", false)
	for _, f := range aboFields {
		if v := strings.TrimSpace(*f.get(a)); v != "" {
			set(e, f.key, v, false)
		} else {
			remove(e, f.key)
		}
	}
	e.Tags = strings.Join(append(tags, "Abo"), ";")
}

// normAmount macht aus «12,9» oder «CHF 12.90» den Wert «12.90».
func normAmount(s string) string {
	s = strings.TrimSpace(strings.NewReplacer("'", "", "’", "", " ", "", "CHF", "", "EUR", "", "USD", "", "€", "", "$", "", "Fr.", "").Replace(s))
	s = strings.Replace(s, ",", ".", 1)
	if s == "" {
		return ""
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}

const (
	FieldSerial   = "Seriennummer"
	FieldCaptured = "Erfasst am"
	fieldPC       = "PC"     // alte Versionen (ein Gerät)
	fieldDevices  = "Geräte" // eine Zeile pro Gerät
)

// ---------- PCs ----------

func (v *Vault) PCs() []PC {
	g := v.pcGroup()
	if g == nil {
		return []PC{}
	}
	out := make([]PC, 0, len(g.Entries))
	for i := range g.Entries {
		out = append(out, pcFromEntry(&g.Entries[i]))
	}
	return out
}

func pcFromEntry(e *kp.Entry) PC {
	pc := PC{ID: uuidStr(e.UUID), Name: get(e, "Title"), Notes: get(e, "Notes"), Fields: []Field{}}
	for _, vd := range e.Values {
		if standardKeys[vd.Key] {
			continue
		}
		pc.Fields = append(pc.Fields, Field{Key: vd.Key, Value: vd.Value.Content})
	}
	return pc
}

// SavePC speichert ein bearbeitetes Datenblatt. Felder, die nicht mehr
// mitgeschickt werden, werden entfernt.
func (v *Vault) SavePC(pc PC) error {
	g := v.ensurePCGroup()
	idx := -1
	for i := range g.Entries {
		if uuidStr(g.Entries[i].UUID) == pc.ID {
			idx = i
		}
	}
	if idx < 0 {
		return errors.New("PC nicht gefunden")
	}
	e := &g.Entries[idx]
	if strings.TrimSpace(pc.Name) == "" {
		return errors.New("Der PC-Name darf nicht leer sein.")
	}
	oldName := get(e, "Title")
	set(e, "Title", strings.TrimSpace(pc.Name), false)
	set(e, "Notes", pc.Notes, false)
	if oldName != strings.TrimSpace(pc.Name) {
		v.renameDevice(oldName, strings.TrimSpace(pc.Name))
	}
	keep := map[string]bool{}
	for _, f := range pc.Fields {
		k := strings.TrimSpace(f.Key)
		if k == "" || standardKeys[k] {
			continue
		}
		keep[k] = true
		set(e, k, f.Value, false)
	}
	for _, vd := range append([]kp.ValueData(nil), e.Values...) {
		if !standardKeys[vd.Key] && !keep[vd.Key] {
			remove(e, vd.Key)
		}
	}
	touch(e)
	return nil
}

func (v *Vault) pcEntry(id string) *kp.Entry {
	g := v.pcGroup()
	for i := range g.Entries {
		if uuidStr(g.Entries[i].UUID) == id {
			return &g.Entries[i]
		}
	}
	return nil
}

// UpsertCaptured übernimmt automatisch erfasste Werte. Ein PC mit derselben
// Seriennummer (oder ohne brauchbare Seriennummer: demselben Namen) wird
// aktualisiert; Ergänzungen des Kunden bleiben erhalten.
// Rückgabe: ID des PCs und ob er neu angelegt wurde.
func (v *Vault) UpsertCaptured(name string, fields []Field) (string, bool) {
	g := v.ensurePCGroup()
	serial := ""
	for _, f := range fields {
		if f.Key == FieldSerial {
			serial = f.Value
		}
	}
	idx := -1
	if usableSerial(serial) {
		for i := range g.Entries {
			if strings.EqualFold(get(&g.Entries[i], FieldSerial), serial) {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		for i := range g.Entries {
			if strings.EqualFold(get(&g.Entries[i], "Title"), name) &&
				(!usableSerial(serial) || !usableSerial(get(&g.Entries[i], FieldSerial))) {
				idx = i
				break
			}
		}
	}
	created := false
	if idx < 0 {
		e := kp.NewEntry()
		e.IconID = 3
		g.Entries = append(g.Entries, e)
		idx = len(g.Entries) - 1
		created = true
	}
	e := &g.Entries[idx]
	set(e, "Title", name, false)
	// Felder der letzten Erfassung, die es jetzt nicht mehr gibt (z.B. entfernter
	// Drucker oder Netzlaufwerk), entfernen. Ergänzungen des Kunden bleiben.
	now := map[string]bool{}
	for _, f := range fields {
		now[f.Key] = true
	}
	for _, k := range strings.Split(getCustom(e, customAuto), "|") {
		if k != "" && !now[k] && !standardKeys[k] {
			remove(e, k)
		}
	}
	keys := make([]string, 0, len(fields))
	for _, f := range fields {
		set(e, f.Key, f.Value, false)
		keys = append(keys, f.Key)
	}
	setCustom(e, customAuto, strings.Join(keys, "|"))
	set(e, FieldCaptured, time.Now().Format("02.01.2006 15:04"), false)
	touch(e)
	return uuidStr(e.UUID), created
}

func usableSerial(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 4 {
		return false
	}
	for _, bad := range []string{"to be filled", "default string", "system serial", "not specified", "none", "0123456789", "chassis serial"} {
		if strings.Contains(s, bad) {
			return false
		}
	}
	return true
}

func (v *Vault) DeletePC(id string) error {
	g := v.pcGroup()
	if g == nil {
		return errors.New("PC nicht gefunden")
	}
	for i := range g.Entries {
		if uuidStr(g.Entries[i].UUID) == id {
			name := get(&g.Entries[i], "Title")
			v.addDeleted(g.Entries[i].UUID)
			g.Entries = append(g.Entries[:i], g.Entries[i+1:]...)
			v.renameDevice(name, "") // aus den Passwort-Einträgen entfernen
			return nil
		}
	}
	return errors.New("PC nicht gefunden")
}

// ---------- Kategorien ----------

func (v *Vault) Categories() []Category {
	out := []Category{}
	r := v.root()
	var walk func(g *kp.Group, prefix string)
	walk = func(g *kp.Group, prefix string) {
		for i := range g.Groups {
			c := &g.Groups[i]
			if v.isHidden(c) || v.isDevParent(c) {
				continue
			}
			name := c.Name
			if prefix != "" {
				name = prefix + " / " + c.Name
			}
			out = append(out, Category{ID: uuidStr(c.UUID), Name: name, Count: len(c.Entries)})
			walk(c, name)
		}
	}
	walk(r, "")
	// Einträge direkt in der Wurzelgruppe = «Ohne Kategorie» (immer zuletzt)
	out = append(out, Category{ID: uuidStr(r.UUID), Name: NoCategory, Count: len(r.Entries), Root: true})
	return out
}

const NoCategory = "Ohne Kategorie"

// ---------- Geräte eines Eintrags ----------

func cleanDevices(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, d := range in {
		d = strings.TrimSpace(d)
		if d != "" && !seen[strings.ToLower(d)] {
			seen[strings.ToLower(d)] = true
			out = append(out, d)
		}
	}
	return out
}

func getDevices(e *kp.Entry) []string {
	return cleanDevices(strings.Split(get(e, fieldDevices), "\n"))
}

// setDevices speichert die Geräte als Feld «Geräte» und zusätzlich als KeePass-Tags
// (andere Tags bleiben erhalten), damit man in KeePassXC danach filtern kann.
func setDevices(e *kp.Entry, devs []string) {
	old := map[string]bool{}
	for _, d := range getDevices(e) {
		old[strings.ToLower(d)] = true
	}
	devs = cleanDevices(devs)
	if len(devs) == 0 {
		remove(e, fieldDevices)
	} else {
		set(e, fieldDevices, strings.Join(devs, "\n"), false)
	}
	tags := []string{}
	for _, t := range strings.FieldsFunc(e.Tags, func(r rune) bool { return r == ';' || r == ',' }) {
		t = strings.TrimSpace(t)
		if t != "" && !old[strings.ToLower(t)] {
			tags = append(tags, t)
		}
	}
	e.Tags = strings.Join(append(tags, devs...), ";")
}

// renameDevice ändert (oder entfernt, wenn neu leer) einen Gerätenamen in allen Einträgen.
func (v *Vault) renameDevice(oldName, newName string) {
	var walk func(g *kp.Group)
	walk = func(g *kp.Group) {
		for i := range g.Entries {
			e := &g.Entries[i]
			devs := getDevices(e)
			changed := false
			for j, d := range devs {
				if strings.EqualFold(d, oldName) {
					devs[j] = newName
					changed = true
				}
			}
			if changed {
				setDevices(e, devs)
			}
		}
		for i := range g.Groups {
			if !v.isHidden(&g.Groups[i]) {
				walk(&g.Groups[i])
			}
		}
	}
	walk(v.root())
}

func (v *Vault) resolveCategory(id string) *kp.Group {
	g := findGroup(v.root(), id)
	if g == nil || v.isHidden(g) || v.isDevParent(g) {
		return nil
	}
	return g
}

// migrateDevices übernimmt Gerätezuordnungen älterer Versionen:
// Feld «PC» (bis 2.0) und Gruppe «Geräte-Passwörter» (2.1).
func (v *Vault) migrateDevices() {
	var walk func(g *kp.Group)
	walk = func(g *kp.Group) {
		for i := range g.Entries {
			e := &g.Entries[i]
			if pc := strings.TrimSpace(get(e, fieldPC)); pc != "" {
				setDevices(e, append(getDevices(e), pc))
				remove(e, fieldPC)
			}
		}
		for i := range g.Groups {
			if !v.isHidden(&g.Groups[i]) {
				walk(&g.Groups[i])
			}
		}
	}
	walk(v.root())
	dg := v.devGroup()
	if dg == nil {
		return
	}
	// Einträge aus den Gerätegruppen in eine normale Kategorie verschieben
	var moved []kp.Entry
	for i := range dg.Groups {
		for _, e := range dg.Groups[i].Entries {
			setDevices(&e, append(getDevices(&e), dg.Groups[i].Name))
			moved = append(moved, e)
		}
	}
	moved = append(moved, dg.Entries...)
	dgID := uuidStr(dg.UUID)
	// Gruppe entfernen
	var del func(parent *kp.Group) bool
	del = func(parent *kp.Group) bool {
		for i := range parent.Groups {
			if uuidStr(parent.Groups[i].UUID) == dgID {
				parent.Groups = append(parent.Groups[:i], parent.Groups[i+1:]...)
				return true
			}
			if del(&parent.Groups[i]) {
				return true
			}
		}
		return false
	}
	del(v.root())
	v.setMeta(metaDevGrp, "")
	if len(moved) == 0 {
		return
	}
	target := ""
	for _, c := range v.Categories() {
		if strings.Contains(strings.ToLower(c.Name), "windows") {
			target = c.ID
			break
		}
	}
	if target == "" {
		if cs := v.Categories(); len(cs) > 1 {
			target = cs[0].ID
		} else {
			target, _ = v.AddCategory("Windows-Anmeldungen")
		}
	}
	g := findGroup(v.root(), target)
	g.Entries = append(g.Entries, moved...)
}

func (v *Vault) AddCategory(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("Bitte einen Namen eingeben.")
	}
	r := v.root()
	for i := range r.Groups {
		if strings.EqualFold(r.Groups[i].Name, name) && !v.isHidden(&r.Groups[i]) {
			return "", errors.New("Diese Kategorie gibt es schon.")
		}
	}
	g := kp.NewGroup()
	g.Name = name
	r.Groups = append(r.Groups, g)
	return uuidStr(g.UUID), nil
}

func (v *Vault) RenameCategory(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("Bitte einen Namen eingeben.")
	}
	g := findGroup(v.root(), id)
	if g == nil || g == v.root() || v.isHidden(g) || v.isDevParent(g) {
		return errors.New("Diese Kategorie kann nicht umbenannt werden.")
	}
	g.Name = name
	return nil
}

func (v *Vault) DeleteCategory(id string) error {
	var del func(parent *kp.Group) bool
	var err error
	del = func(parent *kp.Group) bool {
		for i := range parent.Groups {
			c := &parent.Groups[i]
			if uuidStr(c.UUID) == id {
				if v.isHidden(c) || v.isDevParent(c) {
					err = errors.New("Diese Gruppe kann nicht gelöscht werden.")
					return true
				}
				if len(c.Entries) > 0 || len(c.Groups) > 0 {
					err = errors.New("Die Kategorie ist nicht leer. Bitte zuerst die Einträge löschen oder verschieben.")
					return true
				}
				v.addDeleted(c.UUID)
				parent.Groups = append(parent.Groups[:i], parent.Groups[i+1:]...)
				return true
			}
			if del(c) {
				return true
			}
		}
		return false
	}
	if !del(v.root()) {
		return errors.New("Kategorie nicht gefunden")
	}
	return err
}

// ---------- Passwort-Einträge ----------

func (v *Vault) Entries() []Entry {
	out := []Entry{}
	r := v.root()
	add := func(g *kp.Group) {
		for i := range g.Entries {
			out = append(out, entryFrom(&g.Entries[i], uuidStr(g.UUID)))
		}
	}
	add(r)
	var walk func(g *kp.Group)
	walk = func(g *kp.Group) {
		for i := range g.Groups {
			c := &g.Groups[i]
			if v.isHidden(c) || v.isDevParent(c) {
				continue
			}
			add(c)
			walk(c)
		}
	}
	walk(r)
	return out
}

func entryFrom(e *kp.Entry, cat string) Entry {
	mod := ""
	if e.Times.LastModificationTime != nil {
		mod = e.Times.LastModificationTime.Time.Local().Format("02.01.2006")
	}
	return Entry{
		ID: uuidStr(e.UUID), CategoryID: cat,
		Title: get(e, "Title"), UserName: get(e, "UserName"), Password: get(e, "Password"),
		URL: get(e, "URL"), Notes: get(e, "Notes"), Devices: getDevices(e), Abo: getAbo(e), Modified: mod,
	}
}

// SaveEntry legt einen Eintrag an (ID leer) oder ändert ihn. Gibt die ID zurück.
func (v *Vault) SaveEntry(in Entry) (string, error) {
	if strings.TrimSpace(in.Title) == "" {
		return "", errors.New("Bitte einen Titel eingeben.")
	}
	target := v.resolveCategory(in.CategoryID)
	if target == nil {
		return "", errors.New("Bitte eine Kategorie wählen.")
	}
	targetID := uuidStr(target.UUID)
	var e *kp.Entry
	if in.ID != "" {
		g, idx := findEntry(v.root(), in.ID)
		if g == nil || v.isHidden(g) {
			return "", errors.New("Eintrag nicht gefunden")
		}
		if uuidStr(g.UUID) != targetID {
			moved := g.Entries[idx]
			g.Entries = append(g.Entries[:idx], g.Entries[idx+1:]...)
			moved.Times.LocationChanged = moved.Times.LastModificationTime
			target = findGroup(v.root(), targetID)
			target.Entries = append(target.Entries, moved)
			e = &target.Entries[len(target.Entries)-1]
		} else {
			e = &g.Entries[idx]
		}
	} else {
		target.Entries = append(target.Entries, kp.NewEntry())
		e = &target.Entries[len(target.Entries)-1]
	}
	set(e, "Title", strings.TrimSpace(in.Title), false)
	set(e, "UserName", in.UserName, false)
	set(e, "Password", in.Password, true)
	set(e, "URL", in.URL, false)
	set(e, "Notes", in.Notes, false)
	remove(e, fieldPC)
	setDevices(e, in.Devices)
	if in.Abo != nil {
		setAbo(e, in.Abo)
	}
	touch(e)
	return uuidStr(e.UUID), nil
}

func (v *Vault) DeleteEntry(id string) error {
	g, idx := findEntry(v.root(), id)
	if g == nil || v.isHidden(g) {
		return errors.New("Eintrag nicht gefunden")
	}
	v.addDeleted(g.Entries[idx].UUID)
	g.Entries = append(g.Entries[:idx], g.Entries[idx+1:]...)
	return nil
}

// UpsertWifi legt das WLAN als Passwort-Eintrag «WLAN <Name>» an oder aktualisiert es.
// Ein leeres Passwort überschreibt kein vorhandenes.
func (v *Vault) UpsertWifi(ssid, password, pcName string) (bool, error) {
	ssid = strings.TrimSpace(ssid)
	if ssid == "" {
		return false, nil
	}
	title := "WLAN " + ssid
	for _, e := range v.Entries() {
		if strings.EqualFold(e.Title, title) || strings.EqualFold(e.UserName, ssid) && strings.HasPrefix(strings.ToUpper(e.Title), "WLAN") {
			if password == "" || e.Password == password {
				return false, nil
			}
			e.Password = password
			_, err := v.SaveEntry(e)
			return true, err
		}
	}
	cat := ""
	for _, c := range v.Categories() {
		if strings.Contains(strings.ToLower(c.Name), "wlan") {
			cat = c.ID
			break
		}
	}
	if cat == "" {
		id, err := v.AddCategory("WLAN & Netzwerk")
		if err != nil {
			return false, err
		}
		cat = id
	}
	notes := "Automatisch erfasst auf " + pcName + "."
	if password == "" {
		notes += " Das Passwort konnte nicht ausgelesen werden (evtl. Administratorrechte nötig). Bitte ergänzen."
	}
	_, err := v.SaveEntry(Entry{CategoryID: cat, Title: title, UserName: ssid, Password: password, Notes: notes})
	return true, err
}

// customAuto merkt sich (unsichtbar in den Zusatzdaten des Eintrags),
// welche Felder automatisch erfasst wurden.
const customAuto = "DM.AutoFelder"

func getCustom(e *kp.Entry, key string) string {
	for _, c := range e.CustomData {
		if c.Key == key {
			return c.Value
		}
	}
	return ""
}

func setCustom(e *kp.Entry, key, val string) {
	for i := range e.CustomData {
		if e.CustomData[i].Key == key {
			e.CustomData[i].Value = val
			return
		}
	}
	e.CustomData = append(e.CustomData, kp.CustomData{Key: key, Value: val})
}

// ImportRow ist eine Zeile aus Excel oder einer CSV-Datei.
type ImportRow struct {
	Category string `json:"category"` // Name; leer = DefaultCategoryID
	Title    string `json:"title"`
	UserName string `json:"userName"`
	Password string `json:"password"`
	URL      string `json:"url"`
	Notes    string `json:"notes"`
	PC       string `json:"pc"` // ein oder mehrere Geräte, getrennt durch , ; /
}

// Import legt mehrere Einträge an. Fehlende Kategorien werden erstellt.
// Mit skipDup werden Einträge übersprungen, die es (Titel, Benutzer, Passwort) schon gibt.
// Rückgabe: angelegt, übersprungen.
func (v *Vault) Import(rows []ImportRow, defaultCat string, skipDup bool) (int, int, error) {
	seen := map[string]bool{}
	key := func(t, u, p string) string {
		return strings.ToLower(strings.TrimSpace(t)) + "\x00" + strings.TrimSpace(u) + "\x00" + p
	}
	for _, e := range v.Entries() {
		seen[key(e.Title, e.UserName, e.Password)] = true
	}
	catByName := map[string]string{}
	for _, c := range v.Categories() {
		catByName[strings.ToLower(c.Name)] = c.ID
	}
	if defaultCat == "" {
		defaultCat = uuidStr(v.root().UUID)
	}
	added, skipped := 0, 0
	for _, r := range rows {
		title := strings.TrimSpace(r.Title)
		if title == "" {
			title = hostOf(r.URL)
		}
		if title == "" {
			title = strings.TrimSpace(r.UserName)
		}
		if title == "" && r.Password == "" {
			continue // leere Zeile
		}
		if title == "" {
			title = "Ohne Bezeichnung"
		}
		k := key(title, r.UserName, r.Password)
		if skipDup && seen[k] {
			skipped++
			continue
		}
		cat := defaultCat
		if c := strings.TrimSpace(r.Category); c != "" {
			if id, ok := catByName[strings.ToLower(c)]; ok {
				cat = id
			} else {
				id, err := v.AddCategory(c)
				if err != nil {
					return added, skipped, err
				}
				catByName[strings.ToLower(c)] = id
				cat = id
			}
		}
		devs := strings.FieldsFunc(r.PC, func(c rune) bool { return c == ',' || c == ';' || c == '/' || c == '\n' })
		if _, err := v.SaveEntry(Entry{CategoryID: cat, Title: title, UserName: r.UserName, Password: r.Password, URL: strings.TrimSpace(r.URL), Notes: r.Notes, Devices: devs}); err != nil {
			return added, skipped, err
		}
		seen[k] = true
		added++
	}
	return added, skipped, nil
}

func hostOf(u string) string {
	u = strings.TrimSpace(u)
	for _, p := range []string{"https://", "http://"} {
		u = strings.TrimPrefix(u, p)
	}
	u = strings.TrimPrefix(u, "www.")
	if i := strings.IndexAny(u, "/?#:"); i >= 0 {
		u = u[:i]
	}
	return u
}
