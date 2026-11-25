package deb

import "pacdpkg/internal/alpm"

// versionSatisfies checks a pacman-style version constraint.
func versionSatisfies(version, op, want string) bool {
	if op == "" {
		return true
	}
	c := alpm.CompareVersion(version, want)
	switch op {
	case "=":
		return c == 0
	case "!=":
		return c != 0
	case ">=":
		return c >= 0
	case ">>":
		return c > 0
	case "<=":
		return c <= 0
	case "<<":
		return c < 0
	case ">":
		return c > 0
	case "<":
		return c < 0
	}
	return false
}

// providesVersion returns the version a package declares for a virtual
// package, and whether the Provides entry is versioned at all.
func (db *DB) providesVersion(p *alpm.Pkg, name string) (string, bool) {
	for _, prov := range p.Provides {
		for _, alt := range prov.Alts {
			if alt.Name == name && alt.Version != "" {
				return alt.Version, true
			}
		}
	}
	return p.Version, false
}

// DepSatisfied reports whether a dependency is met by an installed package
// that is not in exclude; each alternative keeps its own constraint.
func (db *DB) DepSatisfied(dep alpm.Dep, exclude map[string]bool) bool {
	for _, alt := range dep.Alts {
		if p := db.byName[alt.Name]; p != nil && !exclude[p.Name] &&
			versionSatisfies(p.Version, alt.Op, alt.Version) {
			return true
		}
		for _, p := range db.provided[alt.Name] {
			if exclude[p.Name] {
				continue
			}
			pv, versioned := db.providesVersion(p, alt.Name)
			if alt.Op != "" && !versioned {
				continue
			}
			if versionSatisfies(pv, alt.Op, alt.Version) {
				return true
			}
		}
	}
	return false
}
