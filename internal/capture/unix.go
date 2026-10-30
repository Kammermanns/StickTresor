//go:build darwin || linux

package capture

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/Kammermanns/StickTresor/internal/vault"
)

func init() {
	// Systemprogramme wie «ip» liegen teils in sbin, das bei normalen Benutzern fehlt.
	os.Setenv("PATH", os.Getenv("PATH")+":/usr/local/sbin:/usr/sbin:/sbin:/usr/bin:/bin")
}

// fields sammelt die Werte in der Reihenfolge, in der sie gesetzt werden.
type fields struct{ list []vault.Field }

func (f *fields) add(k, v string) {
	v = strings.TrimSpace(v)
	if v != "" {
		f.list = append(f.list, vault.Field{Key: k, Value: v})
	}
}

// run führt ein Programm aus und liefert seine Ausgabe ("" bei Fehler oder Zeitüberschreitung).
func run(timeout time.Duration, name string, args ...string) string {
	if _, err := exec.LookPath(name); err != nil && !strings.HasPrefix(name, "/") {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil && out.Len() == 0 {
		return ""
	}
	return out.String()
}

func readFile(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// gb formatiert Bytes wie unter Windows («512 GB», «7,8 GB»).
func gb(b float64) string {
	g := b / (1 << 30)
	if g >= 100 {
		return fmt.Sprintf("%.0f GB", g)
	}
	return strings.Replace(fmt.Sprintf("%.1f GB", g), ".", ",", 1)
}

// good filtert Platzhalter, die Hersteller statt echter Seriennummern eintragen.
func good(s string) bool {
	l := strings.ToLower(strings.TrimSpace(s))
	if len(l) < 4 {
		return false
	}
	for _, bad := range []string{"to be filled", "default string", "system serial", "not specified", "none", "chassis serial", "not applicable", "0123456789"} {
		if strings.Contains(l, bad) {
			return false
		}
	}
	return strings.Trim(l, "0") != ""
}

func uniqSorted(in []string) []string {
	m := map[string]bool{}
	out := []string{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !m[strings.ToLower(s)] {
			m[strings.ToLower(s)] = true
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

func bulletList(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return "- " + strings.Join(items, "\n- ")
}

// lineValue liefert den Wert nach «Schlüssel:» aus einer Textausgabe.
func lineValue(text, key string) string {
	for _, l := range strings.Split(text, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, key) {
			rest := strings.TrimSpace(strings.TrimPrefix(t, key))
			rest = strings.TrimSpace(strings.TrimLeft(rest, ":="))
			return rest
		}
	}
	return ""
}

var avNames = []string{"Sophos", "ESET", "Norton", "Avast", "AVG", "Bitdefender", "Kaspersky", "Malwarebytes", "Intego", "McAfee", "Trend Micro", "F-Secure", "G DATA", "Avira", "SentinelOne", "CrowdStrike", "Microsoft Defender", "ClamXAV", "Webroot"}

func browserName(id string) string {
	l := strings.ToLower(id)
	switch {
	case strings.Contains(l, "chrome") && !strings.Contains(l, "chromium"):
		return "Google Chrome"
	case strings.Contains(l, "chromium"):
		return "Chromium"
	case strings.Contains(l, "firefox"):
		return "Mozilla Firefox"
	case strings.Contains(l, "edge"):
		return "Microsoft Edge"
	case strings.Contains(l, "brave"):
		return "Brave"
	case strings.Contains(l, "opera"):
		return "Opera"
	case strings.Contains(l, "vivaldi"):
		return "Vivaldi"
	case strings.Contains(l, "safari"):
		return "Safari"
	case strings.Contains(l, "thunderbird"):
		return "Thunderbird"
	case strings.Contains(l, "outlook"):
		return "Outlook"
	case strings.Contains(l, "evolution"):
		return "Evolution"
	case strings.Contains(l, "geary"):
		return "Geary"
	case strings.Contains(l, "kmail"):
		return "KMail"
	case l == "com.apple.mail" || strings.HasSuffix(l, ".mail"):
		return "Apple Mail"
	}
	return id
}
