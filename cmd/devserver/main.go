// devserver startet die Oberfläche im Browser (nur für Entwicklung und Tests).
// Die PC-Erfassung liefert ausserhalb von Windows Beispieldaten.
//
//	go run ./cmd/devserver -dir /tmp/stick -addr 127.0.0.1:8099
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/Kammermanns/StickTresor/internal/app"
	"github.com/Kammermanns/StickTresor/internal/update"
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

const shim = `<script>
(function(){
  var names = %s;
  names.forEach(function(n){
    window["api_" + n] = function(){
      var args = Array.prototype.slice.call(arguments);
      return fetch("/api/" + n, {method:"POST", body: JSON.stringify(args)}).then(function(r){
        return r.json().then(function(j){ if (!r.ok) throw j.error; return j.result; });
      });
    };
  });
})();
</script>`

func main() {
	dir := flag.String("dir", ".", "Ordner mit daten.kdbx")
	addr := flag.String("addr", "127.0.0.1:8099", "Adresse")
	flag.Parse()

	if v := os.Getenv("ST_UPDATE_API"); v != "" {
		update.API = v
	}
	plat := &fakePlat{}
	a := app.New(*dir, plat)
	binds := a.Bindings()
	names := make([]string, 0, len(binds))
	for n := range binds {
		names = append(names, n)
	}
	nj, _ := json.Marshal(names)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		page := ui.Page()
		page = strings.Replace(page, "<script>\n\"use strict\";", fmt.Sprintf(shim, nj)+"\n<script>\n\"use strict\";", 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, page)
	})
	http.HandleFunc("/clipboard", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, plat.last) })
	http.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/api/")
		fn, ok := binds[name]
		if !ok {
			http.Error(w, "unbekannt", 404)
			return
		}
		var raw []json.RawMessage
		json.NewDecoder(r.Body).Decode(&raw)
		v := reflect.ValueOf(fn)
		t := v.Type()
		if len(raw) != t.NumIn() {
			writeJSON(w, 400, map[string]any{"error": "function arguments mismatch"})
			return
		}
		args := []reflect.Value{}
		for i := range raw {
			p := reflect.New(t.In(i))
			if err := json.Unmarshal(raw[i], p.Interface()); err != nil {
				writeJSON(w, 400, map[string]any{"error": err.Error()})
				return
			}
			args = append(args, p.Elem())
		}
		res := v.Call(args)
		var result any
		for _, x := range res {
			if x.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
				if !x.IsNil() {
					writeJSON(w, 400, map[string]any{"error": x.Interface().(error).Error()})
					return
				}
				continue
			}
			result = x.Interface()
		}
		writeJSON(w, 200, map[string]any{"result": result})
	})
	log.Printf("http://%s  (Daten: %s)", *addr, *dir)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
