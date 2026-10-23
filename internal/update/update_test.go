package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"4.1.0", "4.0", true}, {"v4.0.1", "4.0", true}, {"4.0", "4.0.0", false}, {"3.9", "4.0", false}, {"10.0", "9.9", true}, {"4.1.0-beta", "4.1", false}} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%s,%s)=%v", c.a, c.b, got)
		}
	}
}

func server(t *testing.T, exe []byte, sha string) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + Repo + "/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v4.2.0","body":"Neu: Updates","html_url":"https://github.com/x","published_at":"2026-11-01T10:00:00Z",
			"assets":[{"name":"StickTresor.exe","browser_download_url":"%[1]s/exe","size":%[2]d},{"name":"StickTresor.exe.sha256","browser_download_url":"%[1]s/sha"}]}`, srv.URL, len(exe))
		case "/exe":
			w.Write(exe)
		case "/sha":
			fmt.Fprintf(w, "%s  StickTresor.exe\n", sha)
		default:
			http.NotFound(w, r)
		}
	}))
	return srv
}

func TestDownloadInstall(t *testing.T) {
	newExe := []byte("NEUE VERSION")
	sum := sha256.Sum256(newExe)
	srv := server(t, newExe, hex.EncodeToString(sum[:]))
	defer srv.Close()
	API = srv.URL

	dir := t.TempDir()
	exe := filepath.Join(dir, "StickTresor.exe")
	os.WriteFile(exe, []byte("ALT"), 0o755)

	in, err := Check(context.Background(), "4.0")
	if err != nil || !in.Available || in.Latest != "4.2.0" || in.Published != "01.11.2026" {
		t.Fatalf("Check: %+v %v", in, err)
	}
	if in2, _ := Check(context.Background(), "4.2"); in2.Available {
		t.Fatal("gleiche Version als Update gemeldet")
	}
	// signiertes Programm, neue Datei unsigniert => abgelehnt
	signedOld := func(p string) (bool, string, error) { return p == exe, "CN=Test", nil }
	if _, err := Download(context.Background(), in, exe, signedOld); err == nil {
		t.Fatal("unsignierte Datei akzeptiert")
	}
	// anderer Herausgeber => abgelehnt
	other := func(p string) (bool, string, error) {
		if p == exe {
			return true, "CN=Test", nil
		}
		return true, "CN=Fremd", nil
	}
	if _, err := Download(context.Background(), in, exe, other); err == nil {
		t.Fatal("fremder Herausgeber akzeptiert")
	}
	nw, err := Download(context.Background(), in, exe, func(string) (bool, string, error) { return false, "", nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := Install(exe, nw); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "NEUE VERSION" {
		t.Fatalf("Exe: %s", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "ALT" {
		t.Fatal(".old fehlt")
	}
	Cleanup(exe)
	if _, err := os.Stat(exe + ".old"); err == nil {
		t.Fatal(".old nicht aufgeräumt")
	}
}

func TestBadChecksum(t *testing.T) {
	srv := server(t, []byte("MANIPULIERT"), "0000000000000000000000000000000000000000000000000000000000000000")
	defer srv.Close()
	API = srv.URL
	dir := t.TempDir()
	exe := filepath.Join(dir, "StickTresor.exe")
	os.WriteFile(exe, []byte("ALT"), 0o755)
	in, _ := Check(context.Background(), "4.0")
	if _, err := Download(context.Background(), in, exe, nil); err == nil {
		t.Fatal("falsche Prüfsumme akzeptiert")
	}
	if _, err := os.Stat(exe + ".new"); err == nil {
		t.Fatal(".new nicht entfernt")
	}
}
