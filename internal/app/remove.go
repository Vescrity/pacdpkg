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
		aptplan.Configure(a.effectiveArch(), a.cfg.CacheDir, a.effectiveArchitectures())
		unneeded, err := aptplan.Unneeded(targets)
		if err != nil {
			Errorf("%v", err)
			return 1
		}
		targets = unneeded
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

	if a.cfg.Nodeps == 0 {
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
			Arch:          a.effectiveArch(),
			Cachedir:      a.cfg.CacheDir,
			Architectures: a.effectiveArchitectures(),
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
