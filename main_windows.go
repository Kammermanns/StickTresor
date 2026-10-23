//go:build windows

// StickTresor – PC-Datenblatt, Passwörter und Abos, portabel auf dem USB-Stick.
// Die Daten liegen in daten.kdbx im selben Ordner wie die Exe,
// die Angaben der IT-Firma (optional) in partner.json.
package main

import (
	"unsafe"

	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"

	"github.com/Kammermanns/StickTresor/internal/app"
	"github.com/Kammermanns/StickTresor/internal/partner"
	"github.com/Kammermanns/StickTresor/internal/update"
	"github.com/Kammermanns/StickTresor/ui"
)

const (
	mbIconError   = 0x10
	mbIconWarning = 0x30
)

func afterUpdate() bool {
	for _, a := range os.Args[1:] {
		if a == "--nach-update" {
			return true
		}
	}
	return false
}

func encodeB64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func dataDir() string {
	if d := os.Getenv("ST_DATA_DIR"); d != "" {
		return d
	}
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return filepath.Dir(exe)
}

func main() {
	dir := dataDir()
	p, _ := partner.Load(dir)
	title := partner.Title(p)

	// Nur eine Instanz pro Ordner, damit nicht zwei Fenster dieselbe Datei schreiben.
	sum := sha256.Sum256([]byte(strings.ToLower(dir)))
	name, _ := windows.UTF16PtrFromString("Local\\StickTresor-" + hex.EncodeToString(sum[:8]))
	h, err := windows.CreateMutex(nil, false, name)
	// Nach einem Update: warten, bis sich die alte Version beendet hat.
	for i := 0; err == windows.ERROR_ALREADY_EXISTS && afterUpdate() && i < 40; i++ {
		windows.CloseHandle(h)
		time.Sleep(250 * time.Millisecond)
		h, err = windows.CreateMutex(nil, false, name)
	}
	if err == windows.ERROR_ALREADY_EXISTS {
		messageBox(title, "Die App ist bereits geöffnet.", mbIconWarning)
		return
	}
	if exe, err := os.Executable(); err == nil {
		update.Cleanup(exe) // Reste eines früheren Updates
	}
	if h != 0 {
		defer windows.CloseHandle(h)
	}

	plat := &winPlatform{}
	a := app.New(dir, plat)

	winW, winH := windowSize()

	// WebView2 legt seinen Zwischenspeicher im Temp-Ordner des PCs ab, nicht auf dem Stick.
	cache := filepath.Join(os.TempDir(), "StickTresor-WebView2")
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     os.Getenv("ST_DEBUG") == "1",
		AutoFocus: true,
		DataPath:  cache,
		WindowOptions: webview2.WindowOptions{
			Title:  title,
			Width:  winW,
			Height: winH,
			IconId: 2, // Icon-Gruppe aus der Ressourcendatei
			Center: true,
		},
	})
	if w == nil {
		msg := "Die Microsoft Edge WebView2-Laufzeit fehlt auf diesem PC.\n\n" +
			"Sie ist in Windows 10 und 11 normalerweise vorhanden und kann kostenlos bei Microsoft " +
			"heruntergeladen werden (\"WebView2 Runtime\")."
		if p != nil && p.Tel != "" {
			msg += "\n\nSupport: " + p.Firma + ", " + p.Tel
		}
		messageBox(title, msg, mbIconError)
		return
	}
	defer w.Destroy()
	plat.quit = func() { w.Dispatch(w.Terminate) }
	w.SetSize(int(scale(900)), int(scale(620)), webview2.HintMin)

	for name, fn := range a.Bindings() {
		if err := w.Bind("api_"+name, fn); err != nil {
			messageBox(title, "Interner Fehler: "+err.Error(), mbIconError)
			return
		}
	}
	w.SetHtml(ui.Page())
	w.Run()

	// Beim Schliessen: Tresor sperren, kopiertes Passwort entfernen.
	a.Lock()
}

var (
	pGetDpiForSystem      = user32.NewProc("GetDpiForSystem")
	pSystemParametersInfo = user32.NewProc("SystemParametersInfoW")
)

// scale rechnet logische Pixel (96 dpi) in Bildschirmpixel um, damit das Fenster
// auch bei 125–200 % Skalierung (z.B. Surface) gross genug ist.
func scale(v uint) uint {
	dpi := uint(96)
	if pGetDpiForSystem.Find() == nil {
		if d, _, _ := pGetDpiForSystem.Call(); d >= 96 {
			dpi = uint(d)
		}
	}
	return v * dpi / 96
}

// windowSize: 1400×920 (logisch), höchstens 92 % des freien Bildschirms.
func windowSize() (uint, uint) {
	w, h := scale(1400), scale(920)
	var r struct{ Left, Top, Right, Bottom int32 }
	if ok, _, _ := pSystemParametersInfo.Call(0x0030, 0, uintptr(unsafe.Pointer(&r)), 0); ok != 0 {
		aw, ah := uint(r.Right-r.Left), uint(r.Bottom-r.Top)
		if mw := aw * 92 / 100; w > mw {
			w = mw
		}
		if mh := ah * 92 / 100; h > mh {
			h = mh
		}
	}
	return w, h
}
