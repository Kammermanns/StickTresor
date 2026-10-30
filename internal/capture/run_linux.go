//go:build linux

package capture

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Collect liest diesen Linux-PC aus (/sys, /proc und Systemprogramme, keine Adminrechte).
// Werte, die nur root lesen darf (z.B. die Seriennummer), bleiben leer.
func Collect() (Result, error) {
	if UseFake {
		return fakeCollect()
	}
	return collectLinux(), nil
}

func dmi(k string) string { return readFile("/sys/class/dmi/id/" + k) }

func collectLinux() Result {
	f := &fields{}
	name, _ := os.Hostname()

	// --- Gerät ---
	vendor := dmi("sys_vendor")
	f.add("Hersteller", vendor)
	model := dmi("product_name")
	if v := dmi("product_version"); good(v) && strings.EqualFold(vendor, "LENOVO") {
		model = v + " (" + model + ")"
	}
	f.add("Modell", model)
	typ := ""
	switch dmi("chassis_type") {
	case "3", "4", "5", "6", "7", "15", "16", "24", "35", "36":
		typ = "Desktop"
	case "8", "9", "10", "14", "31", "32":
		typ = "Notebook"
	case "30":
		typ = "Tablet"
	case "17", "23", "28", "29":
		typ = "Server"
	}
	virt := strings.TrimSpace(run(5*time.Second, "systemd-detect-virt"))
	if (virt != "" && virt != "none") || strings.Contains(strings.ToLower(vendor+model), "qemu") || strings.Contains(strings.ToLower(model), "virtual") {
		typ = "Virtuelle Maschine"
	}
	f.add("Gerätetyp", typ)
	for _, k := range []string{"product_serial", "chassis_serial", "board_serial"} {
		if sn := dmi(k); good(sn) { // meist nur für root lesbar
			f.add("Seriennummer", sn)
			break
		}
	}

	// --- Hardware ---
	f.add("Prozessor", cpuInfo())
	if mt := lineValue(readFile("/proc/meminfo"), "MemTotal"); mt != "" {
		kb, _ := strconv.ParseFloat(strings.Fields(mt)[0], 64)
		f.add("Arbeitsspeicher", fmt.Sprintf("%.0f GB", kb/1024/1024))
	}
	f.add("Datenträger", linuxDisks())
	f.add("Laufwerke", linuxMounts())
	f.add("Grafik", linuxGPU())
	f.add("Monitore", linuxMonitors())
	f.add("Drucker", linuxPrinters())

	// --- Betriebssystem & Programme ---
	osr := readFile("/etc/os-release")
	osName := strings.Trim(lineValue(osr, "PRETTY_NAME"), `"`)
	if k := readFile("/proc/sys/kernel/osrelease"); k != "" {
		osName += " · Kernel " + k
	}
	f.add("Betriebssystem", osName)
	apps := desktopApps()
	av := []string{}
	if has("clamscan") || has("clamd") || has("clamdscan") {
		av = append(av, "ClamAV")
	}
	for _, a := range apps {
		for _, x := range avNames {
			if strings.Contains(strings.ToLower(a), strings.ToLower(x)) {
				av = append(av, a)
			}
		}
	}
	for dir, n := range map[string]string{"/opt/sophos-spl": "Sophos", "/opt/eset": "ESET", "/opt/microsoft/mdatp": "Microsoft Defender"} {
		if _, err := os.Stat(dir); err == nil {
			av = append(av, n)
		}
	}
	f.add("Virenschutz", strings.Join(uniqSorted(av), ", "))
	office := ""
	if has("libreoffice") || has("soffice") {
		bin := "libreoffice"
		if !has(bin) {
			bin = "soffice"
		}
		v := strings.Fields(run(15*time.Second, bin, "--version"))
		office = "LibreOffice"
		if len(v) >= 2 {
			office += " " + v[1]
		}
	} else {
		for _, a := range apps {
			if strings.Contains(a, "LibreOffice") || strings.Contains(a, "ONLYOFFICE") || strings.Contains(a, "WPS") {
				office = a
				break
			}
		}
	}
	f.add("Office", office)
	f.add("Standard-Browser", desktopName(strings.TrimSpace(run(5*time.Second, "xdg-settings", "get", "default-web-browser"))))
	f.add("E-Mail-Programm", desktopName(strings.TrimSpace(run(5*time.Second, "xdg-mime", "query", "default", "x-scheme-handler/mailto"))))
	f.add("Software", bulletList(apps))

	// --- Netzwerk ---
	var wifi *Wifi
	route := run(5*time.Second, "ip", "route", "show", "default")
	dev, gw, dhcp := "", "", false
	if fs := strings.Fields(strings.Split(route, "\n")[0]); len(fs) > 0 {
		for i := 0; i+1 < len(fs); i++ {
			switch fs[i] {
			case "dev":
				dev = fs[i+1]
			case "via":
				gw = fs[i+1]
			case "proto":
				dhcp = fs[i+1] == "dhcp"
			}
		}
	}
	types := []string{}
	for _, n := range physicalNICs() {
		if readFile("/sys/class/net/"+n+"/operstate") != "up" {
			continue
		}
		if _, err := os.Stat("/sys/class/net/" + n + "/wireless"); err == nil {
			types = append(types, "WLAN")
		} else {
			types = append(types, "LAN")
		}
	}
	f.add("Netzwerk-Typ", strings.Join(uniqSorted(types), " + "))
	if ssid := currentSSID(); ssid != "" {
		f.add("WLAN-Name", ssid)
		wifi = &Wifi{SSID: ssid, Password: wifiPassword()}
	}
	if dev != "" {
		addr := strings.Fields(run(5*time.Second, "ip", "-4", "-o", "addr", "show", "dev", dev))
		ip := ""
		for i := 0; i+1 < len(addr); i++ {
			if addr[i] == "inet" {
				ip = strings.Split(addr[i+1], "/")[0]
				if strings.Contains(strings.Join(addr, " "), "dynamic") {
					dhcp = true
				}
				break
			}
		}
		if ip != "" {
			s := ip
			if dhcp {
				s += " (DHCP)"
			} else {
				s += " (fest)"
			}
			if gw != "" {
				s += " · Gateway " + gw
			}
			if dns := linuxDNS(dev); dns != "" {
				s += " · DNS " + dns
			}
			f.add("IP-Konfiguration", s)
		}
	}
	f.add("Netzlaufwerke", linuxNetDrives())
	f.add("SMB-Freigaben", linuxShares(name))

	return Result{Name: name, Fields: f.list, Wifi: wifi}
}

func has(name string) bool {
	for _, d := range filepath.SplitList(os.Getenv("PATH") + ":/usr/sbin:/sbin") {
		if st, err := os.Stat(filepath.Join(d, name)); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

func cpuInfo() string {
	txt := readFile("/proc/cpuinfo")
	model := lineValue(txt, "model name")
	if model == "" {
		model = lineValue(txt, "Model") // ARM (z.B. Raspberry Pi)
	}
	threads := strings.Count(txt, "\nprocessor") + 1
	cores := map[string]bool{}
	phys := ""
	for _, l := range strings.Split(txt, "\n") {
		if strings.HasPrefix(l, "physical id") {
			phys = l
		}
		if strings.HasPrefix(l, "core id") {
			cores[phys+"|"+l] = true
		}
	}
	model = strings.Join(strings.Fields(model), " ")
	if model == "" {
		return ""
	}
	if len(cores) > 0 {
		return fmt.Sprintf("%s · %d Kerne / %d Threads", model, len(cores), threads)
	}
	return fmt.Sprintf("%s · %d Kerne", model, threads)
}

func linuxDisks() string {
	out := run(10*time.Second, "lsblk", "-J", "-b", "-d", "-o", "NAME,SIZE,MODEL,ROTA,TRAN,TYPE")
	var j struct {
		Blockdevices []struct {
			Name  string `json:"name"`
			Size  any    `json:"size"`
			Model any    `json:"model"`
			Rota  any    `json:"rota"`
			Tran  any    `json:"tran"`
			Type  string `json:"type"`
		} `json:"blockdevices"`
	}
	if json.Unmarshal([]byte(out), &j) != nil {
		return ""
	}
	list := []string{}
	for _, d := range j.Blockdevices {
		tran := fmt.Sprint(d.Tran)
		if d.Type != "disk" || tran == "usb" || strings.HasPrefix(d.Name, "zram") || strings.HasPrefix(d.Name, "loop") {
			continue
		}
		size, _ := strconv.ParseFloat(fmt.Sprint(d.Size), 64)
		if size < 4<<30 {
			continue // Kleinstspeicher (z.B. Kartenleser ohne Karte)
		}
		model := strings.TrimSpace(fmt.Sprint(d.Model))
		if model == "" || model == "<nil>" {
			model = d.Name
		}
		kind := "SSD"
		if r := fmt.Sprint(d.Rota); r == "true" || r == "1" {
			kind = "HDD"
		}
		if strings.HasPrefix(d.Name, "nvme") {
			kind = "SSD NVMe"
		} else if tran != "<nil>" && tran != "" {
			kind += " " + strings.ToUpper(tran)
		}
		list = append(list, fmt.Sprintf("%s · %s · %s", model, kind, gb(size)))
	}
	return strings.Join(list, "\n")
}

func linuxMounts() string {
	out := run(10*time.Second, "df", "-B1", "--output=target,size,avail", "-x", "tmpfs", "-x", "devtmpfs", "-x", "squashfs", "-x", "overlay", "-x", "efivarfs", "-x", "vfat", "-x", "exfat",
		"-x", "9p", "-x", "virtiofs", "-x", "fuse", "-x", "fuse.sshfs", "-x", "fuse.portal", "-x", "fuse.gvfsd-fuse", "-x", "nfs", "-x", "nfs4", "-x", "cifs", "-x", "smb3")
	fstype := map[string]string{}
	for _, l := range strings.Split(readFile("/proc/mounts"), "\n") {
		if x := strings.Fields(l); len(x) >= 3 {
			fstype[x[1]] = x[2]
		}
	}
	list := []string{}
	for i, l := range strings.Split(out, "\n") {
		fs := strings.Fields(l)
		if i == 0 || len(fs) != 3 {
			continue
		}
		mp := fs[0]
		if t := fstype[mp]; strings.HasPrefix(t, "fuse") {
			continue // Cloud- und Netzlaufwerke über FUSE
		}
		if strings.HasPrefix(mp, "/boot") || strings.HasPrefix(mp, "/media") || strings.HasPrefix(mp, "/run") || strings.HasPrefix(mp, "/snap") || strings.HasPrefix(mp, "/var/lib") {
			continue
		}
		size, _ := strconv.ParseFloat(fs[1], 64)
		free, _ := strconv.ParseFloat(fs[2], 64)
		if size < 1<<30 {
			continue
		}
		list = append(list, fmt.Sprintf("%s %s, %s frei", mp, gb(size), gb(free)))
	}
	return strings.Join(list, "\n")
}

func linuxGPU() string {
	list := []string{}
	for _, l := range strings.Split(run(10*time.Second, "lspci", "-mm"), "\n") {
		if !strings.Contains(l, "VGA compatible controller") && !strings.Contains(l, "3D controller") && !strings.Contains(l, "Display controller") {
			continue
		}
		q := strings.Split(l, `"`)
		// Feld 3 = Hersteller, Feld 5 = Gerät
		if len(q) >= 6 {
			list = append(list, strings.TrimSpace(q[3]+" "+q[5]))
		}
	}
	return strings.Join(uniqSorted(list), ", ")
}

// linuxMonitors liest Name und Seriennummer der angeschlossenen Monitore aus dem EDID.
func linuxMonitors() string {
	list := []string{}
	conns, _ := filepath.Glob("/sys/class/drm/card*-*")
	for _, c := range conns {
		if readFile(filepath.Join(c, "status")) != "connected" {
			continue
		}
		edid, _ := os.ReadFile(filepath.Join(c, "edid"))
		name, sn := edidInfo(edid)
		if name == "" {
			name = "Monitor " + strings.SplitN(filepath.Base(c), "-", 2)[1]
		}
		internal := strings.Contains(c, "-eDP") || strings.Contains(c, "-LVDS") || strings.Contains(c, "-DSI")
		if internal {
			name += " (eingebaut)"
		}
		if sn != "" {
			name += " (SN: " + sn + ")"
		}
		list = append(list, name)
	}
	return strings.Join(list, "\n")
}

func edidInfo(e []byte) (name, serial string) {
	if len(e) < 128 {
		return "", ""
	}
	mfg := ""
	if v := uint16(e[8])<<8 | uint16(e[9]); v != 0 {
		mfg = string([]byte{byte('A' - 1 + (v>>10)&0x1f), byte('A' - 1 + (v>>5)&0x1f), byte('A' - 1 + v&0x1f)})
	}
	for _, off := range []int{54, 72, 90, 108} {
		d := e[off : off+18]
		if d[0] != 0 || d[1] != 0 {
			continue
		}
		txt := strings.TrimSpace(strings.Split(string(d[5:18]), "\n")[0])
		switch d[3] {
		case 0xFC:
			name = txt
		case 0xFF:
			if good(txt) {
				serial = txt
			}
		}
	}
	if name == "" && mfg != "" {
		name = "Monitor " + mfg
	} else if mfg != "" && !strings.HasPrefix(strings.ToUpper(name), mfg) {
		name = mfg + " " + name
	}
	return name, serial
}

func linuxPrinters() string {
	def := strings.TrimSpace(strings.TrimPrefix(run(5*time.Second, "lpstat", "-d"), "system default destination:"))
	list := []string{}
	for _, l := range strings.Split(run(10*time.Second, "lpstat", "-v"), "\n") {
		rest, ok := strings.CutPrefix(l, "device for ")
		if !ok {
			continue
		}
		n, uri, _ := strings.Cut(rest, ": ")
		x := n
		if h := uriHostLinux(uri); h != "" {
			x += " (IP " + h + ")"
		} else if uri != "" {
			x += " (" + strings.TrimSpace(uri) + ")"
		}
		if n == def {
			x += " · Standarddrucker"
		}
		list = append(list, x)
	}
	return strings.Join(list, "\n")
}

func uriHostLinux(u string) string {
	u = strings.TrimSpace(u)
	for _, p := range []string{"ipp://", "ipps://", "socket://", "lpd://", "http://", "https://", "smb://"} {
		if rest, ok := strings.CutPrefix(u, p); ok {
			if i := strings.LastIndex(rest, "@"); i >= 0 {
				rest = rest[i+1:]
			}
			h := strings.FieldsFunc(rest, func(r rune) bool { return r == '/' || r == ':' })
			if len(h) > 0 && !strings.Contains(h[0], ".local") && !strings.Contains(h[0], "%") {
				return h[0]
			}
		}
	}
	return ""
}

// desktopApps listet die installierten Programme mit Startmenü-Eintrag.
func desktopApps() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{"/usr/share/applications", "/usr/local/share/applications", filepath.Join(home, ".local/share/applications"),
		"/var/lib/flatpak/exports/share/applications", filepath.Join(home, ".local/share/flatpak/exports/share/applications"), "/var/lib/snapd/desktop/applications"}
	names := []string{}
	for _, d := range dirs {
		files, _ := filepath.Glob(filepath.Join(d, "*.desktop"))
		for _, file := range files {
			if n := desktopEntryName(file, true); n != "" {
				names = append(names, n)
			}
		}
	}
	return uniqSorted(names)
}

// desktopEntryName liest «Name=» aus einer .desktop-Datei; skipHidden lässt Systemeinträge weg.
func desktopEntryName(path string, skipHidden bool) string {
	fh, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer fh.Close()
	name, inMain := "", false
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(l, "[") {
			inMain = l == "[Desktop Entry]"
			continue
		}
		if !inMain {
			continue
		}
		switch {
		case strings.HasPrefix(l, "Name=") && name == "":
			name = strings.TrimPrefix(l, "Name=")
		case skipHidden && (l == "NoDisplay=true" || l == "Hidden=true" || l == "Terminal=true"):
			return ""
		case skipHidden && strings.HasPrefix(l, "Type=") && l != "Type=Application":
			return ""
		case skipHidden && strings.HasPrefix(l, "Categories=") && (strings.Contains(l, "Settings") || strings.Contains(l, "System;") && !strings.Contains(l, "TerminalEmulator")):
			return ""
		case skipHidden && strings.HasPrefix(l, "OnlyShowIn="):
			return ""
		}
	}
	return name
}

// desktopName macht aus «firefox.desktop» einen lesbaren Namen.
func desktopName(id string) string {
	if id == "" {
		return ""
	}
	home, _ := os.UserHomeDir()
	for _, d := range []string{filepath.Join(home, ".local/share/applications"), "/usr/share/applications", "/usr/local/share/applications",
		"/var/lib/flatpak/exports/share/applications", "/var/lib/snapd/desktop/applications"} {
		if n := desktopEntryName(filepath.Join(d, id), false); n != "" {
			return n
		}
	}
	return browserName(strings.TrimSuffix(id, ".desktop"))
}

func physicalNICs() []string {
	list := []string{}
	ents, _ := os.ReadDir("/sys/class/net")
	for _, e := range ents {
		if _, err := os.Stat(filepath.Join("/sys/class/net", e.Name(), "device")); err == nil {
			list = append(list, e.Name())
		}
	}
	return list
}

func currentSSID() string {
	for _, l := range strings.Split(run(10*time.Second, "nmcli", "-t", "-f", "ACTIVE,SSID", "dev", "wifi"), "\n") {
		if v, ok := strings.CutPrefix(l, "yes:"); ok {
			return strings.ReplaceAll(v, `\:`, ":")
		}
	}
	return strings.TrimSpace(run(5*time.Second, "iwgetid", "-r"))
}

// wifiPassword liest das Passwort der aktiven WLAN-Verbindung über NetworkManager
// (gelingt, wenn der angemeldete Benutzer die Verbindung verwalten darf).
func wifiPassword() string {
	for _, l := range strings.Split(run(10*time.Second, "nmcli", "-t", "-f", "NAME,TYPE", "connection", "show", "--active"), "\n") {
		i := strings.LastIndex(l, ":")
		if i < 0 || !strings.Contains(l[i+1:], "wireless") {
			continue
		}
		conn := strings.ReplaceAll(l[:i], `\:`, ":")
		return strings.TrimSpace(run(15*time.Second, "nmcli", "-s", "-g", "802-11-wireless-security.psk", "connection", "show", "id", conn))
	}
	return ""
}

func linuxDNS(dev string) string {
	list := []string{}
	if out := run(5*time.Second, "resolvectl", "dns", dev); out != "" {
		if _, v, ok := strings.Cut(out, ":"); ok {
			list = strings.Fields(v)
		}
	}
	if len(list) == 0 {
		for _, l := range strings.Split(readFile("/etc/resolv.conf"), "\n") {
			if v, ok := strings.CutPrefix(l, "nameserver"); ok {
				if v = strings.TrimSpace(v); !strings.HasPrefix(v, "127.") {
					list = append(list, v)
				}
			}
		}
	}
	if len(list) > 2 {
		list = list[:2]
	}
	return strings.Join(list, ", ")
}

func linuxNetDrives() string {
	list := []string{}
	for _, l := range strings.Split(readFile("/proc/mounts"), "\n") {
		fs := strings.Fields(l)
		if len(fs) < 3 {
			continue
		}
		switch fs[2] {
		case "cifs", "smb3", "nfs", "nfs4", "fuse.sshfs", "davfs":
			src := fs[0]
			if i := strings.LastIndex(src, "@"); i >= 0 {
				src = src[i+1:]
			}
			list = append(list, strings.ReplaceAll(fs[1], `\040`, " ")+" ("+src+")")
		}
	}
	// In der Dateiverwaltung (GNOME/gvfs) verbundene Freigaben
	if u, err := user.Current(); err == nil {
		ents, _ := os.ReadDir("/run/user/" + u.Uid + "/gvfs")
		for _, e := range ents {
			n := e.Name()
			if rest, ok := strings.CutPrefix(n, "smb-share:"); ok {
				srv, share := "", ""
				for _, kv := range strings.Split(rest, ",") {
					k, v, _ := strings.Cut(kv, "=")
					if k == "server" {
						srv = v
					}
					if k == "share" {
						share = v
					}
				}
				list = append(list, "//"+srv+"/"+share+" (Dateiverwaltung)")
			}
		}
	}
	return strings.Join(uniqSorted(list), "\n")
}

func linuxShares(host string) string {
	list := []string{}
	name, path := "", ""
	flush := func() {
		if name != "" && path != "" {
			list = append(list, path+" (\\\\"+host+"\\"+name+")")
		}
	}
	for _, l := range strings.Split(run(5*time.Second, "net", "usershare", "info"), "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "[") && strings.HasSuffix(l, "]") {
			flush()
			name, path = strings.Trim(l, "[]"), ""
		}
		if v, ok := strings.CutPrefix(l, "path="); ok {
			path = v
		}
	}
	flush()
	return strings.Join(list, "\n")
}
