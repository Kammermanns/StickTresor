//go:build darwin || linux

// StickTresor auf macOS und Linux: Die Oberfläche öffnet sich als Browserfenster.
// Das Programm läuft im Hintergrund und beendet sich, wenn das Fenster geschlossen wird.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Kammermanns/StickTresor/internal/app"
	"github.com/Kammermanns/StickTresor/internal/update"
	"github.com/Kammermanns/StickTresor/internal/web"
	"github.com/Kammermanns/StickTresor/ui"
)

// exeDir ist der Ordner, in dem das Programm auf dem Stick liegt.
// Auf dem Mac ist das der Ordner, der StickTresor.app enthält.
func exeDir() (string, string) {
	if d := os.Getenv("ST_DATA_DIR"); d != "" {
		return d, ""
	}
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		return wd, ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	dir := filepath.Dir(exe)
	if i := strings.Index(exe, ".app/Contents/MacOS/"); i >= 0 {
		dir = filepath.Dir(exe[:i+4])
	}
	return dir, exe
}

func afterUpdate() bool {
	for _, a := range os.Args[1:] {
		if a == "--nach-update" {
			return true
		}
	}
	return false
}

func main() {
	dir, exe := exeDir()
	if strings.Contains(dir, "/AppTranslocation/") {
		fatal("StickTresor wurde von macOS in einen geschützten Ordner verschoben und findet seine Daten nicht.\n\n" +
			"Bitte im Terminal einmal ausführen:\n  xattr -cr <Pfad zum Stick>/StickTresor.app\n\nund die App danach erneut starten.")
		return
	}
	if exe != "" {
		update.Cleanup(exe) // Reste eines früheren Updates
	}

	// Nur eine Instanz pro Stick: Sperrdatei im Temp-Ordner des Benutzers
	sum := sha256.Sum256([]byte(dir))
	lockPath := filepath.Join(os.TempDir(), "sticktresor-"+hex.EncodeToString(sum[:8])+".lock")
	lock, err := acquireLock(lockPath, afterUpdate())
	if err != nil {
		// Läuft schon: das vorhandene Fenster nochmals öffnen
		if b, rerr := os.ReadFile(lockPath); rerr == nil && strings.HasPrefix(string(b), "http://127.0.0.1:") {
			openBrowser(strings.TrimSpace(string(b)))
			return
		}
		fatal("StickTresor ist bereits geöffnet.")
		return
	}
	defer func() { lock.Close(); os.Remove(lockPath) }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal("Der lokale Zugang konnte nicht geöffnet werden: " + err.Error())
		return
	}
	host := ln.Addr().String()
	token := web.NewToken()
	url := "http://" + host + "/#t=" + token
	lock.Truncate(0)
	lock.WriteAt([]byte(url+"\n"), 0)

	plat := &unixPlatform{}
	a := app.New(dir, plat)

	var (
		mu       sync.Mutex
		lastSeen = time.Now()
		byeAt    time.Time
	)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second}
	var once sync.Once
	quit := func() {
		once.Do(func() {
			go func() {
				time.Sleep(400 * time.Millisecond) // Antwort an den Browser noch senden
				a.Lock()
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				srv.Shutdown(ctx)
			}()
		})
	}
	plat.quit = quit
	s := &web.Server{
		Binds: a.Bindings(),
		Page:  ui.Page(),
		Token: token,
		Host:  host,
		OnActivity: func() {
			mu.Lock()
			lastSeen = time.Now()
			byeAt = time.Time{}
			mu.Unlock()
		},
		OnBye: func() {
			mu.Lock()
			byeAt = time.Now()
			mu.Unlock()
		},
	}
	srv.Handler = s.Handler()

	// Beenden, wenn das Fenster geschlossen wurde (und nicht gleich wieder lädt)
	// oder sehr lange kein Lebenszeichen kam (Browser drosseln Hintergrund-Tabs).
	go func() {
		for range time.Tick(time.Second) {
			mu.Lock()
			closed := !byeAt.IsZero() && time.Since(byeAt) > 5*time.Second
			idle := time.Since(lastSeen) > 15*time.Minute
			mu.Unlock()
			if closed || idle {
				quit()
				return
			}
		}
	}()

	fmt.Println("StickTresor läuft. Falls sich kein Fenster öffnet, diese Adresse im Browser öffnen:")
	fmt.Println(url)
	go openBrowser(url)
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fatal(err.Error())
	}
	a.Lock()
	plat.ClearClipboardIfOurs()
}

// acquireLock sperrt die Datei exklusiv. Nach einem Update wird kurz gewartet,
// bis die alte Version beendet ist.
func acquireLock(path string, wait bool) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	for i := 0; ; i++ {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if !wait || i >= 40 {
			f.Close()
			return nil, err
		}
		time.Sleep(250 * time.Millisecond)
	}
}
