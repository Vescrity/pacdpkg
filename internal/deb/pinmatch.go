package deb

import (
	"net/url"
	"path/filepath"
	"strings"
)

// pinPriority applies apt's pin rules: the first specific (Package != "*")
// matching pin wins; otherwise the highest priority among general pins.
func pinPriority(cand *SyncPkg, pins []Pin) (int, bool) {
	var specific *Pin
	general := -1
	for i := range pins {
		pin := &pins[i]
		if !pin.matches(cand) {
			continue
		}
		if pin.Package != "*" {
			if specific == nil {
				specific = pin
			}
			continue
		}
		if pin.Priority > general {
			general = pin.Priority
		}
	}
	if specific != nil {
		return specific.Priority, true
	}
	if general >= 0 {
		return general, true
	}
	return 0, false
}

func (p Pin) matches(cand *SyncPkg) bool {
	if p.Package != "*" {
		if ok, err := filepath.Match(p.Package, cand.Name); err != nil || !ok {
			return false
		}
	}
	pin := strings.TrimSpace(p.Pin)
	if pin == "" {
		return true
	}
	switch {
	case strings.HasPrefix(pin, "version"):
		pattern := strings.TrimSpace(strings.TrimPrefix(pin, "version"))
		ok, err := filepath.Match(pattern, cand.Version)
		return err == nil && ok
	case strings.HasPrefix(pin, "origin"):
		pattern := strings.Trim(strings.TrimSpace(strings.TrimPrefix(pin, "origin")), "\"")
		return strings.Contains(hostOfSync(cand), pattern)
	case strings.HasPrefix(pin, "release"):
		return releaseMatches(strings.TrimSpace(strings.TrimPrefix(pin, "release")), cand)
	}
	return false
}

func releaseMatches(expr string, cand *SyncPkg) bool {
	fields := map[string]string{}
	for _, part := range strings.Split(expr, ",") {
		part = strings.TrimSpace(part)
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if len(fields) == 0 {
		return true
	}
	info := cand.Release
	for key, want := range fields {
		var have string
		switch key {
		case "a":
			if info != nil {
				have = info.Suite
			}
		case "n":
			if info != nil {
				have = info.Codename
			}
		case "o":
			if info != nil {
				have = info.Origin
			}
		case "l":
			if info != nil {
				have = info.Label
			}
		case "c":
			have = componentOf(cand)
		case "v":
			if info != nil {
				have = info.Suite
			}
		}
		ok, err := filepath.Match(want, have)
		if err != nil || !ok {
			return false
		}
	}
	return true
}

func componentOf(cand *SyncPkg) string {
	if cand.Source != nil {
		return cand.Source.Component
	}
	if i := strings.LastIndexByte(cand.Repo, '/'); i >= 0 {
		return cand.Repo[i+1:]
	}
	return cand.Repo
}

func hostOfSync(cand *SyncPkg) string {
	uri := ""
	if cand.Source != nil {
		uri = cand.Source.URI
	}
	if uri == "" {
		return ""
	}
	if u, err := url.Parse(uri); err == nil {
		return u.Host
	}
	return hostOf(uri)
}
