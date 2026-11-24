package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pacdpkg/internal/alpm"
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
		if a.cfg.Needed && local != nil && alpm.CompareVersion(local.Version, version) == 0 {
			if !a.cfg.Print {
				Warnf("%s-%s is up to date -- skipping", name, version)
			}
			continue
		}
		if !a.cfg.Print && local != nil && alpm.CompareVersion(version, local.Version) < 0 {
			Warnf("downgrading package %s (%s => %s)", name, local.Version, version)
		}
		seen[name] = true
		targets = append(targets, deb.InstallTarget{
			Name: name, Version: version, DebPath: path, Meta: pf.Pkg,
		})
		paths[name] = path
	}

	if !a.cfg.Print {
		fmt.Fprintln(os.Stdout, "loading packages...")
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

	sync := a.loadSync()
	if sync == nil {
		return 1
	}
	if !a.cfg.Print {
		fmt.Fprintln(os.Stdout, "resolving dependencies...")
		if !a.cfg.DownloadOnly {
			fmt.Fprintln(os.Stdout, "looking for conflicting packages...")
		}
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
