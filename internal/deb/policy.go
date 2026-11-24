package deb

import (
	"os"
	"strings"

	"pacdpkg/internal/alpm"
)

// Policy is apt's view of a package's candidate version.
type Policy struct {
	Name      string
	Installed string
	Candidate string
	FromRepo  bool
	Repo      string
}

// Policies computes the candidate version of each package from the package
// lists, release metadata and pin priorities.
func (s *Sync) Policies(db *DB, names []string) map[string]*Policy {
	pins := loadPins()
	defaultRelease := defaultRelease()
	out := make(map[string]*Policy, len(names))
	for _, name := range names {
		out[name] = s.policyFor(name, db, pins, defaultRelease)
	}
	return out
}

func (s *Sync) policyFor(name string, db *DB, pins []Pin, defaultRelease string) *Policy {
	p := &Policy{Name: name}
	if local := db.Get(name); local != nil {
		p.Installed = local.Version
	}
	var best *SyncPkg
	bestPrio := -1
	for _, cand := range s.Lookup(name) {
		prio := 500
		if cand.Release != nil {
			if cand.Release.NotAutomatic {
				prio = 1
				if cand.Release.ButAutomaticUpgrades {
					prio = 100
				}
			}
			if defaultRelease != "" &&
				(defaultRelease == cand.Release.Suite || defaultRelease == cand.Release.Codename) {
				prio = 990
			}
		}
		if pinned, ok := pinPriority(cand, pins); ok {
			prio = pinned
		}
		if prio <= 0 {
			continue
		}
		p.FromRepo = true
		if best == nil || prio > bestPrio ||
			(prio == bestPrio && alpm.CompareVersion(cand.Version, best.Version) > 0) {
			best, bestPrio = cand, prio
		}
	}
	if best != nil {
		p.Candidate = best.Version
		p.Repo = best.Repo
		return p
	}
	p.Candidate = p.Installed
	return p
}

// defaultRelease reads APT::Default-Release from the apt configuration.
func defaultRelease() string {
	var files []string
	files = append(files, "/etc/apt/apt.conf")
	entries, err := os.ReadDir("/etc/apt/apt.conf.d")
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				files = append(files, "/etc/apt/apt.conf.d/"+e.Name())
			}
		}
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "APT::Default-Release") {
				continue
			}
			_, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), ";"))
			return strings.Trim(value, "\"")
		}
	}
	return ""
}
