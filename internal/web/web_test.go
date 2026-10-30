package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServer(t *testing.T) {
	bye := 0
	s := &Server{
		Binds: map[string]any{
			"add":  func(a, b int) (int, error) { return a + b, nil },
			"fail": func() error { return errors.New("geht nicht") },
		},
		Page:  "<html><body><script>\n\"use strict\";\n</script></body></html>",
		Token: "geheim",
		Host:  "127.0.0.1:5000",
		OnBye: func() { bye++ },
	}
	h := s.Handler()
	do := func(method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Host = "127.0.0.1:5000"
		for k, v := range hdr {
			if k == "Host" {
				r.Host = v
				continue
			}
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	ok := map[string]string{"X-ST-Token": "geheim"}
	if w := do("POST", "/api/add", "[2,3]", ok); w.Code != 200 || !strings.Contains(w.Body.String(), `"result":5`) {
		t.Fatalf("Aufruf: %d %s", w.Code, w.Body)
	}
	if w := do("POST", "/api/add", "[2,3]", nil); w.Code != 403 {
		t.Fatalf("ohne Schlüssel: %d", w.Code)
	}
	if w := do("POST", "/api/add", "[2,3]", map[string]string{"X-ST-Token": "falsch"}); w.Code != 403 {
		t.Fatal("falscher Schlüssel akzeptiert")
	}
	if w := do("POST", "/api/add", "[2,3]", map[string]string{"X-ST-Token": "geheim", "Host": "evil.example:5000"}); w.Code != 403 {
		t.Fatal("fremder Host akzeptiert (DNS-Rebinding)")
	}
	if w := do("POST", "/api/add", "[2,3]", map[string]string{"X-ST-Token": "geheim", "Origin": "http://evil.example"}); w.Code != 403 {
		t.Fatal("fremde Origin akzeptiert")
	}
	if w := do("GET", "/api/add", "", ok); w.Code != 403 {
		t.Fatal("GET akzeptiert")
	}
	if w := do("POST", "/api/fail", "[]", ok); w.Code != 400 || !strings.Contains(w.Body.String(), "geht nicht") {
		t.Fatalf("Fehler: %s", w.Body)
	}
	w := do("GET", "/", "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "api_") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Seite: %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "geheim") {
		t.Fatal("Schlüssel steht in der Seite")
	}
	do("POST", "/bye", "falsch", nil)
	do("POST", "/bye", "geheim", nil)
	if bye != 1 {
		t.Fatalf("bye: %d", bye)
	}
	_ = http.StatusOK
}
