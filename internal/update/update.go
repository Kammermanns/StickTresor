// Package update sucht auf Knopfdruck nach einer neueren Version auf GitHub
// und tauscht die Exe auf dem Stick aus.
//
// Ablauf:
//  1. Neueste Version abfragen (api.github.com, nur dieses Projekt).
//  2. StickTresor.exe und StickTresor.exe.sha256 aus dem Release laden.
//  3. Prüfsumme vergleichen; ist das laufende Programm signiert, muss auch die
//     neue Datei gültig und vom selben Herausgeber signiert sein.
//  4. Laufende Exe in .old umbenennen, neue an ihre Stelle legen.
//
// Daten (daten.kdbx, partner.json) werden nicht verändert.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Repo ist das GitHub-Projekt, aus dem Updates geladen werden.
const Repo = "Kammermanns/StickTresor"

const (
	AssetName = "StickTresor.exe"
	maxSize   = 60 << 20
)

// API kann in Tests ersetzt werden.
var API = "https://api.github.com"

type Info struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	Notes     string `json:"notes"`
	Page      string `json:"page"`
	Published string `json:"published"`
	exeURL    string
	shaURL    string
	size      int64
}

type release struct {
	TagName     string `json:"tag_name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

var client = &http.Client{Timeout: 90 * time.Second}

func get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "StickTresor-Update")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("Keine Verbindung zu GitHub. Ist der PC mit dem Internet verbunden?")
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		if resp.StatusCode == 404 {
			return nil, errors.New("Es ist noch keine Version veröffentlicht.")
		}
		return nil, fmt.Errorf("GitHub antwortet mit Fehler %d.", resp.StatusCode)
	}
	return resp, nil
}

// Check fragt die neueste veröffentlichte Version ab.
func Check(ctx context.Context, current string) (*Info, error) {
	resp, err := get(ctx, API+"/repos/"+Repo+"/releases/latest")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return nil, errors.New("Die Antwort von GitHub ist unverständlich.")
	}
	in := &Info{Current: current, Latest: strings.TrimPrefix(r.TagName, "v"), Notes: r.Body, Page: r.HTMLURL}
	if t, err := time.Parse(time.RFC3339, r.PublishedAt); err == nil {
		in.Published = t.Local().Format("02.01.2006")
	}
	for _, a := range r.Assets {
		switch a.Name {
		case AssetName:
			in.exeURL, in.size = a.URL, a.Size
		case AssetName + ".sha256":
			in.shaURL = a.URL
		}
	}
	in.Available = !r.Draft && !r.Prerelease && Newer(in.Latest, current) && in.exeURL != "" && in.shaURL != ""
	return in, nil
}

// Newer meldet, ob Version a neuer ist als b ("4.1.0" > "4.0").
func Newer(a, b string) bool {
	pa, pb := parts(a), parts(b)
	for i := 0; i < 4; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parts(v string) [4]int {
	var p [4]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	for i, s := range strings.SplitN(v, ".", 4) {
		n, _ := strconv.Atoi(s)
		p[i] = n
	}
	return p
}

// SignatureFunc liefert, ob eine Datei gültig signiert ist, und den Herausgeber.
type SignatureFunc func(path string) (valid bool, subject string, err error)

// Download lädt die neue Version neben exe (exe + ".new") und prüft sie.
func Download(ctx context.Context, in *Info, exe string, sig SignatureFunc) (string, error) {
	if in == nil || !in.Available {
		return "", errors.New("Kein Update verfügbar.")
	}
	if in.size > maxSize {
		return "", errors.New("Die Update-Datei ist unerwartet gross.")
	}
	// erwartete Prüfsumme
	resp, err := get(ctx, in.shaURL)
	if err != nil {
		return "", err
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	want := strings.ToLower(strings.Fields(string(b) + " ")[0])
	if len(want) != 64 {
		return "", errors.New("Die Prüfsummen-Datei ist ungültig.")
	}

	dst := exe + ".new"
	resp, err = get(ctx, in.exeURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", fmt.Errorf("Kann nicht auf den Stick schreiben: %v", err)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxSize+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	fail := func(msg string) (string, error) { os.Remove(dst); return "", errors.New(msg) }
	if err != nil {
		return fail("Download abgebrochen: " + err.Error())
	}
	if n > maxSize {
		return fail("Die Update-Datei ist unerwartet gross.")
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		return fail("Die Prüfsumme stimmt nicht. Das Update wurde verworfen.")
	}
	if sig != nil {
		curOK, curSub, _ := sig(exe)
		if curOK {
			newOK, newSub, err := sig(dst)
			if err != nil || !newOK {
				return fail("Die neue Version ist nicht gültig signiert. Das Update wurde verworfen.")
			}
			if newSub != curSub {
				return fail("Die neue Version stammt von einem anderen Herausgeber. Das Update wurde verworfen.")
			}
		}
	}
	return dst, nil
}

// Install ersetzt exe durch die geprüfte Datei newPath. Die bisherige Exe bleibt als .old
// liegen (eine laufende Exe kann unter Windows umbenannt, aber nicht gelöscht werden).
func Install(exe, newPath string) error {
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		os.Remove(newPath)
		return fmt.Errorf("Das Programm kann nicht ersetzt werden: %v", err)
	}
	if err := os.Rename(newPath, exe); err != nil {
		os.Rename(old, exe) // zurück
		os.Remove(newPath)
		return fmt.Errorf("Das Programm kann nicht ersetzt werden: %v", err)
	}
	return nil
}

// Cleanup entfernt Reste eines früheren Updates (beim Start aufrufen).
func Cleanup(exe string) {
	os.Remove(exe + ".old")
	os.Remove(exe + ".new")
}
