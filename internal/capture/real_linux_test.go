//go:build linux

package capture

import (
	"os"
	"testing"
)

func TestRealLinux(t *testing.T) {
	if os.Getenv("ST_REAL_CAPTURE") != "1" {
		t.Skip("nur mit ST_REAL_CAPTURE=1")
	}
	r := collectLinux()
	t.Logf("Name: %s", r.Name)
	for _, f := range r.Fields {
		v := f.Value
		if len(v) > 300 {
			v = v[:300] + " …"
		}
		t.Logf("%-18s %s", f.Key, v)
	}
	if r.Wifi != nil {
		t.Logf("WLAN %s (Passwort %d Zeichen)", r.Wifi.SSID, len(r.Wifi.Password))
	}
}
