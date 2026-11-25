package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pacdpkg/internal/alpm"
	"pacdpkg/internal/aptplan"
	"pacdpkg/internal/deb"
)

// upgrade implements -U: install or upgrade packages from local package files.
func (a *App) upgrade() int {
	if len(a.targets) == 0 {
		Errorf("no targets specified (use -h for help)")
		return 1
	}

	db, err := deb.Open()
	if err != nil {
		Errorf("could not read the dpkg database: %v", err)
		return 1
	}

	if !a.cfg.Print {
		fmt.Fprintln(os.Stdout, "loading packages...")
	}

	var targets []deb.InstallTarget
	paths := make(map[string]string, len(a.targets))
	seen := make(map[string]bool, len(a.targets))
	ret := 0
	for _, path := range a.targets {
		pf, err := deb.OpenPackageFile(path)
		if err != nil {
			Errorf("'%s': %v", path, err)
			ret = 1
			continue
		}
		name, version := pf.Pkg.Name, pf.Pkg.Version
		if seen[name] {
			Errorf("'%s': duplicate target", path)
			ret = 1
			continue
		}
		local := db.Get(name)
		if local != nil {
			switch alpm.CompareVersion(local.Version, version) {
			case 0:
				if a.cfg.Needed {
					if !a.cfg.Print {
						Warnf("%s-%s is up to date -- skipping", local.Name, local.Version)
					}
					continue
				}
				if !a.cfg.Print && !a.cfg.DownloadOnly {
					Warnf("%s-%s is up to date -- reinstalling", local.Name, local.Version)
				}
			case 1:
				if !a.cfg.Print && !a.cfg.DownloadOnly {
					Warnf("downgrading package %s (%s => %s)", local.Name, local.Version, version)
				}
			}
		}
		seen[name] = true
		targets = append(targets, deb.InstallTarget{
			Name: name, Version: version, DebPath: path, Meta: pf.Pkg,
		})
		paths[name] = path
	}

	if ret != 0 {
		return 1
	}
	if len(targets) == 0 {
		if !a.cfg.Print {
			fmt.Fprintln(os.Stdout, " there is nothing to do")
		}
		return 0
	}

	if !a.cfg.Print {
		fmt.Fprintln(os.Stdout, "resolving dependencies...")
		if !a.cfg.DownloadOnly {
			fmt.Fprintln(os.Stdout, "looking for conflicting packages...")
		}
	}
	if aptplan.Available() && a.cfg.Nodeps == 0 {
		var ats []aptplan.Target
		for _, t := range targets {
			ats = append(ats, aptplan.Target{Name: t.Name, Version: t.Version, Deb: t.DebPath})
		}
		res, err := aptplan.Resolve(aptplan.OpInstall, ats, aptplan.Options{
			Needed: a.cfg.Needed,
			Lock:   !a.cfg.Print && !a.cfg.DownloadOnly,
		})
		if err != nil {
			Errorf("%v", err)
			return 1
		}
		if len(res.Problems) > 0 {
			a.reportPlanProblems(nil, res.Problems)
			return 1
		}
		if !a.confirmRemovals(res.Plan) {
			aptplan.Close(res.Session)
			return 1
		}
		return a.commitPlan(res.Plan, txOpts{paths: paths})
	}
	sync := a.loadSync()
	if sync == nil {
		return 1
	}
	plan, problems := sync.ResolveInstall(db, targets, false)
	if a.reportPlanProblems(plan, problems) {
		return 1
	}
	for _, p := range plan.Install {
		if p.DebPath == "" {
			if path, ok := paths[p.Name]; ok {
				p.DebPath = path
			}
		}
	}
	return a.commitPlan(plan, txOpts{
		paths:     paths,
		forceDeps: a.cfg.Nodeps > 0,
	})
}

// aptTarget makes a bare file name recognizable to apt-get, which otherwise
// treats arguments without a slash as package names.
func aptTarget(path string) string {
	if filepath.IsAbs(path) || strings.ContainsRune(path, '/') {
		return path
	}
	return "./" + path
}
