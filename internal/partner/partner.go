// Package partner verwaltet die Angaben der IT-Firma, die den Stick einrichtet
// (Name, Adresse, Support, Logo, Farbe). Sie liegen in partner.json neben der Exe,
// damit im Programmcode keine Firmendaten stehen und die signierte Exe unverändert bleibt.
// Beim Anlegen des Tresors wird eine Kopie verschlüsselt in der KeePass-Datei abgelegt.
package partner

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const FileName = "partner.json"

// DefaultAppName wird verwendet, wenn keine Partner-Datei vorhanden ist.
const DefaultAppName = "StickTresor"

// DefaultColor ist die Akzentfarbe ohne Partner-Datei.
const DefaultColor = "#1F6FB2"

type Partner struct {
	AppName      string `json:"appName"` // z.B. "[D]er Passwort[M]anager": Buchstaben in [] in Akzentfarbe
	Firma        string `json:"firma"`
	Strasse      string `json:"strasse"`
	Ort          string `json:"ort"`
	Tel          string `json:"tel"`
	Mail         string `json:"mail"`
	Web          string `json:"web"`
	SupportTitel string `json:"supportTitel"` // Überschrift der Karte, z.B. "Ihr EDV Partner"
	Helpdesk     string `json:"helpdesk"`     // Telefonnummer
	HelpdeskInfo string `json:"helpdeskInfo"` // z.B. "ab Festnetz CHF 2.83/Min. (1 Minute gratis)"
	SupportInfo  string `json:"supportInfo"`  // freier Text, z.B. Öffnungszeiten
	Color        string `json:"color"`        // #RRGGBB
	Logo         string `json:"logo"`         // data:image/...;base64,...
}

var (
	reColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	reLogo  = regexp.MustCompile(`^data:image/(png|jpeg|webp|svg\+xml);base64,[A-Za-z0-9+/=]+$`)
)

const maxLogo = 1500 * 1024

// Clean kürzt Leerzeichen und prüft die Angaben.
func (p *Partner) Clean() error {
	for _, s := range []*string{&p.AppName, &p.Firma, &p.Strasse, &p.Ort, &p.Tel, &p.Mail, &p.Web,
		&p.SupportTitel, &p.Helpdesk, &p.HelpdeskInfo, &p.SupportInfo, &p.Color} {
		*s = strings.TrimSpace(*s)
		if len(*s) > 300 {
			*s = (*s)[:300]
		}
	}
	p.Web = strings.TrimPrefix(strings.TrimPrefix(p.Web, "https://"), "http://")
	p.Web = strings.TrimSuffix(p.Web, "/")
	if p.Firma == "" {
		return errors.New("Bitte den Firmennamen angeben.")
	}
	if p.Color == "" {
		p.Color = DefaultColor
	}
	if !reColor.MatchString(p.Color) {
		return errors.New("Die Farbe muss im Format #RRGGBB angegeben werden.")
	}
	p.Color = strings.ToUpper(p.Color)
	if p.Logo != "" {
		if len(p.Logo) > maxLogo || !reLogo.MatchString(p.Logo) {
			return errors.New("Das Logo muss ein Bild (PNG, JPG, WebP oder SVG) unter 1 MB sein.")
		}
	}
	return nil
}

// Load liest partner.json. Fehlt die Datei, ist das Ergebnis nil (neutrale App).
func Load(dir string) (*Partner, error) {
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

func Parse(b []byte) (*Partner, error) {
	var p Partner
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, errors.New("partner.json ist beschädigt.")
	}
	if err := p.Clean(); err != nil {
		return nil, err
	}
	return &p, nil
}

func (p *Partner) JSON() []byte {
	b, _ := json.MarshalIndent(p, "", "  ")
	return b
}

// Save schreibt partner.json (atomar).
func Save(dir string, p *Partner) error {
	path := filepath.Join(dir, FileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, p.JSON(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Title liefert den App-Namen ohne Markierungen, z.B. für den Fenstertitel.
func Title(p *Partner) string {
	if p == nil || p.AppName == "" {
		return DefaultAppName
	}
	return strings.NewReplacer("[", "", "]", "").Replace(p.AppName)
}
