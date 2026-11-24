package deb

import (
	"sort"

	"pacdpkg/internal/alpm"
)

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
	case ">=", ">>":
		return c >= 0
	case "<=", "<<":
		return c <= 0
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

// Satisfies reports whether an installed package (or one of its Provides)
// meets the constraint.
func (db *DB) Satisfies(name, op, version string) bool {
	if p := db.byName[name]; p != nil && versionSatisfies(p.Version, op, version) {
		return true
	}
	for _, p := range db.provided[name] {
		pv, versioned := db.providesVersion(p, name)
		if op != "" && !versioned {
			continue
		}
		if versionSatisfies(pv, op, version) {
			return true
		}
	}
	return false
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

// RemovalClosure extends a target list with the packages that were pulled in
// as its dependencies and become unneeded (pacman -Rs). With includeExplicit
// it also takes packages that were installed explicitly (pacman -Rss).
func (db *DB) RemovalClosure(targets []string, includeExplicit bool) []string {
	inSet := map[string]bool{}
	for _, t := range targets {
		inSet[t] = true
	}

	candidates := map[string]bool{}
	frontier := append([]string{}, targets...)
	for len(frontier) > 0 {
		name := frontier[0]
		frontier = frontier[1:]
		p := db.Get(name)
		if p == nil {
			continue
		}
		for _, dep := range p.Depends {
			for _, depName := range db.targets(dep) {
				q := db.Get(depName)
				if q == nil || inSet[q.Name] || candidates[q.Name] {
					continue
				}
				if q.Explicit && !includeExplicit {
					continue
				}
				candidates[q.Name] = true
				frontier = append(frontier, q.Name)
			}
		}
	}

	for changed := true; changed; {
		changed = false
		for name := range candidates {
			if inSet[name] {
				continue
			}
			p := db.Get(name)
			if p == nil {
				continue
			}
			needed := false
			for _, req := range p.RequiredBy {
				if !inSet[req] {
					needed = true
					break
				}
			}
			if !needed {
				inSet[name] = true
				changed = true
			}
		}
	}

	out := make([]string, 0, len(inSet))
	for name := range inSet {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// DependentClosure extends a target list with every package that depends on
// it, recursively (pacman -Rc).
func (db *DB) DependentClosure(targets []string) []string {
	inSet := map[string]bool{}
	for _, t := range targets {
		inSet[t] = true
	}
	for changed := true; changed; {
		changed = false
		for _, p := range db.Pkgs {
			if inSet[p.Name] {
				continue
			}
			for _, dep := range p.Depends {
				hit := false
				for _, name := range db.targets(dep) {
					if inSet[name] {
						hit = true
						break
					}
				}
				if hit {
					inSet[p.Name] = true
					changed = true
					break
				}
			}
		}
	}
	out := make([]string, 0, len(inSet))
	for name := range inSet {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// DepTargets returns the installed package names that can satisfy a
// dependency.
func (db *DB) DepTargets(dep alpm.Dep) []string {
	var out []string
	for _, alt := range dep.Alts {
		if p := db.byName[alt.Name]; p != nil {
			out = append(out, p.Name)
		}
		for _, p := range db.provided[alt.Name] {
			out = append(out, p.Name)
		}
	}
	return out
}
