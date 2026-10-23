//go:build windows

package capture

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unicode/utf16"
)

const createNoWindow = 0x08000000

// Collect liest diesen PC aus.
func Collect() (Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		b[2*i] = byte(c)
		b[2*i+1] = byte(c >> 8)
	}
	enc := base64.StdEncoding.EncodeToString(b)

	ps := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if _, err := os.Stat(ps); err != nil {
		ps = "powershell.exe"
	}
	cmd := exec.CommandContext(ctx, ps, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", enc)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Result{}, errors.New("Die Erfassung hat zu lange gedauert.")
		}
		return Result{}, fmt.Errorf("PowerShell konnte nicht ausgeführt werden: %v %s", err, errOut.String())
	}
	data := bytes.TrimSpace(bytes.TrimPrefix(out.Bytes(), []byte{0xEF, 0xBB, 0xBF}))
	// Nur die letzte Zeile ist das JSON (falls Profile o.Ä. etwas ausgeben)
	if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
		data = data[i+1:]
	}
	var r Result
	if err := json.Unmarshal(data, &r); err != nil {
		return Result{}, fmt.Errorf("Antwort der Erfassung unlesbar: %v", err)
	}
	if r.Name == "" {
		r.Name, _ = os.Hostname()
	}
	return r, nil
}
