// devserver startet die Oberfläche im Browser (nur für Entwicklung und Tests).
// Die PC-Erfassung liefert Beispieldaten (mit -echt die echten Werte dieses PCs).
//
//	go run ./cmd/devserver -dir /tmp/stick -addr 127.0.0.1:8099
package main

import (
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Kammermanns/StickTresor/internal/app"
	"github.com/Kammermanns/StickTresor/internal/capture"
	"github.com/Kammermanns/StickTresor/internal/update"
	"github.com/Kammermanns/StickTresor/internal/web"
	"github.com/Kammermanns/StickTresor/ui"
)

type fakePlat struct{ last string }

func (f *fakePlat) CopyToClipboard(t string, _ time.Duration) error { f.last = t; return nil }
func (f *fakePlat) ClearClipboardIfOurs()                           {}
func (f *fakePlat) OpenFolder(string) error                         { return nil }
func (f *fakePlat) PickFolder(string) (string, error)               { return os.Getenv("PICK_DIR"), nil }
func (f *fakePlat) Quit()                                           {}
func (f *fakePlat) OpenURL(string) error                            { return nil }
func (f *fakePlat) Signature(string) (bool, string, error)          { return false, "", nil }
func (f *fakePlat) Restart(string) error                            { return nil }

func main() {
	dir := flag.String("dir", ".", "Programmordner (Daten liegen in <dir>/Daten)")
	addr := flag.String("addr", "127.0.0.1:8099", "Adresse")
	echt := flag.Bool("echt", false, "echte PC-Erfassung statt Beispieldaten")
	flag.Parse()
	capture.UseFake = !*echt
	if v := os.Getenv("ST_UPDATE_API"); v != "" {
		update.API = v
	}
	plat := &fakePlat{}
	a := app.New(*dir, plat)
	s := &web.Server{
		Binds: a.Bindings(),
		Page:  ui.Page(),
		Extra: map[string]http.HandlerFunc{
			"/clipboard": func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, plat.last) },
		},
	}
	log.Printf("http://%s  (Programmordner: %s)", *addr, *dir)
	log.Fatal(http.ListenAndServe(*addr, s.Handler()))
}
