package app

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"pacdpkg/internal/alpm"
	"pacdpkg/internal/deb"
	"pacdpkg/internal/render"
)

// Seams so tests can supply package data.
var (
	loadFilesSync = deb.LoadSync
	openLocalDB   = deb.Open
)

func (a *App) files() int {
	cfg := a.cfg
	if cfg.Refresh > 0 {
		if ret := a.filesRefresh(); ret != 0 {
			return ret
		}
	}

	db, err := deb.OpenContents(deb.DefaultContentsDir())
	if err != nil || len(db.Repos) == 0 {
		Errorf("you do not have a files database. Try running 'pacman -Fy'")
		return 1
	}

	if cfg.List {
		return a.filesList(db)
	}
	if len(a.targets) == 0 {
		if cfg.Refresh > 0 {
			return 0
		}
		Errorf("no targets specified (use -h for help)")
		return 1
	}
	return a.filesSearch(db)
}

func (a *App) filesRefresh() int {
	fmt.Fprintln(os.Stdout, ":: Synchronizing package databases...")
	targets, err := deb.ContentsTargets(deb.ContentsArch(a.cfg.Arch))
	if err != nil {
		Errorf("failed to synchronize all databases: %v", err)
		return 1
	}
	if len(targets) == 0 {
		Errorf("no usable package repositories configured.")
		return 1
	}
	dir := deb.ContentsWriteDir()
	if dir == "" {
		Errorf("no writable cache directory for the files database")
		return 1
	}
	report, err := deb.RefreshContents(dir, targets, a.cfg.Refresh > 1)
	if report != nil {
		for _, w := range report.Warnings {
			Warnf("%s", w)
		}
	}
	if err != nil {
		Errorf("failed to synchronize all databases: %v", err)
		return 1
	}
	if report.Updated == 0 && report.Unchanged == 0 {
		Errorf("failed to synchronize all databases (failed to retrieve some files)")
		return 1
	}
	return 0
}

// fileVersions supplies the package versions that Debian's Contents index
// lacks. They come from the apt package lists when those are available.
type fileVersions struct {
	byRepo map[string]string
	byName map[string]string
}

func newFileVersions() *fileVersions {
	v := &fileVersions{byRepo: map[string]string{}, byName: map[string]string{}}
	sync, err := loadFilesSync()
	if err != nil {
		return v
	}
	for _, p := range sync.Pkgs {
		key := p.Repo + "/" + p.Name
		if cur, ok := v.byRepo[key]; !ok || alpm.CompareVersion(p.Version, cur) > 0 {
			v.byRepo[key] = p.Version
		}
		if cur, ok := v.byName[p.Name]; !ok || alpm.CompareVersion(p.Version, cur) > 0 {
			v.byName[p.Name] = p.Version
		}
	}
	return v
}

func (v *fileVersions) get(repo, name string) string {
	if ver, ok := v.byRepo[repo+"/"+name]; ok {
		return ver
	}
	return v.byName[name]
}

type fileTarget struct {
	path  string
	exact bool
	re    *regexp.Regexp
}

func (t *fileTarget) matches(path string) bool {
	if t.exact {
		if t.re != nil {
			return t.re.MatchString(path)
		}
		return path == t.path
	}
	slash := strings.LastIndexByte(path, '/')
	if slash < 0 || slash == len(path)-1 {
		return false
	}
	base := path[slash+1:]
	if t.re != nil {
		return t.re.MatchString(base)
	}
	return base == t.path
}

func (a *App) filesSearch(db *deb.ContentsDB) int {
	targets := make([]*fileTarget, 0, len(a.targets))
	failed := false
	for _, raw := range a.targets {
		path := raw
		if strings.Contains(raw, "/") {
			for len(path) > 1 && path[0] == '/' {
				path = path[1:]
			}
		}
		t := &fileTarget{path: path, exact: strings.Contains(raw, "/")}
		if a.cfg.FileRegex {
			re, err := regexp.Compile("(?i)" + path)
			if err != nil {
				Errorf("invalid regular expression '%s'", path)
				failed = true
				continue
			}
			t.re = re
		}
		targets = append(targets, t)
	}
	if failed {
		return 1
	}

	var versions *fileVersions
	if !a.cfg.Quiet || a.cfg.MachineReadable {
		versions = newFileVersions()
	}
	var local *deb.DB
	if !a.cfg.Quiet && !a.cfg.MachineReadable {
		local, _ = openLocalDB()
	}

	ret := 0
	for _, t := range targets {
		found := false
		for _, repo := range db.Repos {
			var order []string
			groups := map[string][]string{}
			err := repo.Scan(func(path string, pkgs []string) bool {
				if !t.matches(path) {
					return true
				}
				for _, pkg := range pkgs {
					if _, ok := groups[pkg]; !ok {
						order = append(order, pkg)
					}
					if !containsString(groups[pkg], path) {
						groups[pkg] = append(groups[pkg], path)
					}
				}
				return true
			})
			if err != nil {
				Warnf("could not read the files database '%s': %v", repo.Label, err)
			}
			for _, pkg := range order {
				found = true
				a.printFileMatch(repo.Label, pkg, groups[pkg], t.exact, versions, local)
			}
		}
		if !found {
			ret = 1
		}
	}
	return ret
}

func (a *App) printFileMatch(repo, pkg string, paths []string, exact bool, versions *fileVersions, local *deb.DB) {
	ver := ""
	if versions != nil {
		ver = versions.get(repo, pkg)
	}
	if a.cfg.MachineReadable {
		for _, path := range paths {
			fmt.Fprintf(os.Stdout, "%s\x00%s\x00%s\x00%s\n", repo, pkg, ver, path)
		}
		return
	}
	if a.cfg.Quiet {
		fmt.Fprintf(os.Stdout, "%s/%s\n", repo, pkg)
		return
	}
	if exact {
		for _, path := range paths {
			if ver != "" {
				fmt.Fprintf(os.Stdout, "%s is owned by %s/%s %s\n", path,
					render.Repo(repo), render.Bold(pkg), render.Version(ver))
			} else {
				fmt.Fprintf(os.Stdout, "%s is owned by %s/%s\n", path,
					render.Repo(repo), render.Bold(pkg))
			}
		}
		return
	}
	if ver != "" {
		fmt.Fprintf(os.Stdout, "%s/%s %s", render.Repo(repo), render.Bold(pkg), render.Version(ver))
	} else {
		fmt.Fprintf(os.Stdout, "%s/%s", render.Repo(repo), render.Bold(pkg))
	}
	if local != nil && ver != "" {
		fmt.Fprint(os.Stdout, render.Meta(installedMark(local, pkg, ver)))
	}
	fmt.Fprintln(os.Stdout)
	for _, path := range paths {
		fmt.Fprintf(os.Stdout, "    %s\n", path)
	}
}

func (a *App) filesList(db *deb.ContentsDB) int {
	var versions *fileVersions
	if a.cfg.MachineReadable {
		versions = newFileVersions()
	}

	if len(a.targets) == 0 {
		for _, repo := range db.Repos {
			err := repo.Scan(func(path string, pkgs []string) bool {
				for _, pkg := range pkgs {
					a.printFileLine(repo.Label, pkg, path, versions)
				}
				return true
			})
			if err != nil {
				Warnf("could not read the files database '%s': %v", repo.Label, err)
			}
		}
		return 0
	}

	ret := 0
	for _, raw := range a.targets {
		repoFilter, name, hasRepo, invalid := splitFilesTarget(db, raw)
		if invalid {
			Errorf("invalid package: '%s'", raw)
			ret++
			continue
		}

		found := false
		for _, repo := range db.Repos {
			if hasRepo && repo.Label != repoFilter {
				continue
			}
			err := repo.Scan(func(path string, pkgs []string) bool {
				if !containsString(pkgs, name) {
					return true
				}
				found = true
				a.printFileLine(repo.Label, name, path, versions)
				return true
			})
			if err != nil {
				Warnf("could not read the files database '%s': %v", repo.Label, err)
			}
			if found {
				break
			}
		}
		if !found {
			Errorf("package '%s' was not found", raw)
			ret++
		}
	}
	return ret
}

func splitFilesTarget(db *deb.ContentsDB, raw string) (repo, pkg string, hasRepo, invalid bool) {
	slash := strings.IndexByte(raw, '/')
	if slash < 0 {
		return "", raw, false, false
	}
	// Debian repo labels such as "crimson/main" contain '/', so prefer the
	// longest known label over pacman's first-slash split.
	best := ""
	for _, r := range db.Repos {
		if strings.HasPrefix(raw, r.Label+"/") && len(r.Label) > len(best) {
			best = r.Label
		}
	}
	if best != "" {
		pkg = raw[len(best)+1:]
		if pkg == "" {
			return "", "", true, true
		}
		return best, pkg, true, false
	}
	if slash == len(raw)-1 {
		return "", "", true, true
	}
	return raw[:slash], raw[slash+1:], true, false
}

func (a *App) printFileLine(repo, pkg, path string, versions *fileVersions) {
	if a.cfg.MachineReadable {
		fmt.Fprintf(os.Stdout, "%s\x00%s\x00%s\x00%s\n", repo, pkg, versions.get(repo, pkg), path)
		return
	}
	if a.cfg.Quiet {
		fmt.Fprintln(os.Stdout, path)
		return
	}
	fmt.Fprintf(os.Stdout, "%s %s\n", render.Bold(pkg), path)
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
