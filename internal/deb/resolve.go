package deb

import (
	"fmt"
	"sort"

	"pacdpkg/internal/alpm"
)

// InstallTarget is one package requested by the user.
type InstallTarget struct {
	Name    string
	Version string
	Repo    string
	DebPath string
	Meta    *alpm.Pkg
}

// PlannedPkg is one package the transaction will install or upgrade.
type PlannedPkg struct {
	Name       string
	OldVersion string
	NewVersion string
	Repo       string
	Sync       *SyncPkg
	Local      *alpm.Pkg
	DebPath    string
	URL        string
	Explicit   bool
	Reinstall  bool
	Held       bool
}

// RemovalInfo describes why a package is part of a transaction it was not
// explicitly asked to remove.
type RemovalInfo struct {
	Reason string
	Other  string
	Repo   string
}

// Plan is a resolved transaction.
type Plan struct {
	Install  []*PlannedPkg
	Remove   []*alpm.Pkg
	Removals map[string]RemovalInfo
	Session  uintptr
}

func (p *Plan) Empty() bool { return len(p.Install) == 0 && len(p.Remove) == 0 }

type request struct {
	name        string
	version     string
	repo        string
	debPath     string
	meta        *alpm.Pkg
	explicit    bool
	upgradeOnly bool
}

type resolver struct {
	sync      *Sync
	db        *DB
	pins      []Pin
	defRel    string
	plan      *Plan
	planned   map[string]*PlannedPkg
	problems  []string
	providers map[string][]*SyncPkg
	queue     []request
}

// ResolveInstall computes an install/upgrade transaction from the package
// lists and the local database, without asking apt.
func (s *Sync) ResolveInstall(db *DB, targets []InstallTarget, upgradeOnly bool) (*Plan, []string) {
	r := &resolver{
		sync:      s,
		db:        db,
		pins:      loadPins(),
		defRel:    defaultRelease(),
		plan:      &Plan{},
		planned:   map[string]*PlannedPkg{},
		providers: map[string][]*SyncPkg{},
	}
	r.seed(targets, upgradeOnly)
	r.run()
	r.resolveConflicts()
	r.sortPlan()
	return r.plan, dedupeStrings(r.problems)
}

func (r *resolver) seed(targets []InstallTarget, upgradeOnly bool) {
	for _, t := range targets {
		r.queue = append(r.queue, request{
			name: t.Name, version: t.Version, repo: t.Repo, debPath: t.DebPath,
			meta: t.Meta, explicit: true, upgradeOnly: upgradeOnly,
		})
	}
	if !upgradeOnly {
		return
	}
	for _, p := range r.db.Pkgs {
		cand := r.candidate(p.Name, "", "")
		if cand == nil || alpm.CompareVersion(cand.Version, p.Version) <= 0 {
			continue
		}
		r.queue = append(r.queue, request{name: p.Name, upgradeOnly: true})
	}
}

func (r *resolver) run() {
	for len(r.queue) > 0 {
		req := r.queue[0]
		r.queue = r.queue[1:]
		r.resolve(req)
	}
}

func (r *resolver) resolve(req request) {
	if existing, ok := r.planned[req.name]; ok {
		if req.explicit {
			existing.Explicit = true
		}
		return
	}
	if req.name == "" {
		return
	}

	local := r.db.Get(req.name)
	var chosen *SyncPkg
	if req.debPath != "" && req.meta != nil {
		chosen = syncPkgFromMeta(req.meta)
	} else if req.version != "" {
		chosen = r.sync.Find(req.name, req.version)
		if chosen == nil && req.debPath == "" {
			chosen = r.candidateIn(req.repo, req.name, "=", req.version)
		}
	} else {
		chosen = r.candidateIn(req.repo, req.name, "", "")
	}

	if chosen == nil && req.meta != nil {
		chosen = syncPkgFromMeta(req.meta)
	}
	if chosen == nil {
		if req.explicit {
			r.problems = append(r.problems, "target not found: "+req.name)
			return
		}
		// An installed package may already satisfy this dependency.
		if local != nil {
			return
		}
		r.problems = append(r.problems, "target not found: "+req.name)
		return
	}

	held := local != nil && r.db.Held(req.name)
	if held && !req.explicit {
		return
	}
	if local != nil && !req.explicit && !req.upgradeOnly &&
		alpm.CompareVersion(chosen.Version, local.Version) < 0 {
		r.problems = append(r.problems, fmt.Sprintf(
			"cannot downgrade %s (%s => %s) to satisfy a dependency",
			req.name, local.Version, chosen.Version))
		return
	}
	if local != nil && local.Version == chosen.Version {
		if !req.explicit || req.upgradeOnly {
			return
		}
		pp := &PlannedPkg{
			Name: req.name, OldVersion: local.Version, NewVersion: chosen.Version,
			Repo: chosen.Repo, Sync: chosen, Local: local, Held: held,
			DebPath: req.debPath, Explicit: true, Reinstall: true,
		}
		r.plan.Install = append(r.plan.Install, pp)
		r.planned[req.name] = pp
		r.enqueueDeps(chosen)
		return
	}
	if local != nil && req.upgradeOnly &&
		alpm.CompareVersion(chosen.Version, local.Version) <= 0 {
		return
	}

	pp := &PlannedPkg{
		Name: req.name, NewVersion: chosen.Version, Repo: chosen.Repo,
		Sync: chosen, Local: local, DebPath: req.debPath, Explicit: req.explicit,
		Held: held,
	}
	if local != nil {
		pp.OldVersion = local.Version
	}
	r.plan.Install = append(r.plan.Install, pp)
	r.planned[req.name] = pp
	r.enqueueDeps(chosen)
	if reqs, problems := r.breakRequests(chosen); len(problems) > 0 {
		r.problems = append(r.problems, problems...)
	} else {
		r.queue = append(r.queue, reqs...)
	}
}

func (r *resolver) enqueueDeps(p *SyncPkg) {
	for _, dep := range append(append([]alpm.Dep{}, p.Depends...), p.PreDepends...) {
		if r.db.DepSatisfied(dep, nil) {
			continue
		}
		if req, ok := r.dependencyRequest(dep); ok {
			r.queue = append(r.queue, req)
		}
	}
}

// dependencyRequest picks one alternative that can be installed.
func (r *resolver) dependencyRequest(dep alpm.Dep) (request, bool) {
	for _, alt := range dep.Alts {
		if cand := r.candidate(alt.Name, alt.Op, alt.Version); cand != nil {
			return request{name: alt.Name, version: cand.Version}, true
		}
	}
	for _, alt := range dep.Alts {
		for _, provider := range r.providersOf(alt.Name) {
			if alt.Op != "" && provider.Version != "" &&
				!satisfiesConstraint(provider.Version, alt.Op, alt.Version) {
				continue
			}
			if cand := r.candidate(provider.Name, "", ""); cand != nil {
				return request{name: provider.Name}, true
			}
		}
	}
	return request{}, false
}

func (r *resolver) providersOf(name string) []*SyncPkg {
	if providers, ok := r.providers[name]; ok {
		return providers
	}
	var out []*SyncPkg
	for _, p := range r.sync.Pkgs {
		for _, prov := range p.Provides {
			for _, alt := range prov.Alts {
				if alt.Name == name {
					out = append(out, p)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	r.providers[name] = out
	return out
}

// candidate returns the best candidate of name that satisfies a constraint.
func (r *resolver) candidate(name, op, version string) *SyncPkg {
	return r.candidateIn("", name, op, version)
}

func (r *resolver) candidateIn(repo, name, op, version string) *SyncPkg {
	var best *SyncPkg
	bestPrio := -1
	for _, cand := range r.sync.Lookup(name) {
		if repo != "" && cand.Repo != repo {
			continue
		}
		if !satisfiesConstraint(cand.Version, op, version) {
			continue
		}
		prio := candidatePriority(cand, r.pins, r.defRel)
		if prio <= 0 {
			continue
		}
		if best == nil || prio > bestPrio ||
			(prio == bestPrio && alpm.CompareVersion(cand.Version, best.Version) > 0) {
			best, bestPrio = cand, prio
		}
	}
	return best
}

func (r *resolver) resolveConflicts() {
	removed := map[string]bool{}
	for _, p := range r.plan.Install {
		if p.Sync == nil {
			continue
		}
		for _, rel := range p.Sync.Replaces {
			for _, alt := range rel.Alts {
				q := r.db.Get(alt.Name)
				if q == nil || r.planned[q.Name] != nil || removed[q.Name] {
					continue
				}
				if !satisfiesConstraint(q.Version, alt.Op, alt.Version) {
					continue
				}
				r.plan.Remove = append(r.plan.Remove, q)
				removed[q.Name] = true
			}
		}
	}
	for _, p := range r.plan.Install {
		if p.Sync == nil {
			continue
		}
		for _, c := range p.Sync.Conflicts {
			for _, alt := range c.Alts {
				if removed[alt.Name] {
					continue
				}
				if other, ok := r.planned[alt.Name]; ok && other != p &&
					satisfiesConstraint(other.NewVersion, alt.Op, alt.Version) {
					r.problems = append(r.problems, fmt.Sprintf(
						"%s-%s and %s-%s are in conflict (%s)",
						p.Name, p.NewVersion, other.Name, other.NewVersion, c.String()))
					continue
				}
				q := r.db.Get(alt.Name)
				if q != nil && r.planned[q.Name] == nil && !removed[q.Name] &&
					satisfiesConstraint(q.Version, alt.Op, alt.Version) {
					r.problems = append(r.problems, fmt.Sprintf(
						"%s-%s and %s-%s are in conflict (%s)",
						p.Name, p.NewVersion, q.Name, q.Version, c.String()))
				}
			}
		}
	}
}

// breakRequests turns a package's Breaks into upgrade requests: Debian
// expects the broken package to be upgraded past the broken range.
func (r *resolver) breakRequests(p *SyncPkg) ([]request, []string) {
	var reqs []request
	var problems []string
	for _, br := range p.Breaks {
		for _, alt := range br.Alts {
			q := r.db.Get(alt.Name)
			if q == nil {
				continue
			}
			current := q.Version
			if planned := r.planned[q.Name]; planned != nil {
				current = planned.NewVersion
			}
			if !satisfiesConstraint(current, alt.Op, alt.Version) {
				continue
			}
			op, version := invertedConstraint(alt.Op, alt.Version)
			cand := r.candidateIn("", q.Name, op, version)
			if cand == nil || alpm.CompareVersion(cand.Version, q.Version) <= 0 {
				problems = append(problems, fmt.Sprintf(
					"installing %s-%s breaks %s-%s", p.Name, p.Version, q.Name, q.Version))
				continue
			}
			reqs = append(reqs, request{name: q.Name, version: cand.Version, upgradeOnly: true})
		}
	}
	return reqs, problems
}

// invertedConstraint expresses "not matching this break" as a version
// constraint for the upgrade candidate.
func invertedConstraint(op, version string) (string, string) {
	switch op {
	case "<<":
		return ">=", version
	case "<=":
		return ">", version
	case ">>":
		return "<=", version
	case ">=":
		return "<", version
	case "=":
		return "!=", version
	}
	return "", ""
}

func (r *resolver) sortPlan() {
	sort.Slice(r.plan.Install, func(i, j int) bool {
		if r.plan.Install[i].Explicit != r.plan.Install[j].Explicit {
			return !r.plan.Install[i].Explicit
		}
		return r.plan.Install[i].Name < r.plan.Install[j].Name
	})
	sort.Slice(r.plan.Remove, func(i, j int) bool { return r.plan.Remove[i].Name < r.plan.Remove[j].Name })
}

func candidatePriority(cand *SyncPkg, pins []Pin, defRel string) int {
	prio := 500
	if cand.Release != nil {
		if cand.Release.NotAutomatic {
			prio = 1
			if cand.Release.ButAutomaticUpgrades {
				prio = 100
			}
		}
		if defRel != "" && (defRel == cand.Release.Suite || defRel == cand.Release.Codename) {
			prio = 990
		}
	}
	if pinned, ok := pinPriority(cand, pins); ok {
		prio = pinned
	}
	return prio
}

func satisfiesConstraint(version, op, want string) bool {
	if op == "" {
		return true
	}
	return versionSatisfies(version, op, want)
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// syncPkgFromMeta adapts a local .deb's metadata to the resolver's model.
func syncPkgFromMeta(p *alpm.Pkg) *SyncPkg {
	return &SyncPkg{
		Name:          p.Name,
		Version:       p.Version,
		Arch:          p.Arch,
		Description:   p.Description,
		Maintainer:    p.Packager,
		Homepage:      p.URL,
		Section:       firstOrEmpty(p.Groups),
		InstalledSize: p.InstalledSize,
		Depends:       p.Depends,
		Provides:      p.Provides,
		Conflicts:     p.Conflicts,
		Replaces:      p.Replaces,
	}
}

func firstOrEmpty(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}
