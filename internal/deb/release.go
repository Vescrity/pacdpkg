package deb

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ReleaseInfo is the subset of an apt Release file that affects candidate
// selection.
type ReleaseInfo struct {
	Suite                string
	Codename             string
	Origin               string
	Label                string
	NotAutomatic         bool
	ButAutomaticUpgrades bool
}

// loadReleases maps package-list filenames to their repository's metadata.
func loadReleases() map[string]*ReleaseInfo {
	out := map[string]*ReleaseInfo{}
	entries, err := os.ReadDir(listDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, "_InRelease") && !strings.HasSuffix(name, "_Release") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(listDir, name))
		if err != nil {
			continue
		}
		info := parseRelease(string(data))
		key := strings.TrimSuffix(strings.TrimSuffix(name, "_InRelease"), "_Release") + "_Packages"
		out[key] = info
	}
	return out
}

func parseRelease(text string) *ReleaseInfo {
	info := &ReleaseInfo{}
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "Suite":
			info.Suite = value
		case "Codename":
			info.Codename = value
		case "Origin":
			info.Origin = value
		case "Label":
			info.Label = value
		case "NotAutomatic":
			info.NotAutomatic = value == "yes"
		case "ButAutomaticUpgrades":
			info.ButAutomaticUpgrades = value == "yes"
		}
	}
	return info
}

// Pin is one /etc/apt/preferences stanza.
type Pin struct {
	Package  string
	Pin      string
	Priority int
}

// loadPins reads apt's preferences files.
func loadPins() []Pin {
	var pins []Pin
	for _, path := range append([]string{"/etc/apt/preferences"}, prefFiles()...) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		pins = append(pins, parsePins(string(data))...)
	}
	return pins
}

func prefFiles() []string {
	entries, err := os.ReadDir("/etc/apt/preferences.d")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			out = append(out, "/etc/apt/preferences.d/"+e.Name())
		}
	}
	return out
}

func parsePins(text string) []Pin {
	var pins []Pin
	var cur Pin
	flush := func() {
		if cur.Package != "" {
			pins = append(pins, cur)
		}
		cur = Pin{}
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			flush()
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Package":
			cur.Package = strings.TrimSpace(value)
		case "Pin":
			cur.Pin = strings.TrimSpace(value)
		case "Pin-Priority":
			if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
				cur.Priority = n
			}
		}
	}
	flush()
	return pins
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}
