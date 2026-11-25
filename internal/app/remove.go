package app

import (
	"fmt"
	"os"
	"strings"

	"pacdpkg/internal/aptplan"
	"pacdpkg/internal/deb"
	"pacdpkg/internal/render"
)

// remove implements -R on top of apt-get remove/purge.
func (a *App) remove() int {
	if len(a.targets) == 0 {
		Errorf("no targets specified (use -h for help)")
		return 1
	}

	db, err := deb.Open()
	if err != nil {
		Errorf("could not read the dpkg database: %v", err)
		return 1
	}

	var explicit []string
	seen := map[string]bool{}
	missing := false
	held := false
	for _, t := range a.targets {
		name := strings.TrimPrefix(t, "local/")
		if db.Get(name) == nil {
			Errorf("target not found: %s", name)
			missing = true
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		if db.Held(name) {
			held = true
			if !a.cfg.Print {
				Warnf("%s is designated as a HoldPkg.", name)
			}
		}
		explicit = append(explicit, name)
	}
	if missing {
		return 1
	}

	targets := explicit
	if a.cfg.Unneeded {
		targets = keepUnneeded(targets, func(name string) []string {
			if p := db.Get(name); p != nil {
				return p.RequiredBy
			}
			return nil
		})
	}

	if !a.cfg.Print && a.cfg.Nodeps == 0 {
		fmt.Fprintln(os.Stdout, "checking dependencies...")
	}

	if !a.cfg.Print && (a.warnHoldPkg(targets) || held) &&
		!render.Question("HoldPkg was found in target list. Do you want to continue?", false, a.cfg.NoConfirm) {
		return 1
	}

	if len(targets) == 0 {
		fmt.Fprintln(os.Stdout, " there is nothing to do")
		return 0
	}

	if aptplan.Available() && a.cfg.Nodeps == 0 {
		var ats []aptplan.Target
		for _, name := range targets {
			ats = append(ats, aptplan.Target{Name: name})
		}
		res, err := aptplan.Resolve(aptplan.OpRemove, ats, aptplan.Options{
			Purge:         a.cfg.NoSave,
			Recursive:     a.cfg.Recursive,
			Cascade:       a.cfg.Cascade,
			OverrideHeld:  held,
			Lock:          !a.cfg.Print,
			Arch:          a.cfg.Arch,
			Cachedir:      a.cfg.CacheDir,
			Architectures: a.cfg.ArchList,
		})
		if err != nil {
			Errorf("%v", err)
			return 1
		}
		if len(res.Problems) > 0 {
			a.reportPlanProblems(nil, res.Problems)
			return 1
		}
		return a.commitPlan(res.Plan, txOpts{
			question: "Do you want to remove these packages?",
		})
	}

	if a.cfg.Cascade {
		targets = db.DependentClosure(targets)
	}
	if a.cfg.Recursive > 0 {
		if !db.ReasonsKnown() {
			Errorf("cannot determine install reasons; refusing to remove dependencies")
			return 1
		}
		targets = db.RemovalClosure(targets, a.cfg.Recursive > 1)
	}

	if a.cfg.Nodeps == 0 && !a.cfg.Cascade {
		if broken := brokenDeps(db, targets); len(broken) > 0 {
			Errorf("failed to prepare transaction (could not satisfy dependencies)")
			for _, line := range broken {
				fmt.Fprintf(os.Stdout, ":: %s\n", line)
			}
			return 1
		}
	}

	plan := &deb.Plan{}
	for _, name := range targets {
		if p := db.Get(name); p != nil {
			plan.Remove = append(plan.Remove, p)
		}
	}
	return a.commitPlan(plan, txOpts{
		question:  "Do you want to remove these packages?",
		purge:     a.cfg.NoSave,
		forceDeps: a.cfg.Nodeps > 0,
	})

}

func (a *App) warnHoldPkg(targets []string) bool {
	found := false
	for _, t := range targets {
		for _, hold := range a.conf.HoldPkg {
			if hold == t {
				Warnf("%s is designated as a HoldPkg.", t)
				found = true
				break
			}
		}
	}
	return found
}

// keepUnneeded drops targets that are still required by a package outside
// the removal set (pacman -Ru). Dropping one target can make another one
// needed, so the targets are re-evaluated until stable.
func keepUnneeded(targets []string, requiredBy func(string) []string) []string {
	for {
		inSet := make(map[string]bool, len(targets))
		for _, t := range targets {
			inSet[t] = true
		}
		var kept []string
		for _, t := range targets {
			needed := false
			for _, req := range requiredBy(t) {
				if !inSet[req] {
					needed = true
					break
				}
			}
			if !needed {
				kept = append(kept, t)
			}
		}
		if len(kept) == len(targets) {
			return kept
		}
		targets = kept
	}
}

// brokenDeps reports removals that would break dependencies of packages
// that stay installed; pacman refuses such transactions.
func brokenDeps(db *deb.DB, targets []string) []string {
	remove := make(map[string]bool, len(targets))
	for _, t := range targets {
		remove[t] = true
	}
	var out []string
	for _, p := range db.Pkgs {
		if remove[p.Name] {
			continue
		}
		for _, dep := range p.Depends {
			if db.DepSatisfied(dep, remove) {
				continue
			}
			culprit := ""
			for _, name := range db.DepTargets(dep) {
				if remove[name] {
					culprit = name
					break
				}
			}
			if culprit == "" {
				continue
			}
			out = append(out, fmt.Sprintf("removing %s breaks dependency '%s' required by %s",
				culprit, dep.String(), p.Name))
		}
	}
	return out
}
