// Package web stellt die Oberfläche im Browser bereit (macOS und Linux, Entwicklung).
//
// Sicherheit: Der Server hört nur auf 127.0.0.1. Jeder Aufruf der App-Funktionen braucht
// einen Zufallsschlüssel, den nur das Browserfenster kennt, das die App selbst öffnet.
// Host- und Origin-Prüfung verhindern, dass fremde Webseiten (DNS-Rebinding) zugreifen.
package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sort"
	"strings"
)

type Server struct {
	Binds map[string]any // Name -> Funktion (wie bei WebView2)
	Page  string         // die Oberfläche (HTML)
	Token string         // "" = keine Prüfung (nur Entwicklung)
	Host  string         // erwarteter Host, z.B. "127.0.0.1:53111"; "" = keine Prüfung
	// Aufrufe aus dem Browser: Lebenszeichen und Schliessen des Fensters
	OnActivity func()
	OnBye      func()
	Extra      map[string]http.HandlerFunc
}

// NewToken erzeugt einen zufälligen Schlüssel.
func NewToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

const shim = `<script>
(function(){
  var KEY = "st-token", tok = "";
  var m = /[#&]t=([0-9a-f]+)/.exec(location.hash);
  try { if (m) sessionStorage.setItem(KEY, m[1]); tok = sessionStorage.getItem(KEY) || ""; } catch (e) { tok = m ? m[1] : ""; }
  if (m) { try { history.replaceState(null, "", location.pathname); } catch (e) {} }
  window.ST_BROWSER = true;
  var dead = false;
  function gone(msg) {
    if (dead) return; dead = true;
    document.body.innerHTML = '<div style="font-family:system-ui,sans-serif;max-width:520px;margin:18vh auto;padding:32px;text-align:center;color:#3A3540">' +
      '<h1 style="font-size:22px;margin:0 0 10px">' + msg + '</h1><p style="margin:0;color:#5F5966">Sie können dieses Fenster schliessen.</p></div>';
  }
  function call(n, args) {
    return fetch("/api/" + n, {method: "POST", headers: {"Content-Type": "application/json", "X-ST-Token": tok}, body: JSON.stringify(args), cache: "no-store"})
      .then(function(r){ return r.json().then(function(j){ if (!r.ok) throw j.error; return j.result; }); },
            function(){ gone("Die App ist beendet."); throw "Die App ist nicht mehr erreichbar."; });
  }
  var names = %s;
  names.forEach(function(n){ window["api_" + n] = function(){ return call(n, Array.prototype.slice.call(arguments)); }; });
  var q = window.api_quit;
  window.api_quit = function(){ return q().then(function(){ gone("Die App wurde beendet."); }); };
  var iu = window.api_installUpdate;
  if (iu) window.api_installUpdate = function(){ return iu().then(function(){ gone("Das Update ist installiert. Die App startet in einem neuen Fenster."); }); };
  setInterval(function(){ if (!dead) fetch("/ping", {method: "POST", headers: {"X-ST-Token": tok}, cache: "no-store"}).catch(function(){ gone("Die App ist beendet."); }); }, 15000);
  window.addEventListener("pagehide", function(){ try { navigator.sendBeacon("/bye", tok); } catch (e) {} });
})();
</script>`

// Handler liefert den HTTP-Handler.
func (s *Server) Handler() http.Handler {
	names := make([]string, 0, len(s.Binds))
	for n := range s.Binds {
		names = append(names, n)
	}
	sort.Strings(names)
	nj, _ := json.Marshal(names)
	page := strings.Replace(s.Page, "<script>\n\"use strict\";", fmt.Sprintf(shim, nj)+"\n<script>\n\"use strict\";", 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, page)
	})
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r, r.Header.Get("X-ST-Token")) {
			http.Error(w, "nicht erlaubt", http.StatusForbidden)
			return
		}
		s.activity()
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/bye", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 200))
		if !s.authorized(r, strings.TrimSpace(string(b))) {
			http.Error(w, "nicht erlaubt", http.StatusForbidden)
			return
		}
		if s.OnBye != nil {
			s.OnBye()
		}
		w.WriteHeader(http.StatusNoContent)
	})
	for p, h := range s.Extra {
		mux.HandleFunc(p, h)
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !s.authorized(r, r.Header.Get("X-ST-Token")) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "nicht erlaubt"})
			return
		}
		s.activity()
		fn, ok := s.Binds[strings.TrimPrefix(r.URL.Path, "/api/")]
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "unbekannte Funktion"})
			return
		}
		var raw []json.RawMessage
		if err := json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&raw); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "ungültige Anfrage"})
			return
		}
		res, err := call(fn, raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"result": res})
	})
	return s.guard(mux)
}

// guard prüft Host und Origin und verbietet das Zwischenspeichern.
func (s *Server) guard(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Host != "" {
			if r.Host != s.Host {
				http.Error(w, "nicht erlaubt", http.StatusForbidden)
				return
			}
			if o := r.Header.Get("Origin"); o != "" && o != "http://"+s.Host {
				http.Error(w, "nicht erlaubt", http.StatusForbidden)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		h.ServeHTTP(w, r)
	})
}

func (s *Server) authorized(r *http.Request, tok string) bool {
	if s.Token == "" {
		return true
	}
	return subtle.ConstantTimeCompare([]byte(tok), []byte(s.Token)) == 1
}

func (s *Server) activity() {
	if s.OnActivity != nil {
		s.OnActivity()
	}
}

type callErr struct{ msg string }

func (e callErr) Error() string { return e.msg }

// call ruft eine gebundene Funktion mit JSON-Argumenten auf (wie die WebView2-Bindings).
func call(fn any, raw []json.RawMessage) (any, error) {
	v := reflect.ValueOf(fn)
	t := v.Type()
	if len(raw) != t.NumIn() {
		return nil, callErr{"falsche Anzahl Argumente"}
	}
	args := make([]reflect.Value, 0, len(raw))
	for i := range raw {
		p := reflect.New(t.In(i))
		if err := json.Unmarshal(raw[i], p.Interface()); err != nil {
			return nil, callErr{"ungültiges Argument"}
		}
		args = append(args, p.Elem())
	}
	var result any
	errType := reflect.TypeOf((*error)(nil)).Elem()
	for _, x := range v.Call(args) {
		if x.Type().Implements(errType) {
			if !x.IsNil() {
				return nil, x.Interface().(error)
			}
			continue
		}
		result = x.Interface()
	}
	return result, nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
