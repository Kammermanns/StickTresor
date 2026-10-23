//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                      = windows.NewLazySystemDLL("user32.dll")
	kernel32                    = windows.NewLazySystemDLL("kernel32.dll")
	pOpenClipboard              = user32.NewProc("OpenClipboard")
	pCloseClipboard             = user32.NewProc("CloseClipboard")
	pEmptyClipboard             = user32.NewProc("EmptyClipboard")
	pSetClipboardData           = user32.NewProc("SetClipboardData")
	pRegisterClipboardFormat    = user32.NewProc("RegisterClipboardFormatW")
	pGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	pMessageBox                 = user32.NewProc("MessageBoxW")
	pGlobalAlloc                = kernel32.NewProc("GlobalAlloc")
	pGlobalLock                 = kernel32.NewProc("GlobalLock")
	pGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
	pGlobalFree                 = kernel32.NewProc("GlobalFree")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

type winPlatform struct {
	quit func()
	mu   sync.Mutex
	seq  uintptr // Sequenznummer nach unserem letzten Kopieren
	gen  int
}

func openClipboard() error {
	for i := 0; i < 20; i++ {
		if r, _, _ := pOpenClipboard.Call(0); r != 0 {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return errors.New("Die Zwischenablage ist gerade belegt. Bitte nochmals versuchen.")
}

func globalBytes(b []byte) (uintptr, error) {
	h, _, _ := pGlobalAlloc.Call(gmemMoveable, uintptr(len(b)))
	if h == 0 {
		return 0, errors.New("Speicher für die Zwischenablage nicht verfügbar")
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		pGlobalFree.Call(h)
		return 0, errors.New("Zwischenablage gesperrt")
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(p)), len(b)), b)
	pGlobalUnlock.Call(h)
	return h, nil
}

func registerFormat(name string) uintptr {
	p, _ := syscall.UTF16PtrFromString(name)
	r, _, _ := pRegisterClipboardFormat.Call(uintptr(unsafe.Pointer(p)))
	return r
}

// CopyToClipboard kopiert Text, schliesst ihn vom Windows-Zwischenablageverlauf
// und der Cloud-Synchronisation aus und löscht ihn nach clearAfter wieder.
func (w *winPlatform) CopyToClipboard(text string, clearAfter time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	u := utf16.Encode([]rune(text + "\x00"))
	b := unsafe.Slice((*byte)(unsafe.Pointer(&u[0])), len(u)*2)
	if err := openClipboard(); err != nil {
		return err
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	h, err := globalBytes(b)
	if err != nil {
		return err
	}
	if r, _, _ := pSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		pGlobalFree.Call(h)
		return errors.New("Kopieren fehlgeschlagen")
	}
	zero := []byte{0, 0, 0, 0}
	for _, f := range []string{"CanIncludeInClipboardHistory", "CanUploadToCloudClipboard"} {
		if id := registerFormat(f); id != 0 {
			if hz, err := globalBytes(zero); err == nil {
				if r, _, _ := pSetClipboardData.Call(id, hz); r == 0 {
					pGlobalFree.Call(hz)
				}
			}
		}
	}
	if id := registerFormat("ExcludeClipboardContentFromMonitorProcessing"); id != 0 {
		if hz, err := globalBytes([]byte{0}); err == nil {
			if r, _, _ := pSetClipboardData.Call(id, hz); r == 0 {
				pGlobalFree.Call(hz)
			}
		}
	}
	pCloseClipboard.Call()
	w.seq, _, _ = pGetClipboardSequenceNumber.Call()
	w.gen++
	gen := w.gen
	go func() {
		time.Sleep(clearAfter)
		w.mu.Lock()
		same := gen == w.gen
		w.mu.Unlock()
		if same {
			w.ClearClipboardIfOurs()
		}
	}()
	return nil
}

// ClearClipboardIfOurs leert die Zwischenablage, wenn seit unserem Kopieren
// nichts anderes hineinkopiert wurde.
func (w *winPlatform) ClearClipboardIfOurs() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	w.mu.Lock()
	seq := w.seq
	w.mu.Unlock()
	if seq == 0 {
		return
	}
	cur, _, _ := pGetClipboardSequenceNumber.Call()
	if cur != seq {
		return
	}
	if openClipboard() == nil {
		pEmptyClipboard.Call()
		pCloseClipboard.Call()
	}
	w.mu.Lock()
	w.seq = 0
	w.mu.Unlock()
}

func (w *winPlatform) OpenFolder(path string) error {
	cmd := exec.Command("explorer.exe", path)
	return cmd.Start()
}

// PickFolder zeigt den Windows-Dialog zur Ordnerauswahl (über PowerShell).
func (w *winPlatform) PickFolder(title string) (string, error) {
	title = strings.ReplaceAll(title, "'", "''")
	script := `Add-Type -AssemblyName System.Windows.Forms
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding $false
$f = New-Object System.Windows.Forms.Form -Property @{TopMost = $true; ShowInTaskbar = $false; WindowState = 'Minimized'}
$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.Description = '` + title + `'
$d.ShowNewFolderButton = $true
if ($d.ShowDialog($f) -eq [System.Windows.Forms.DialogResult]::OK) { $d.SelectedPath }`
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		b[2*i] = byte(c)
		b[2*i+1] = byte(c >> 8)
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-STA", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodeB64(b))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.Output()
	if err != nil {
		return "", errors.New("Der Auswahldialog konnte nicht geöffnet werden.")
	}
	return strings.TrimSpace(strings.TrimPrefix(string(out), "\uFEFF")), nil
}

func messageBox(title, text string, flags uintptr) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	pMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags)
}

func (w *winPlatform) Quit() {
	if w.quit != nil {
		w.quit()
	}
}

func (w *winPlatform) OpenURL(u string) error {
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", u).Start()
}

// Signature prüft die Authenticode-Signatur mit PowerShell (Get-AuthenticodeSignature).
func (w *winPlatform) Signature(path string) (bool, string, error) {
	script := `[Console]::OutputEncoding=[Text.Encoding]::UTF8; $s = Get-AuthenticodeSignature -LiteralPath $env:ST_SIGFILE; Write-Output ([string]$s.Status + '|' + [string]$s.SignerCertificate.Subject)`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.Env = append(os.Environ(), "ST_SIGFILE="+path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.Output()
	if err != nil {
		return false, "", err
	}
	st, sub, _ := strings.Cut(strings.TrimSpace(string(out)), "|")
	return st == "Valid", sub, nil
}

// Restart startet das Programm neu; die neue Instanz wartet, bis diese beendet ist.
func (w *winPlatform) Restart(exe string) error {
	cmd := exec.Command(exe, "--nach-update")
	cmd.Dir = filepath.Dir(exe)
	return cmd.Start()
}
