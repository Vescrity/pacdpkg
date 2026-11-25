package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"pacdpkg/internal/alpm"
	"pacdpkg/internal/aptplan"
	"pacdpkg/internal/deb"
	"pacdpkg/internal/render"
)

func getCols() int {
	if c := os.Getenv("COLUMNS"); c != "" {
		if n, err := strconv.Atoi(c); err == nil && n > 0 {
			return n
		}
	}
	var ws struct{ Row, Col, X, Y uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdout.Fd(),
		uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if errno == 0 && ws.Col > 0 {
		return int(ws.Col)
	}
	return 80
}

func (a *App) query() int {
	db, err := deb.Open()
	if err != nil {
		Errorf("could not read the dpkg database: %v", err)
		return 1
	}
	switch {
	case a.cfg.Search:
		return a.querySearch(db)
	case a.cfg.Group > 0:
		return a.queryGroup(db)
	case a.cfg.Owns:
		return a.queryOwns(db)
	}
	return a.queryPackages(db)
}

func (a *App) querySearch(db *deb.DB) int {
	var res []searchPattern
	invalid := false
	for _, t := range a.targets {
		pat := compileSearch(t)
		if pat.invalid {
			invalid = true
		}
		res = append(res, pat)
	}
	match := false
	for _, p := range db.Pkgs {
		if len(res) > 0 && !searchMatches(res, p) {
			continue
		}
		render.SearchResult(os.Stdout, p, "local", "", a.cfg.Quiet, getCols())
		match = true
	}
	if !match {
		if invalid {
			Errorf("search failed: invalid regular expression")
		}
		return 1
	}
	return 0
}

type searchPattern struct {
	re      *regexp.Regexp
	literal string
	invalid bool
}

func compileSearch(t string) searchPattern {
	re, err := regexp.Compile("(?i)" + t)
	if err == nil {
		return searchPattern{re: re}
	}
	if literalFallback(t) {
		return searchPattern{literal: t}
	}
	return searchPattern{invalid: true}
}

// literalFallback keeps patterns like libsigc++ searchable even though Go's
// RE2 rejects the repeated quantifier. Unbalanced brackets or parentheses
// stay invalid, matching pacman's error for patterns such as "[".
func literalFallback(t string) bool {
	if t == "" || strings.ContainsAny(t[:1], "*+?{") {
		return false
	}
	brackets, parens := 0, 0
	escaped := false
	for _, r := range t {
		if escaped {
			escaped = false
			continue
		}
		switch r {
		case '\\':
			escaped = true
		case '[':
			brackets++
		case ']':
			if brackets > 0 {
				brackets--
			}
		case '(':
			parens++
		case ')':
			if parens > 0 {
				parens--
			}
		}
	}
	return !escaped && brackets == 0 && parens == 0
}

func (p searchPattern) match(s string) bool {
	if p.re != nil {
		return p.re.MatchString(s)
	}
	if p.invalid {
		return false
	}
	return strings.Contains(strings.ToLower(s), strings.ToLower(p.literal))
}

func searchMatches(res []searchPattern, p *alpm.Pkg) bool {
	for _, pat := range res {
		if !matchPattern(pat, p) {
			return false
		}
	}
	return true
}

func matchPattern(pat searchPattern, p *alpm.Pkg) bool {
	if pat.match(p.Name) || pat.match(p.Description) {
		return true
	}
	for _, g := range p.Groups {
		if pat.match(g) {
			return true
		}
	}
	for _, prov := range p.Provides {
		for _, alt := range prov.Alts {
			if pat.match(alt.Name) {
				return true
			}
		}
	}
	return false
}

func (a *App) queryGroup(db *deb.DB) int {
	sections := map[string][]*alpm.Pkg{}
	for _, p := range db.Pkgs {
		for _, g := range p.Groups {
			sections[g] = append(sections[g], p)
		}
	}
	ret := 0
	if len(a.targets) == 0 {
		allowed, err := a.filterSet(db)
		if err != nil {
			Errorf("%v", err)
			return 1
		}
		var names []string
		for name := range sections {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			for _, p := range sections[name] {
				if allowed != nil && !allowed.allowed[p.Name] {
					continue
				}
				fmt.Fprintf(os.Stdout, "%s %s\n", name, p.Name)
			}
		}
		return 0
	}
	for _, t := range a.targets {
		pkgs, ok := sections[t]
		if !ok {
			Errorf("group '%s' was not found", t)
			ret = 1
			continue
		}
		for _, p := range pkgs {
			if a.cfg.Quiet {
				fmt.Fprintln(os.Stdout, p.Name)
			} else {
				fmt.Fprintf(os.Stdout, "%s %s\n", t, p.Name)
			}
		}
	}
	return ret
}

func (a *App) queryOwns(db *deb.DB) int {
	if len(a.targets) == 0 {
		Errorf("no targets specified (use -h for help)")
		return 1
	}
	type target struct {
		path    string
		display string
	}
	var targets []target
	var paths []string
	ret := 0
	for _, t := range a.targets {
		if t == "" {
			Errorf("empty string passed to file owner query")
			ret = 1
			continue
		}
		path := resolvePath(t)
		display := path
		if st, err := os.Stat(path); err == nil && st.IsDir() {
			display = strings.TrimSuffix(path, "/") + "/"
		}
		targets = append(targets, target{path: path, display: display})
		paths = append(paths, path)
		if abs, err := filepath.Abs(t); err == nil && abs != path {
			paths = append(paths, abs)
		}
	}
	if len(targets) == 0 {
		return ret
	}

	ownersByPath := db.OwnersOfAll(paths)
	for _, tg := range targets {
		seen := map[string]bool{}
		var owners []*alpm.Pkg
		addOwners := func(list []*alpm.Pkg) {
			for _, p := range list {
				if seen[p.Name] {
					continue
				}
				seen[p.Name] = true
				owners = append(owners, p)
			}
		}
		addOwners(ownersByPath[tg.path])
		if abs, err := filepath.Abs(tg.path); err == nil {
			addOwners(ownersByPath[abs])
		}
		if len(owners) == 0 {
			Errorf("No package owns %s", tg.display)
			ret = 1
			continue
		}
		for _, p := range owners {
			if a.cfg.Quiet {
				fmt.Fprintln(os.Stdout, p.Name)
			} else {
				fmt.Fprintf(os.Stdout, "%s is owned by %s %s\n", tg.display, p.Name, p.Version)
			}
		}
	}
	return ret
}

func resolvePath(p string) string {
	if !strings.Contains(p, "/") {
		lp, err := exec.LookPath(p)
		if err != nil {
			return p
		}
		p = lp
	}
	dir, base := filepath.Split(p)
	if dir == "" {
		dir = "."
	}
	if rd, err := filepath.EvalSymlinks(dir); err == nil {
		dir = rd
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return filepath.Join(dir, base)
}

func (a *App) queryPackages(db *deb.DB) int {
	if a.cfg.IsFile {
		return a.queryPackageFiles()
	}
	if len(a.targets) == 0 {
		if a.cfg.Owns {
			Errorf("no targets specified (use -h for help)")
			return 1
		}
		st, err := a.filterSet(db)
		if err != nil {
			Errorf("%v", err)
			return 1
		}
		match := false
		ret := 0
		for _, p := range db.Pkgs {
			if st != nil && !st.allowed[p.Name] {
				continue
			}
			if !a.display(db, p, st) {
				ret = 1
			}
			match = true
		}
		if !match {
			return 1
		}
		return ret
	}

	ret := 0
	match := false
	st, err := a.filterSet(db)
	if err != nil {
		Errorf("%v", err)
		return 1
	}
	for _, t := range a.targets {
		name := strings.TrimPrefix(t, "local/")
		p := db.Get(name)
		if p == nil {
			Errorf("package '%s' was not found", name)
			if _, err := os.Stat(name); err == nil {
				Warnf("'%s' is a file, you might want to use -p/--file.", name)
			}
			ret = 1
			continue
		}
		if st != nil && !st.allowed[p.Name] {
			continue
		}
		if !a.display(db, p, st) {
			ret = 1
		}
		match = true
	}
	if !match && ret == 0 {
		ret = 1
	}
	return ret
}

type queryState struct {
	allowed   map[string]bool
	candidate map[string]string
}

// filterSet returns nil when no filter is active, or the state describing
// which packages pass every active filter.
func (a *App) filterSet(db *deb.DB) (*queryState, error) {
	cfg := a.cfg
	needRepo := cfg.Foreign || cfg.Native || cfg.Upgrades
	var repo map[string]*deb.Policy
	if needRepo {
		sync := a.loadSync()
		if sync == nil {
			return nil, fmt.Errorf("could not read the package lists")
		}
		names := make([]string, 0, len(db.Pkgs))
		for _, p := range db.Pkgs {
			names = append(names, p.Name)
		}
		repo = sync.Policies(db, names)
	}
	var unreq map[string]bool
	if cfg.Unrequired > 0 {
		set, err := unrequiredSet(db, cfg.Unrequired)
		if err != nil {
			return nil, err
		}
		unreq = set
	}
	if !needRepo && unreq == nil && !cfg.Deps && !cfg.Explicit {
		return nil, nil
	}
	if cfg.Deps || cfg.Explicit {
		db.EnsureReasons()
	}
	st := &queryState{allowed: map[string]bool{}, candidate: map[string]string{}}
	for _, p := range db.Pkgs {
		if cfg.Deps && p.Explicit {
			continue
		}
		if cfg.Explicit && !p.Explicit {
			continue
		}
		if repo != nil {
			pi := repo[p.Name]
			inRepo := pi != nil && pi.FromRepo
			if cfg.Foreign && inRepo {
				continue
			}
			if cfg.Native && !inRepo {
				continue
			}
			if cfg.Upgrades {
				if pi == nil || pi.Candidate == "" || alpm.CompareVersion(pi.Candidate, p.Version) <= 0 {
					continue
				}
			}
		}
		if unreq != nil && !unreq[p.Name] {
			continue
		}
		st.allowed[p.Name] = true
		if repo != nil && repo[p.Name] != nil {
			st.candidate[p.Name] = repo[p.Name].Candidate
		}
	}
	return st, nil
}

func unrequiredSet(db *deb.DB, level int) (map[string]bool, error) {
	if aptplan.Available() {
		if set, err := aptplan.Orphans(level > 1); err == nil {
			return set, nil
		}
	}
	db.EnsureReasons()
	set := map[string]bool{}
	for _, p := range db.Pkgs {
		if p.Explicit || len(p.RequiredBy) > 0 {
			continue
		}
		if level < 2 && len(p.OptionalFor) > 0 {
			continue
		}
		set[p.Name] = true
	}
	return set, nil
}

func (a *App) display(db *deb.DB, p *alpm.Pkg, st *queryState) bool {
	cfg := a.cfg
	cols := getCols()
	if cfg.Info > 0 {
		db.Enrich(p)
		render.PkgInfo(os.Stdout, p, render.InfoOpts{Origin: render.FromLocal, Extra: cfg.Info > 1, Cols: cols})
	}
	if cfg.List {
		files, err := db.Files(p)
		if err != nil {
			Errorf("%s: %v", p.Name, err)
			return false
		}
		for _, f := range files {
			render.FileLine(os.Stdout, p.Name, f, cfg.Quiet)
		}
	}
	if cfg.Changelog {
		if text, ok := deb.Changelog(p.Name); ok {
			fmt.Fprintf(os.Stdout, "Changelog for %s:\n", p.Name)
			io.WriteString(os.Stdout, text)
			if !strings.HasSuffix(text, "\n") {
				fmt.Fprintln(os.Stdout)
			}
			fmt.Fprintln(os.Stdout)
		} else {
			Errorf("no changelog available for '%s'.", p.Name)
		}
	}
	if cfg.Check > 0 {
		return a.checkPackage(db, p)
	}
	if cfg.Info == 0 && !cfg.List && !cfg.Changelog {
		if cfg.Upgrades {
			newver := ""
			if st != nil {
				newver = st.candidate[p.Name]
			}
			ignored := db.Held(p.Name) || a.ignored(p.Name) || a.ignoredGroup(p)
			render.UpgradeLine(os.Stdout, p, newver, ignored, cfg.Quiet)
		} else {
			render.PkgLine(os.Stdout, p, cfg.Quiet)
		}
	}
	return true
}

func (a *App) checkPackage(db *deb.DB, p *alpm.Pkg) bool {
	res, err := db.Verify(p, a.cfg.Check > 1)
	if err != nil {
		Errorf("%s: %v", p.Name, err)
		return false
	}
	if a.cfg.Check == 1 {
		if !a.cfg.Quiet {
			fmt.Fprintf(os.Stdout, "%s: %d total files, %d missing files\n", p.Name, res.Total, res.Missing)
		}
		for _, path := range res.MissingPaths {
			Warnf("%s: %s (No such file or directory)", p.Name, path)
		}
		return res.Missing == 0
	}
	for _, path := range res.MissingPaths {
		Warnf("%s: %s (No such file or directory)", p.Name, path)
	}
	for i, path := range res.AlteredPaths {
		reason := "Checksum mismatch"
		if i < len(res.AlteredReasons) {
			reason = res.AlteredReasons[i]
		}
		Warnf("%s: %s (%s)", p.Name, path, reason)
	}
	if !a.cfg.Quiet {
		fmt.Fprintf(os.Stdout, "%s: %d total files, %d altered files\n", p.Name, res.Total, res.Altered+res.Missing)
	}
	return res.Altered == 0 && res.Missing == 0
}

// queryPackageFiles implements -Qp: query a .deb instead of the database.
func (a *App) queryPackageFiles() int {
	if len(a.targets) == 0 {
		Errorf("no targets specified (use -h for help)")
		return 1
	}
	ret := 0
	for _, t := range a.targets {
		pf, err := deb.OpenPackageFile(t)
		if err != nil {
			Errorf("could not load package '%s': %v", t, err)
			ret = 1
			continue
		}
		p := pf.Pkg
		if a.cfg.Info > 0 {
			render.PkgInfo(os.Stdout, p, render.InfoOpts{
				Origin: render.FromFile, Extra: a.cfg.Info > 1, Cols: getCols(),
			})
		}
		if a.cfg.List {
			for _, f := range pf.Files {
				render.FileLine(os.Stdout, p.Name, f, a.cfg.Quiet)
			}
		}
		if a.cfg.Check > 0 {
			missing := 0
			for _, f := range pf.Files {
				if _, err := os.Lstat(f); err != nil {
					missing++
				}
			}
			fmt.Fprintf(os.Stdout, "%s: %d total files, %d missing files\n", p.Name, len(pf.Files), missing)
			if missing > 0 {
				ret = 1
			}
		}
		if a.cfg.Info == 0 && !a.cfg.List && a.cfg.Check == 0 {
			render.PkgLine(os.Stdout, p, a.cfg.Quiet)
		}
	}
	return ret
}
