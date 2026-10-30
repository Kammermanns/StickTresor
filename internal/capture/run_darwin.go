//go:build darwin

package capture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Collect liest diesen Mac aus (system_profiler und Bordmittel, keine Adminrechte).
func Collect() (Result, error) {
	if UseFake {
		return fakeCollect()
	}
	return collectMac(), nil
}

// sp ruft system_profiler mit JSON-Ausgabe auf und liefert die Liste des Datentyps.
func sp(dataType string, timeout time.Duration) []map[string]any {
	out := run(timeout, "/usr/sbin/system_profiler", "-json", dataType)
	if out == "" {
		return nil
	}
	var m map[string][]map[string]any
	if json.Unmarshal([]byte(out), &m) != nil {
		return nil
	}
	return m[dataType]
}

func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch x := v.(type) {
			case string:
				if strings.TrimSpace(x) != "" {
					return strings.TrimSpace(x)
				}
			case float64:
				return fmt.Sprintf("%.0f", x)
			}
		}
	}
	return ""
}

func num(m map[string]any, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}

func collectMac() Result {
	f := &fields{}
	name := strings.TrimSpace(run(5*time.Second, "/usr/sbin/scutil", "--get", "ComputerName"))
	if name == "" {
		name, _ = os.Hostname()
	}

	// --- Gerät ---
	hw := map[string]any{}
	if l := sp("SPHardwareDataType", 30*time.Second); len(l) > 0 {
		hw = l[0]
	}
	model := str(hw, "machine_name")
	if id := str(hw, "machine_model", "model_number"); id != "" {
		if model != "" {
			model += " (" + id + ")"
		} else {
			model = id
		}
	}
	f.add("Hersteller", "Apple")
	f.add("Modell", model)
	typ := "Desktop"
	if strings.Contains(str(hw, "machine_name"), "Book") {
		typ = "Notebook"
	}
	if str(hw, "machine_name") == "" {
		typ = ""
	}
	f.add("Gerätetyp", typ)
	if sn := str(hw, "serial_number"); good(sn) {
		f.add("Seriennummer", sn)
	}

	// --- Hardware ---
	cpu := str(hw, "chip_type", "cpu_type")
	if speed := str(hw, "current_processor_speed"); speed != "" && !strings.Contains(cpu, "Apple") {
		cpu += " " + speed
	}
	if n := str(hw, "number_processors"); n != "" {
		// Apple Silicon: «proc 10:8:2» = gesamt:Leistung:Effizienz
		if strings.HasPrefix(n, "proc ") {
			p := strings.Split(strings.TrimPrefix(n, "proc "), ":")
			if len(p) == 3 {
				cpu += fmt.Sprintf(" · %s Kerne (%s Leistung, %s Effizienz)", p[0], p[1], p[2])
			}
		} else {
			cpu += " · " + n + " Kerne"
		}
	}
	f.add("Prozessor", cpu)
	f.add("Arbeitsspeicher", str(hw, "physical_memory"))

	disks, vols := []string{}, []string{}
	seen := map[string]bool{}
	for _, v := range sp("SPStorageDataType", 30*time.Second) {
		pd, _ := v["physical_drive"].(map[string]any)
		if pd != nil && str(pd, "is_internal_disk") == "no" {
			continue // externe Laufwerke (auch der Stick) nicht aufführen
		}
		if pd != nil {
			dn := str(pd, "device_name")
			if dn != "" && !seen[dn] {
				seen[dn] = true
				kind := strings.ToUpper(str(pd, "medium_type"))
				parts := []string{dn}
				if k := strings.TrimSpace(kind + " " + str(pd, "protocol")); k != "" {
					parts = append(parts, k)
				}
				disks = append(disks, strings.Join(parts, " · "))
			}
		}
		mp := str(v, "mount_point")
		if mp == "/" || mp == "/System/Volumes/Data" || strings.HasPrefix(mp, "/Volumes/") {
			if size := num(v, "size_in_bytes"); size > 0 {
				vols = append(vols, fmt.Sprintf("%s %s, %s frei", str(v, "_name"), gb(size), gb(num(v, "free_space_in_bytes"))))
			}
		}
	}
	f.add("Datenträger", strings.Join(disks, "\n"))
	f.add("Laufwerke", strings.Join(uniqSorted(vols), "\n"))

	gpus, mons := []string{}, []string{}
	for _, g := range sp("SPDisplaysDataType", 30*time.Second) {
		gpus = append(gpus, str(g, "sppci_model", "_name"))
		if ds, ok := g["spdisplays_ndrvs"].([]any); ok {
			for _, d := range ds {
				dm, _ := d.(map[string]any)
				if dm == nil {
					continue
				}
				n := str(dm, "_name")
				if n == "" {
					continue
				}
				if str(dm, "spdisplays_connection_type") == "spdisplays_internal" {
					n += " (eingebaut)"
				}
				for k, v := range dm {
					if strings.Contains(strings.ToLower(k), "serial") {
						if s, ok := v.(string); ok && good(s) {
							n += " (SN: " + s + ")"
							break
						}
					}
				}
				mons = append(mons, n)
			}
		}
	}
	f.add("Grafik", strings.Join(uniqSorted(gpus), ", "))
	f.add("Monitore", strings.Join(mons, "\n"))

	prs := []string{}
	for _, p := range sp("SPPrintersDataType", 30*time.Second) {
		x := str(p, "_name")
		if x == "" {
			continue
		}
		if u := str(p, "uri"); u != "" {
			if h := uriHost(u); h != "" {
				x += " (IP " + h + ")"
			} else {
				x += " (" + u + ")"
			}
		}
		if str(p, "default") == "yes" {
			x += " · Standarddrucker"
		}
		prs = append(prs, x)
	}
	f.add("Drucker", strings.Join(prs, "\n"))

	// --- Betriebssystem & Programme ---
	osName := strings.TrimSpace(run(5*time.Second, "/usr/bin/sw_vers", "-productName"))
	osVer := strings.TrimSpace(run(5*time.Second, "/usr/bin/sw_vers", "-productVersion"))
	osBuild := strings.TrimSpace(run(5*time.Second, "/usr/bin/sw_vers", "-buildVersion"))
	osS := strings.TrimSpace(osName + " " + osVer)
	if osBuild != "" {
		osS += " (" + osBuild + ")"
	}
	f.add("Betriebssystem", osS)

	apps := []string{}
	av := []string{}
	office := map[string]string{}
	libre := ""
	for _, a := range sp("SPApplicationsDataType", 120*time.Second) {
		p := str(a, "path")
		n := str(a, "_name")
		if n == "" || !strings.HasPrefix(p, "/Applications/") || strings.HasPrefix(p, "/Applications/Utilities/") {
			continue
		}
		if str(a, "obtained_from") == "apple" && !strings.Contains(p, "/Applications/Pages") && !strings.Contains(p, "/Applications/Numbers") && !strings.Contains(p, "/Applications/Keynote") {
			continue // mitgelieferte Apple-Programme
		}
		ver := str(a, "version")
		apps = append(apps, n)
		for _, x := range avNames {
			if strings.Contains(strings.ToLower(n), strings.ToLower(x)) {
				av = append(av, n)
			}
		}
		switch n {
		case "Microsoft Word", "Microsoft Excel", "Microsoft PowerPoint", "Microsoft Outlook", "Microsoft OneNote":
			office[strings.TrimPrefix(n, "Microsoft ")] = ver
		case "LibreOffice":
			libre = "LibreOffice " + ver
		}
	}
	if len(av) == 0 {
		av = []string{"XProtect (in macOS enthalten)"}
	}
	f.add("Virenschutz", strings.Join(uniqSorted(av), ", "))
	if len(office) > 0 {
		names := []string{}
		ver := ""
		for k, v := range office {
			names = append(names, k)
			if ver == "" || v > ver {
				ver = v
			}
		}
		sort.Strings(names)
		s := "Microsoft Office (" + strings.Join(names, ", ")
		if ver != "" {
			s += ", Version " + ver
		}
		f.add("Office", s+")")
	} else if libre != "" {
		f.add("Office", libre)
	}
	browser, mail := macHandlers()
	f.add("Standard-Browser", browser)
	f.add("E-Mail-Programm", mail)
	f.add("Software", bulletList(uniqSorted(apps)))

	// --- Netzwerk ---
	ports := hardwarePorts()
	route := run(5*time.Second, "/sbin/route", "-n", "get", "default")
	dev, gw := lineValue(route, "interface"), lineValue(route, "gateway")
	if dev != "" {
		if strings.Contains(ports[dev], "Wi-Fi") || strings.Contains(ports[dev], "AirPort") {
			f.add("Netzwerk-Typ", "WLAN")
		} else {
			f.add("Netzwerk-Typ", "LAN")
		}
	}
	var wifi *Wifi
	for d, port := range ports {
		if !strings.Contains(port, "Wi-Fi") && !strings.Contains(port, "AirPort") {
			continue
		}
		ssid := lineValue(run(5*time.Second, "/usr/sbin/ipconfig", "getsummary", d), "SSID")
		if ssid == "" || strings.Contains(ssid, "redacted") {
			ssid = strings.TrimSpace(strings.TrimPrefix(run(5*time.Second, "/usr/sbin/networksetup", "-getairportnetwork", d), "Current Wi-Fi Network:"))
			if strings.Contains(ssid, "not associated") || strings.Contains(ssid, "Error") {
				ssid = ""
			}
		}
		if ssid != "" {
			f.add("WLAN-Name", ssid)
			// macOS fragt dabei nach dem Benutzerpasswort (Schlüsselbund). Abbrechen ist möglich.
			pw := strings.TrimSpace(run(60*time.Second, "/usr/bin/security", "find-generic-password", "-D", "AirPort network password", "-a", ssid, "-w"))
			wifi = &Wifi{SSID: ssid, Password: pw}
		}
		break
	}
	if dev != "" {
		if ip := strings.TrimSpace(run(5*time.Second, "/usr/sbin/ipconfig", "getifaddr", dev)); ip != "" {
			s := ip
			if strings.TrimSpace(run(5*time.Second, "/usr/sbin/ipconfig", "getpacket", dev)) != "" {
				s += " (DHCP)"
			} else {
				s += " (fest)"
			}
			if gw != "" {
				s += " · Gateway " + gw
			}
			if dns := macDNS(); dns != "" {
				s += " · DNS " + dns
			}
			f.add("IP-Konfiguration", s)
		}
	}
	nd := []string{}
	for _, l := range strings.Split(run(5*time.Second, "/sbin/mount"), "\n") {
		if !strings.Contains(l, "(smbfs") && !strings.Contains(l, "(afpfs") && !strings.Contains(l, "(nfs") && !strings.Contains(l, "(webdav") {
			continue
		}
		src, rest, ok := strings.Cut(l, " on ")
		if !ok {
			continue
		}
		mp := rest
		if i := strings.Index(rest, " ("); i >= 0 {
			mp = rest[:i]
		}
		if i := strings.LastIndex(src, "@"); i >= 0 && strings.HasPrefix(src, "//") {
			src = "//" + src[i+1:] // Benutzername nicht speichern
		}
		nd = append(nd, mp+" ("+src+")")
	}
	f.add("Netzlaufwerke", strings.Join(uniqSorted(nd), "\n"))

	return Result{Name: name, Fields: f.list, Wifi: wifi}
}

var reHost = regexp.MustCompile(`^[a-z]+://(?:[^@/]*@)?\[?([0-9A-Za-z.\-:]+?)\]?(?::\d+)?(?:/|$)`)

func uriHost(u string) string {
	m := reHost.FindStringSubmatch(u)
	if m == nil || strings.HasPrefix(u, "usb:") || strings.HasPrefix(u, "dnssd:") {
		return ""
	}
	return m[1]
}

// hardwarePorts ordnet Geräte (en0) ihren Anschlüssen zu («Wi-Fi», «Ethernet»).
func hardwarePorts() map[string]string {
	out := map[string]string{}
	port := ""
	for _, l := range strings.Split(run(10*time.Second, "/usr/sbin/networksetup", "-listallhardwareports"), "\n") {
		if v, ok := strings.CutPrefix(l, "Hardware Port:"); ok {
			port = strings.TrimSpace(v)
		}
		if v, ok := strings.CutPrefix(l, "Device:"); ok && port != "" {
			out[strings.TrimSpace(v)] = port
		}
	}
	return out
}

func macDNS() string {
	seen := map[string]bool{}
	list := []string{}
	for _, l := range strings.Split(run(5*time.Second, "/usr/sbin/scutil", "--dns"), "\n") {
		if strings.Contains(l, "nameserver[") {
			v := strings.TrimSpace(l[strings.Index(l, ":")+1:])
			if v != "" && !seen[v] && strings.Contains(v, ".") {
				seen[v] = true
				list = append(list, v)
			}
		}
		if len(list) >= 2 {
			break
		}
	}
	return strings.Join(list, ", ")
}

// macHandlers liest Standard-Browser und E-Mail-Programm aus den LaunchServices.
func macHandlers() (string, string) {
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, "Library", "Preferences", "com.apple.LaunchServices", "com.apple.launchservices.secure.plist")
	out := run(5*time.Second, "/usr/bin/plutil", "-convert", "json", "-o", "-", p)
	browser, mail := "Safari", "Apple Mail"
	var m struct {
		LSHandlers []map[string]any `json:"LSHandlers"`
	}
	if json.Unmarshal([]byte(out), &m) == nil {
		for _, h := range m.LSHandlers {
			scheme := str(h, "LSHandlerURLScheme")
			id := str(h, "LSHandlerRoleAll")
			if id == "" || id == "-" {
				continue
			}
			switch scheme {
			case "https", "http":
				browser = browserName(id)
			case "mailto":
				mail = browserName(id)
			}
		}
	}
	return browser, mail
}
