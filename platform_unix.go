//go:build darwin || linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ErrNoClipboard sagt der Oberfläche, dass sie die Zwischenablage des Browsers verwenden soll.
var errNoClipboard = errors.New("NOCLIP")

type unixPlatform struct {
	quit func()
	mu   sync.Mutex
	last string // zuletzt von uns kopierter Text
	gen  int
}

func has(name string) bool { _, err := exec.LookPath(name); return err == nil }

func runQuiet(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	return cmd.Run()
}

// startDetached startet ein Programm, das weiterläuft, auch wenn StickTresor endet.
func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// ---------- Zwischenablage ----------

func clipCopyCmd() (string, []string) {
	if runtime.GOOS == "darwin" {
		return "pbcopy", nil
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" && has("wl-copy") {
		return "wl-copy", nil
	}
	if has("xclip") {
		return "xclip", []string{"-selection", "clipboard"}
	}
	if has("xsel") {
		return "xsel", []string{"--clipboard", "--input"}
	}
	if has("wl-copy") {
		return "wl-copy", nil
	}
	return "", nil
}

func clipPasteCmd() (string, []string) {
	if runtime.GOOS == "darwin" {
		return "pbpaste", nil
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" && has("wl-paste") {
		return "wl-paste", []string{"-n"}
	}
	if has("xclip") {
		return "xclip", []string{"-selection", "clipboard", "-o"}
	}
	if has("xsel") {
		return "xsel", []string{"--clipboard", "--output"}
	}
	return "", nil
}

func setClipboard(text string) error {
	name, args := clipCopyCmd()
	if name == "" {
		return errNoClipboard
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(text)
	// xclip/wl-copy bleiben im Hintergrund, bis jemand anderes kopiert
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if name == "wl-copy" && text == "" {
		cmd = exec.Command("wl-copy", "--clear")
	}
	if err := cmd.Start(); err != nil {
		return errNoClipboard
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return errNoClipboard
		}
	case <-time.After(2 * time.Second): // läuft weiter (xclip hält die Auswahl)
	}
	return nil
}

func getClipboard() (string, bool) {
	name, args := clipPasteCmd()
	if name == "" {
		return "", false
	}
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return "", false
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return out.String(), err == nil
	case <-time.After(2 * time.Second):
		cmd.Process.Kill()
		return "", false
	}
}

func (p *unixPlatform) CopyToClipboard(text string, clearAfter time.Duration) error {
	if err := setClipboard(text); err != nil {
		return err
	}
	p.mu.Lock()
	p.gen++
	gen := p.gen
	p.last = text
	p.mu.Unlock()
	if clearAfter > 0 {
		go func() {
			time.Sleep(clearAfter)
			p.mu.Lock()
			still := p.gen == gen
			p.mu.Unlock()
			if still {
				p.ClearClipboardIfOurs()
			}
		}()
	}
	return nil
}

func (p *unixPlatform) ClearClipboardIfOurs() {
	p.mu.Lock()
	last := p.last
	p.mu.Unlock()
	if last == "" {
		return
	}
	if cur, ok := getClipboard(); ok && strings.TrimRight(cur, "\n") == strings.TrimRight(last, "\n") {
		setClipboard("")
	}
	p.mu.Lock()
	p.last = ""
	p.mu.Unlock()
}

// ---------- Ordner, Links ----------

func openWith(target string) error {
	if runtime.GOOS == "darwin" {
		return runQuiet("open", target)
	}
	return startDetached("xdg-open", target)
}

func (p *unixPlatform) OpenFolder(path string) error { return openWith(path) }
func (p *unixPlatform) OpenURL(u string) error       { return openWith(u) }

func (p *unixPlatform) PickFolder(title string) (string, error) {
	var out []byte
	var err error
	switch {
	case runtime.GOOS == "darwin":
		script := fmt.Sprintf(`POSIX path of (choose folder with prompt %q)`, title)
		out, err = exec.Command("osascript", "-e", script).Output()
	case has("zenity"):
		out, err = exec.Command("zenity", "--file-selection", "--directory", "--title="+title).Output()
	case has("kdialog"):
		home, _ := os.UserHomeDir()
		out, err = exec.Command("kdialog", "--getexistingdirectory", home, "--title", title).Output()
	case has("qarma"):
		out, err = exec.Command("qarma", "--file-selection", "--directory", "--title="+title).Output()
	default:
		return "", errors.New("Kein Dialog zur Ordnerwahl gefunden. Bitte «zenity» oder «kdialog» installieren.")
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", nil // abgebrochen
		}
		return "", err
	}
	return strings.TrimRight(strings.TrimSpace(string(out)), "/"), nil
}

func (p *unixPlatform) Quit() {
	if p.quit != nil {
		p.quit()
	}
}

// Signature: Auf macOS und Linux gibt es keine Authenticode-Signatur; das Update
// verlässt sich dort auf die Prüfsumme aus dem Release.
func (p *unixPlatform) Signature(path string) (bool, string, error) { return false, "", nil }

func (p *unixPlatform) Restart(exe string) error {
	return startDetached(exe, "--nach-update")
}

// ---------- Browser ----------

// openBrowser öffnet die Oberfläche möglichst als eigenes App-Fenster
// (Chrome, Edge, Brave, Chromium), sonst im Standardbrowser.
func openBrowser(url string) {
	args := []string{"--app=" + url, "--window-size=1400,920"}
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		for _, name := range []string{"Google Chrome", "Microsoft Edge", "Brave Browser", "Chromium", "Vivaldi"} {
			for _, base := range []string{"/Applications", filepath.Join(home, "Applications")} {
				appPath := filepath.Join(base, name+".app")
				if _, err := os.Stat(appPath); err == nil {
					if runQuiet("open", append([]string{"-na", appPath, "--args"}, args...)...) == nil {
						return
					}
				}
			}
		}
		runQuiet("open", url)
		return
	}
	for _, b := range []string{"google-chrome", "google-chrome-stable", "microsoft-edge", "microsoft-edge-stable", "brave-browser", "chromium", "chromium-browser", "vivaldi"} {
		if has(b) && startDetached(b, args...) == nil {
			return
		}
	}
	startDetached("xdg-open", url)
}

// fatal zeigt eine Meldung an (Dialog, falls möglich) und auf der Konsole.
func fatal(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	switch {
	case runtime.GOOS == "darwin":
		exec.Command("osascript", "-e", fmt.Sprintf(`display dialog %q with title "StickTresor" buttons {"OK"} default button 1 with icon caution`, msg)).Run()
	case has("zenity"):
		exec.Command("zenity", "--error", "--title=StickTresor", "--text="+msg).Run()
	case has("kdialog"):
		exec.Command("kdialog", "--title", "StickTresor", "--error", msg).Run()
	}
}
