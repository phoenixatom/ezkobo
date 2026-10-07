package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Kobo device IDs: the last three digits of the last field of .kobo/version.
var koboModels = map[string]string{
	"310": "Touch", "320": "Touch", "330": "Glo", "340": "Mini", "350": "Aura HD",
	"360": "Aura", "370": "Aura H2O", "371": "Glo HD", "372": "Touch 2.0",
	"373": "Aura ONE", "374": "Aura H2O", "375": "Aura", "376": "Clara HD",
	"377": "Forma", "378": "Aura H2O", "379": "Aura", "380": "Forma", "381": "Aura ONE",
	"382": "Nia", "383": "Sage", "384": "Libra H2O", "386": "Clara 2E", "387": "Elipsa",
	"388": "Libra 2", "389": "Elipsa 2E", "390": "Libra Colour", "391": "Clara BW",
	"393": "Clara Colour", "395": "Clara BW",
}

type deviceInfo struct {
	Model    string `json:"model"`
	Serial   string `json:"serial"` // last 4 characters only
	Firmware string `json:"firmware"`
}

// readDevice parses /mnt/onboard/.kobo/version, which looks like
// "N4180XXXXXXXX,4.9.77,4.38.21908,4.9.77,4.9.77,00000000-0000-0000-0000-000000000390".
func readDevice() deviceInfo {
	d := deviceInfo{Model: "Kobo"}
	b, err := os.ReadFile("/mnt/onboard/.kobo/version")
	if err != nil {
		return d
	}
	f := strings.Split(strings.TrimSpace(string(b)), ",")
	if s := strings.TrimSpace(f[0]); len(s) >= 4 {
		d.Serial = strings.ToUpper(s[len(s)-4:])
	}
	if len(f) > 2 {
		d.Firmware = f[2]
	}
	if id := f[len(f)-1]; len(id) >= 3 {
		if m, ok := koboModels[id[len(id)-3:]]; ok {
			d.Model = m
		}
	}
	return d
}

// deviceName is the advertised name, e.g. "Libra Colour 1A2B". The serial
// suffix keeps two Kobos of the same model apart.
func deviceName() string {
	d := readDevice()
	if d.Serial != "" {
		return d.Model + " " + d.Serial
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return strings.ReplaceAll(h, ".", "-")
	}
	return "Kobo"
}

// defaultHost is the mDNS host name, unique per device.
func defaultHost() string {
	if s := readDevice().Serial; s != "" {
		return "kobo-" + strings.ToLower(s)
	}
	return "ezkobo"
}

type battery struct {
	Level    int  `json:"level"`
	Charging bool `json:"charging"`
}

func readBattery() *battery {
	dirs, _ := filepath.Glob("/sys/class/power_supply/*")
	for _, d := range dirs {
		if t, _ := os.ReadFile(filepath.Join(d, "type")); strings.TrimSpace(string(t)) != "Battery" {
			continue
		}
		c, err := os.ReadFile(filepath.Join(d, "capacity"))
		if err != nil {
			continue
		}
		level, err := strconv.Atoi(strings.TrimSpace(string(c)))
		if err != nil {
			continue
		}
		st, _ := os.ReadFile(filepath.Join(d, "status"))
		s := strings.TrimSpace(string(st))
		return &battery{level, s == "Charging" || s == "Full"}
	}
	return nil
}

func diskSpace(dir string) (free, total uint64) {
	var st syscall.Statfs_t
	if syscall.Statfs(dir, &st) != nil {
		return 0, 0
	}
	return uint64(st.Bavail) * uint64(st.Bsize), uint64(st.Blocks) * uint64(st.Bsize)
}
