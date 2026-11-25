package app

import (
	"fmt"
	"os"
	"strings"

	"pacdpkg/internal/aptplan"
	"pacdpkg/internal/deb"
)

// database implements -D: set install reasons or validate the database.
func (a *App) database() int {
	cfg := a.cfg
	if cfg.Check > 0 && !cfg.AsDeps && !cfg.AsExplicit {
		if cfg.Check >= 2 {
			return a.checkSyncDB()
		}
		return a.checkLocalDB()
	}
	if len(a.targets) == 0 {
		if cfg.AsDeps || cfg.AsExplicit {
			Errorf("no targets specified (use -h for help)")
			return 1
		}
		return 0
	}
	if !cfg.AsDeps && !cfg.AsExplicit {
		Errorf("no install reason specified (use -h for help)")
		return 1
	}

	db, err := deb.Open()
	if err != nil {
		Errorf("could not read the dpkg database: %v", err)
		return 1
	}
	ret := 0
	for _, t := range a.targets {
		if db.Get(t) == nil {
			Errorf("package '%s' was not found", t)
			ret = 1
			continue
		}
		mark := "manual"
		if cfg.AsDeps {
			mark = "auto"
		}
		if err := deb.Run("apt-mark", mark, t); err != nil {
			Errorf("could not set install reason for package %s (%v)", t, err)
			ret = 1
			continue
		}
		if !cfg.Quiet {
			if cfg.AsDeps {
				fmt.Fprintf(os.Stdout, "%s: install reason has been set to 'installed as dependency'\n", t)
			} else {
				fmt.Fprintf(os.Stdout, "%s: install reason has been set to 'explicitly installed'\n", t)
			}
		}
	}
	return ret
}

// checkLocalDB implements -Dk: report packages whose dpkg state is not
// cleanly installed.
func (a *App) checkLocalDB() int {
	problems, err := deb.AuditProblems()
	if err != nil {
		Errorf("%v", err)
		return 1
	}
	if len(problems) == 0 {
		if !a.cfg.Quiet {
			fmt.Fprintln(os.Stdout, "No database errors have been found!")
		}
		return 0
	}
	for _, line := range problems {
		Errorf("%s", line)
	}
	return 1
}

// checkSyncDB implements -Dkk: every installed package's dependencies must
// be satisfied by another installed package.
func (a *App) checkSyncDB() int {
	if aptplan.Available() {
		if problems, err := aptplan.CheckDB(); err == nil {
			for _, problem := range problems {
				Errorf("%s", problem)
			}
			if len(problems) == 0 {
				if !a.cfg.Quiet {
					fmt.Fprintln(os.Stdout, "No database errors have been found!")
				}
				return 0
			}
			return 1
		}
	}
	db, err := deb.Open()
	if err != nil {
		Errorf("could not read the dpkg database: %v", err)
		return 1
	}
	broken := 0
	for _, p := range db.Pkgs {
		for _, dep := range p.Depends {
			if !db.DepSatisfied(dep, nil) {
				Errorf("missing '%s' dependency for '%s'", dep.String(), p.Name)
				broken++
			}
		}
	}
	if broken == 0 {
		if !a.cfg.Quiet {
			fmt.Fprintln(os.Stdout, "No database errors have been found!")
		}
		return 0
	}
	return 1
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
