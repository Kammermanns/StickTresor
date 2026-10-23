// Package capture liest die Daten eines Windows-PCs aus (WMI/CIM über PowerShell).
package capture

import "github.com/Kammermanns/StickTresor/internal/vault"

// Wifi ist das aktuell verbundene WLAN. Das Passwort landet nicht im Datenblatt,
// sondern als verschlüsselter Passwort-Eintrag.
type Wifi struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
}

type Result struct {
	Name   string        `json:"name"`
	Fields []vault.Field `json:"fields"`
	Wifi   *Wifi         `json:"wifi"`
}

// Section beschreibt einen Block im Datenblatt.
type Section struct {
	Title    string   `json:"title"`
	Column   string   `json:"column"` // "left" oder "right"
	Keys     []string `json:"keys"`
	Multline bool     `json:"multline"` // Werte sind mehrzeilig (Liste)
}

// Sections legt Reihenfolge, Spalte und Gruppierung im Datenblatt fest.
// «WLAN-Passwort» wird nicht gespeichert, sondern aus dem Passwort-Eintrag «WLAN <Name>» angezeigt.
// Felder, die hier nicht vorkommen, erscheinen unter «Weitere Felder».
var Sections = []Section{
	{Title: "Gerät", Column: "left", Keys: []string{"Hersteller", "Modell", "Seriennummer", "Gerätetyp", "Standort", "Hauptbenutzer", "Kaufdatum", "Garantie bis"}},
	{Title: "Hardware", Column: "left", Keys: []string{"Prozessor", "Arbeitsspeicher", "Datenträger", "Laufwerke", "Grafik", "Monitore", "Drucker"}},
	{Title: "Netzwerk", Column: "left", Keys: []string{"Netzwerk-Typ", "WLAN-Name", "WLAN-Passwort", "IP-Konfiguration", "Netzlaufwerke", "SMB-Freigaben"}},
	{Title: "Betriebssystem & Software", Column: "right", Keys: []string{"Betriebssystem", "Virenschutz", "Office", "Standard-Browser", "E-Mail-Programm", "Software"}},
}

// CustomerKeys füllt der Kunde selbst aus; sie werden auch leer angezeigt.
var CustomerKeys = []string{"Standort", "Hauptbenutzer", "Kaufdatum", "Garantie bis"}

// script wird mit PowerShell 5.1 (in Windows 10/11 enthalten) ausgeführt.
// Es braucht keine Administratorrechte und gibt JSON aus.
const script = `
$ErrorActionPreference = 'SilentlyContinue'
$ProgressPreference = 'SilentlyContinue'
$UTF8 = New-Object System.Text.UTF8Encoding $false
[Console]::OutputEncoding = $UTF8
$OEM = [System.Text.Encoding]::GetEncoding([System.Globalization.CultureInfo]::CurrentCulture.TextInfo.OEMCodePage)
$NL = [string][char]10
$r = New-Object System.Collections.ArrayList
function Add($k, $v) {
  if ($v -ne $null) { $s = ("$v").Trim(); if ($s -ne '') { [void]$r.Add([ordered]@{ key = $k; value = $s }) } }
}
function D($d) { if ($d) { try { return ([datetime]$d).ToString('dd.MM.yyyy') } catch {} }; return $null }
function GB($b) { if ($b) { $g = [double]$b / 1GB; if ($g -ge 100) { return ('{0:N0} GB' -f $g) } else { return ('{0:N1} GB' -f $g) } } }
function Good($s) { return ($s -and ("$s".Trim().Length -ge 4) -and ("$s" -notmatch 'to be filled|default string|system serial|not specified|^0+$|none|chassis serial')) }
function Netsh($a) {
  [Console]::OutputEncoding = $OEM
  $o = & netsh.exe @a 2>$null
  [Console]::OutputEncoding = $UTF8
  return $o
}
function ProgName($p) {
  if (-not $p) { return $null }
  if ($p -match '^MSEdge') { return 'Microsoft Edge' }
  if ($p -match '^ChromeHTML') { return 'Google Chrome' }
  if ($p -match '^Firefox') { return 'Mozilla Firefox' }
  if ($p -match '^Brave') { return 'Brave' }
  if ($p -match '^Opera') { return 'Opera' }
  if ($p -match '^IE\.') { return 'Internet Explorer' }
  if ($p -match '^Outlook') { return 'Outlook' }
  if ($p -match 'olk|OutlookForWindows|msoutlook') { return 'Outlook (neu)' }
  if ($p -match '^Thunderbird') { return 'Thunderbird' }
  if ($p -match '^AppX') { return 'Windows-App (' + $p + ')' }
  return $p
}

$cs   = Get-CimInstance Win32_ComputerSystem
$csp  = Get-CimInstance Win32_ComputerSystemProduct
$bios = Get-CimInstance Win32_BIOS
$enc  = Get-CimInstance Win32_SystemEnclosure | Select-Object -First 1
$os   = Get-CimInstance Win32_OperatingSystem
$cv   = Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion'

# --- Gerät ---
Add 'Hersteller' $cs.Manufacturer
$model = $cs.Model
if ($cs.Manufacturer -match 'LENOVO' -and (Good $csp.Version)) { $model = $csp.Version + ' (' + $cs.Model + ')' }
Add 'Modell' $model
$typ = switch ([int]$cs.PCSystemType) { 1 {'Desktop'} 2 {'Notebook'} 3 {'Workstation'} 4 {'Server'} 5 {'Server'} 8 {'Tablet'} default {''} }
if ($cs.Model -match 'Virtual|VMware|KVM|HVM') { $typ = 'Virtuelle Maschine' }
Add 'Gerätetyp' $typ
$sn = $bios.SerialNumber
if (-not (Good $sn)) { $sn = $enc.SerialNumber }
if (-not (Good $sn)) { $sn = $csp.IdentifyingNumber }
Add 'Seriennummer' $sn

# --- Hardware ---
$cpu = Get-CimInstance Win32_Processor | Select-Object -First 1
if ($cpu) { Add 'Prozessor' (($cpu.Name -replace '\s+', ' ').Trim() + ' · ' + $cpu.NumberOfCores + ' Kerne / ' + $cpu.NumberOfLogicalProcessors + ' Threads') }
$mem = @(Get-CimInstance Win32_PhysicalMemory)
if ($mem.Count -gt 0) {
  $tot = ($mem | Measure-Object Capacity -Sum).Sum
  $speed = ($mem | Select-Object -First 1).ConfiguredClockSpeed
  $s = ((GB $tot) -replace '[,.]0 GB', ' GB') + ' (' + $mem.Count + ' Modul' + $(if ($mem.Count -gt 1) {'e'} else {''})
  if ($speed) { $s = $s + ', ' + $speed + ' MHz' }
  Add 'Arbeitsspeicher' ($s + ')')
} else { Add 'Arbeitsspeicher' (GB $cs.TotalPhysicalMemory) }
$disks = @()
$pd = @(Get-PhysicalDisk | Where-Object { "$($_.BusType)" -ne 'USB' })
if ($pd.Count -gt 0) {
  foreach ($d in $pd) {
    $mt = "$($d.MediaType)"; if ($mt -eq 'Unspecified' -or $mt -eq '0') { $mt = '' }
    $bt = "$($d.BusType)"
    $parts = @($d.FriendlyName, ((@($mt, $bt) | Where-Object { $_ }) -join ' '), (GB $d.Size)) | Where-Object { $_ }
    $disks += ($parts -join ' · ')
  }
} else {
  foreach ($d in Get-CimInstance Win32_DiskDrive | Where-Object { $_.InterfaceType -ne 'USB' }) { $disks += ($d.Model + ' · ' + (GB $d.Size)) }
}
Add 'Datenträger' ($disks -join $NL)
$ld = foreach ($l in Get-CimInstance Win32_LogicalDisk -Filter 'DriveType=3') { $l.DeviceID + ' ' + (GB $l.Size) + ', ' + (GB $l.FreeSpace) + ' frei' }
Add 'Laufwerke' ($ld -join $NL)
Add 'Grafik' ((Get-CimInstance Win32_VideoController | ForEach-Object { $_.Name } | Sort-Object -Unique) -join ', ')
function Txt($a) { if ($a) { return (($a | Where-Object { $_ -ne 0 } | ForEach-Object { [char]$_ }) -join '').Trim() } }
$mon = foreach ($m in Get-CimInstance -Namespace root/wmi -ClassName WmiMonitorID) {
  $n = Txt $m.UserFriendlyName
  if (-not $n) { $n = 'Monitor ' + (Txt $m.ManufacturerName) }
  $s = Txt $m.SerialNumberID
  if ($s -and $s -ne '0') { "$n (SN: $s)" } else { $n }
}
Add 'Monitore' ($mon -join $NL)

# --- Drucker ---
$ports = @{}
foreach ($p in Get-CimInstance Win32_TCPIPPrinterPort) { $ports[$p.Name] = $p.HostAddress }
$skip = 'Microsoft Print to PDF|Microsoft XPS Document Writer|OneNote|^Fax$|An OneNote senden|Send To OneNote'
$pr = foreach ($p in Get-CimInstance Win32_Printer | Where-Object { $_.Name -notmatch $skip }) {
  $x = $p.Name
  $con = $null
  if ($ports.ContainsKey($p.PortName) -and $ports[$p.PortName]) { $con = 'IP ' + $ports[$p.PortName] }
  elseif ($p.Network) { $con = 'Netzwerk ' + $p.ServerName }
  elseif ($p.PortName) { $con = $p.PortName }
  if ($con) { $x = $x + ' (' + $con.Trim() + ')' }
  if ($p.DriverName) { $x = $x + ' · Treiber ' + $p.DriverName }
  if ($p.Default) { $x = $x + ' · Standarddrucker' }
  $x
}
Add 'Drucker' ($pr -join $NL)

# --- Betriebssystem & Programme ---
Add 'Betriebssystem' $os.Caption
$av = Get-CimInstance -Namespace root/SecurityCenter2 -ClassName AntiVirusProduct | ForEach-Object { $_.displayName } | Sort-Object -Unique
Add 'Virenschutz' ($av -join ', ')
$c2r = Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Office\ClickToRun\Configuration'
if ($c2r.ProductReleaseIds) {
  $ids = $c2r.ProductReleaseIds
  $off = $ids
  if ($ids -match 'O365|M365') { $off = 'Microsoft 365' }
  elseif ($ids -match '(20\d\d)') { $off = 'Office ' + $Matches[1] }
  Add 'Office' ($off + ' (' + $ids + ', Version ' + $c2r.VersionToReport + ')')
}
$bp = (Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\Shell\Associations\UrlAssociations\https\UserChoice').ProgId
if (-not $bp) { $bp = (Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\Shell\Associations\UrlAssociations\http\UserChoice').ProgId }
Add 'Standard-Browser' (ProgName $bp)
$mp = (Get-ItemProperty 'HKCU:\Software\Microsoft\Windows\Shell\Associations\UrlAssociations\mailto\UserChoice').ProgId
if (-not $mp) { $mp = (Get-ItemProperty 'HKLM:\SOFTWARE\Clients\Mail').'(default)' }
Add 'E-Mail-Programm' (ProgName $mp)

# --- Software ---
$upaths = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*', 'HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*', 'HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*'
$skipSw = '^(Microsoft Visual C\+\+|Microsoft \.NET|Microsoft Windows Desktop Runtime|Microsoft ASP\.NET|Windows SDK|Windows Software Development Kit|Microsoft Update Health|Update for|Security Update|Hotfix|Microsoft Edge WebView2|Microsoft Edge Update|Office 16 Click-to-Run|Microsoft GameInput|Teams Machine-Wide|Windows PC Health Check|Windows-PC-Integritätsprüfung|Intel\(R\) (Chipset|Serial IO|Management Engine|Wireless Bluetooth)|Realtek (High Definition|Audio|Card Reader|Ethernet)|NVIDIA (PhysX|Graphics Driver|HD Audio|FrameView|USBC)|vs_|icecap_|Microsoft Policy Platform)'
$sw = Get-ItemProperty $upaths | Where-Object {
  $_.DisplayName -and $_.SystemComponent -ne 1 -and -not $_.ParentKeyName -and "$($_.ReleaseType)" -notmatch 'Update|Hotfix' -and $_.DisplayName -notmatch $skipSw
} | ForEach-Object { ($_.DisplayName -replace '\s+', ' ').Trim() } | Sort-Object -Unique
Add 'Software' (($sw | ForEach-Object { '- ' + $_ }) -join $NL)

# --- Netzwerk ---
$up = @(Get-NetAdapter -Physical | Where-Object { $_.Status -eq 'Up' })
if ($up.Count -eq 0) { $up = @(Get-NetAdapter -Physical) }
$types = foreach ($n in $up) {
  if ("$($n.MediaType)" -match '802\.11' -or "$($n.PhysicalMediaType)" -match '802\.11' -or $n.InterfaceDescription -match 'Wi-?Fi|Wireless|WLAN|802\.11') { 'WLAN' } else { 'LAN' }
}
Add 'Netzwerk-Typ' ((@($types) | Sort-Object -Unique) -join ' + ')
$ssid = $null; $wifiPw = $null
$wi = Netsh @('wlan', 'show', 'interfaces')
$l = $wi | Where-Object { $_ -match '^\s*SSID\s*:' } | Select-Object -First 1
if ($l) { $ssid = ($l -split ':', 2)[1].Trim() }
if (-not $ssid) {
  $ssid = (Get-NetConnectionProfile | Where-Object { $_.InterfaceAlias -match 'WLAN|Wi-?Fi|Wireless|Drahtlos' } | Select-Object -First 1).Name
}
if ($ssid) {
  Add 'WLAN-Name' $ssid
  $pr = Netsh @('wlan', 'show', 'profile', ('name=' + $ssid), 'key=clear')
  $kl = $pr | Where-Object { $_ -match '^\s*(Key Content|Schl.sselinhalt|Contenu de la cl.|Contenuto chiave)\s*:' } | Select-Object -First 1
  if ($kl) { $wifiPw = ($kl -split ':', 2)[1].Trim() }
}
$cfg = @(Get-CimInstance Win32_NetworkAdapterConfiguration -Filter 'IPEnabled=True')
$prim = $cfg | Where-Object { $_.DefaultIPGateway } | Select-Object -First 1
if ($prim) {
  $ip = @($prim.IPAddress | Where-Object { $_ -match '^\d+\.\d+\.\d+\.\d+$' })[0]
  $s = $ip + $(if ($prim.DHCPEnabled) {' (DHCP)'} else {' (fest)'})
  $gw = @($prim.DefaultIPGateway | Where-Object { $_ -match '\.' }) -join ', '
  if ($gw) { $s = $s + ' · Gateway ' + $gw }
  $dns = @($prim.DNSServerSearchOrder | Where-Object { $_ -match '\.' }) -join ', '
  if ($dns) { $s = $s + ' · DNS ' + $dns }
  Add 'IP-Konfiguration' $s
}
$nd = @(Get-PSDrive -PSProvider FileSystem | Where-Object { $_.DisplayRoot -like '\\*' } | ForEach-Object { $_.Name + ':\ (' + $_.DisplayRoot + ')' })
foreach ($k in Get-ChildItem 'HKCU:\Network') {
  $rp = (Get-ItemProperty $k.PSPath).RemotePath
  $e = $k.PSChildName.ToUpper() + ':\ (' + $rp + ')'
  if ($rp -and ($nd -notcontains $e)) { $nd += $e }
}
Add 'Netzlaufwerke' (($nd | Sort-Object -Unique) -join $NL)
$sh = Get-CimInstance Win32_Share | Where-Object { $_.Type -eq 0 -and $_.Name -notmatch '\$$' } | ForEach-Object { $_.Path + ' (\\' + $env:COMPUTERNAME + '\' + $_.Name + ')' }
Add 'SMB-Freigaben' ($sh -join $NL)

$name = $cs.Name; if (-not $name) { $name = $env:COMPUTERNAME }
$wifi = $null
if ($ssid) { $wifi = @{ ssid = $ssid; password = $wifiPw } }
@{ name = $name; fields = @($r); wifi = $wifi } | ConvertTo-Json -Depth 5 -Compress
`
