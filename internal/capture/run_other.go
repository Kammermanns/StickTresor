//go:build !windows

package capture

import (
	"os"

	"github.com/Kammermanns/StickTresor/internal/vault"
)

// Collect liefert ausserhalb von Windows Beispieldaten (nur für Entwicklung und Tests).
func Collect() (Result, error) {
	name := os.Getenv("DM_FAKE_PC")
	if name == "" {
		name = "DM25-SURFACE"
	}
	f := func(k, v string) vault.Field { return vault.Field{Key: k, Value: v} }
	return Result{Name: name, Wifi: &Wifi{SSID: "Muster-Buero", Password: "Gruene-Wiese-2026"}, Fields: []vault.Field{
		f("Hersteller", "Microsoft Corporation"),
		f("Modell", "Surface Pro 7"),
		f("Gerätetyp", "Notebook"),
		f("Seriennummer", "035699303253"),
		f("Prozessor", "Intel(R) Core(TM) i5-1035G4 CPU @ 1.10GHz · 4 Kerne / 8 Threads"),
		f("Arbeitsspeicher", "8 GB (2 Module, 3733 MHz)"),
		f("Datenträger", "SAMSUNG KBG40ZPZ256G · SSD NVMe · 238 GB"),
		f("Laufwerke", "C: 237 GB, 98,4 GB frei"),
		f("Grafik", "Intel(R) Iris(R) Plus Graphics"),
		f("Monitore", "Monitor LGD\nDELL P2423D (SN: CN0F2K1M)\nDELL P2423D (SN: CN0F2K2X)"),
		f("Drucker", "HP LaserJet Pro M404dn (IP 192.168.11.40) · Treiber HP Universal Printing PCL 6 · Standarddrucker"),
		f("Betriebssystem", "Microsoft Windows 11 Pro"),
		f("Virenschutz", "Windows Defender"),
		f("Office", "Microsoft 365 (O365BusinessRetail, Version 16.0.19127.20154)"),
		f("Standard-Browser", "Microsoft Edge"),
		f("E-Mail-Programm", "Outlook"),
		f("Software", "- Google Chrome\n- Microsoft 365 - de-de\n- Microsoft OneDrive\n- TeamViewer\n- VLC media player\n- XAMPP"),
		f("Netzwerk-Typ", "LAN + WLAN"),
		f("WLAN-Name", "Muster-Buero"),
		f("IP-Konfiguration", "192.168.11.196 (DHCP) · Gateway 192.168.11.1 · DNS 192.168.11.1"),
		f("Netzlaufwerke", `N:\ (\\server\daten)`),
		f("SMB-Freigaben", `C:\freigabe (\\`+name+`\freigabe)`),
	}}, nil
}
