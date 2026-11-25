package app

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"pacdpkg/internal/alpm"
	"pacdpkg/internal/deb"
	"pacdpkg/internal/render"
)

func (a *App) sync() int {
	cfg := a.cfg
	if cfg.Clean > 0 {
		return a.syncClean()
	}
	if cfg.Refresh > 0 {
		if ret := a.syncRefresh(); ret != 0 {
			return ret
		}
	}
	switch {
	case cfg.Search:
		return a.syncSearch()
	case cfg.Group > 0:
		return a.syncGroup()
	case cfg.Info > 0:
		return a.syncInfo()
	case cfg.List:
		return a.syncList()
	}
	if cfg.SysUpgrade > 0 {
		return a.syncUpgrade()
	}
	if len(a.targets) == 0 {
		if cfg.Refresh > 0 {
			return 0
		}
		Errorf("no targets specified (use -h for help)")
		return 1
	}
	return a.syncTransaction()
}

func (a *App) loadSync() *deb.Sync {
	s, err := deb.LoadSync()
	if err != nil {
		Errorf("could not read the package lists: %v", err)
		return nil
	}
	for _, w := range s.Warnings {
		Warnf("%s", w)
	}
	return s
}

func installedMark(db *deb.DB, name, version string) string {
	p := db.Get(name)
	if p == nil {
		return ""
	}
	if p.Version == version {
		return " [installed]"
	}
	return " [installed: " + p.Version + "]"
}

func (a *App) syncSearch() int {
	db, err := deb.Open()
	if err != nil {
		Errorf("could not read the dpkg database: %v", err)
		return 1
	}
	sync := a.loadSync()
	if sync == nil {
		return 1
	}

	var patterns []*regexp.Regexp
	for _, t := range a.targets {
		re, err := regexp.Compile("(?i)" + t)
		if err != nil {
			Errorf("invalid regular expression '%s'", t)
			return 1
		}
		patterns = append(patterns, re)
	}

	found := false
	seen := map[string]bool{}
	for _, p := range sync.Pkgs {
		if len(patterns) > 0 && !matchesSync(patterns, p) {
			continue
		}
		key := p.Repo + "/" + p.Name + " " + p.Version
		if seen[key] {
			continue
		}
		seen[key] = true
		render.SearchResult(os.Stdout, syncInfoPkg(p), p.Repo, installedMark(db, p.Name, p.Version), a.cfg.Quiet, getCols())
		found = true
	}
	if !found {
		return 1
	}
	return 0
}

func matchesSync(patterns []*regexp.Regexp, p *deb.SyncPkg) bool {
	for _, re := range patterns {
		if !matchSyncPattern(re, p) {
			return false
		}
	}
	return true
}

func matchSyncPattern(re *regexp.Regexp, p *deb.SyncPkg) bool {
	if re.MatchString(p.Name) || re.MatchString(p.Description) || re.MatchString(p.Section) {
		return true
	}
	for _, prov := range p.Provides {
		for _, alt := range prov.Alts {
			if re.MatchString(alt.Name) {
				return true
			}
		}
	}
	return false
}

func syncInfoPkg(p *deb.SyncPkg) *alpm.Pkg {
	return &alpm.Pkg{
		Name:          p.Name,
		Version:       p.Version,
		Arch:          p.Arch,
		Description:   p.Description,
		URL:           p.Homepage,
		Groups:        nonEmptyList(p.Section),
		Provides:      p.Provides,
		Depends:       p.Depends,
		OptDepends:    p.OptDepends,
		Conflicts:     p.Conflicts,
		Replaces:      p.Replaces,
		Repo:          p.Repo,
		SHA256:        p.SHA256,
		MD5:           p.MD5,
		DownloadSize:  p.DownloadSize,
		InstalledSize: p.InstalledSize,
		Packager:      p.Maintainer,
		Validated:     "SHA-256 Sum",
	}
}

func nonEmptyList(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

func (a *App) syncInfo() int {
	sync := a.loadSync()
	if sync == nil {
		return 1
	}
	cols := getCols()
	ret := 0
	if len(a.targets) == 0 {
		for _, p := range sync.Pkgs {
			render.PkgInfo(os.Stdout, syncInfoPkg(p), render.InfoOpts{
				Origin: render.FromSync, Extra: a.cfg.Info > 1, Cols: cols,
			})
		}
		return 0
	}
	repos := sync.Repos()
	knownRepo := map[string]bool{}
	for _, r := range repos {
		knownRepo[r] = true
	}
	for _, t := range a.targets {
		repo, name, _ := splitSyncTarget(repos, t)
		if repo != "" && !knownRepo[repo] {
			Errorf("repository '%s' does not exist", repo)
			Errorf("package '%s' was not found", t)
			ret = 2
			continue
		}
		var hits []*deb.SyncPkg
		for _, p := range sync.Lookup(name) {
			if repo == "" || p.Repo == repo {
				hits = append(hits, p)
			}
		}
		if len(hits) == 0 {
			Errorf("package '%s' was not found", t)
			if ret == 0 {
				ret = 1
			}
			continue
		}
		for _, p := range hits {
			render.PkgInfo(os.Stdout, syncInfoPkg(p), render.InfoOpts{
				Origin: render.FromSync, Extra: a.cfg.Info > 1, Cols: cols,
			})
		}
	}
	return ret
}

// splitSyncTarget splits an optional "<repo>/" prefix (repository labels may
// contain slashes) and an optional "=version" suffix.
func splitSyncTarget(repos []string, target string) (repo, name, version string) {
	best := ""
	for _, r := range repos {
		if strings.HasPrefix(target, r+"/") && len(r) > len(best) {
			best = r
		}
	}
	if best != "" {
		target = strings.TrimPrefix(target, best+"/")
		repo = best
	} else if i := strings.IndexByte(target, '/'); i > 0 {
		repo, target = target[:i], target[i+1:]
	}
	if i := strings.IndexByte(target, '='); i > 0 {
		target, version = target[:i], target[i+1:]
	}
	return repo, target, version
}

func (a *App) syncList() int {
	sync := a.loadSync()
	if sync == nil {
		return 1
	}
	db, err := deb.Open()
	if err != nil {
		Errorf("could not read the dpkg database: %v", err)
		return 1
	}
	ret := 0
	wanted := map[string]bool{}
	if len(a.targets) > 0 {
		known := map[string]bool{}
		for _, r := range sync.Repos() {
			known[r] = true
		}
		for _, t := range a.targets {
			if !known[t] {
				Errorf("repository \"%s\" was not found.", t)
				ret = 1
				continue
			}
			wanted[t] = true
		}
	}
	for _, p := range sync.Pkgs {
		if len(wanted) > 0 && !wanted[p.Repo] {
			continue
		}
		if a.cfg.Quiet {
			fmt.Fprintln(os.Stdout, p.Name)
			continue
		}
		fmt.Fprintf(os.Stdout, "%s %s %s%s\n",
			render.Repo(p.Repo), render.Bold(p.Name), render.Version(p.Version),
			render.Meta(installedMark(db, p.Name, p.Version)))
	}
	return ret
}

func (a *App) syncGroup() int {
	sync := a.loadSync()
	if sync == nil {
		return 1
	}
	groups := map[string][]*deb.SyncPkg{}
	for _, p := range sync.Pkgs {
		if p.Section == "" {
			continue
		}
		groups[p.Section] = append(groups[p.Section], p)
	}
	var names []string
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)

	if len(a.targets) == 0 {
		if a.cfg.Group > 1 {
			for _, name := range names {
				for _, p := range groups[name] {
					fmt.Fprintf(os.Stdout, "%s %s\n", name, p.Name)
				}
			}
			return 0
		}
		for _, name := range names {
			fmt.Fprintln(os.Stdout, name)
		}
		return 0
	}
	ret := 0
	for _, t := range a.targets {
		pkgs, ok := groups[t]
		if !ok {
			Errorf("package group '%s' was not found", t)
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

func (a *App) syncRefresh() int {
	fmt.Fprintln(os.Stdout, ":: Synchronizing package databases...")
	lock, err := lockLists()
	if err != nil {
		Errorf("could not get the package list lock: %v", err)
		return 1
	}
	recoverLists()

	backup := false
	if a.cfg.Refresh > 1 {
		backup, err = moveListsAside()
		if err != nil {
			Warnf("could not move the old package lists aside: %v", err)
			if backup {
				restoreLists()
			}
			unlockLists(lock)
			return 1
		}
	}
	unlockLists(lock)
	lock = nil
	defer func() { unlockLists(lock) }()

	if err := runUpdate(); err != nil {
		if l, lerr := lockLists(); lerr == nil {
			lock = l
		}
		if backup {
			restoreLists()
		}
		Errorf("failed to synchronize all databases: %v", err)
		return 1
	}
	if lock, err = lockLists(); err != nil {
		Errorf("could not get the package list lock: %v", err)
		return 1
	}
	if broken := repositoriesWithoutIndex(); len(broken) > 0 {
		Errorf("failed to synchronize all databases (failed to retrieve some files)")
		for _, repo := range broken {
			Warnf("no package index for repository: %s", repo)
		}
		if backup {
			restoreLists()
		}
		return 1
	}
	if backup {
		os.RemoveAll(listsBackup)
	}
	return 0
}

// lockFile takes the same fcntl lock apt uses for a directory.
func lockFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	lk := syscall.Flock_t{Type: syscall.F_WRLCK}
	if err := syscall.FcntlFlock(f.Fd(), syscall.F_SETLK, &lk); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func unlockFile(f *os.File) {
	if f == nil {
		return
	}
	lk := syscall.Flock_t{Type: syscall.F_UNLCK}
	syscall.FcntlFlock(f.Fd(), syscall.F_SETLK, &lk)
	f.Close()
}

func lockLists() (*os.File, error) {
	return lockFile(filepath.Join(listsDir, "lock"))
}

func unlockLists(f *os.File) { unlockFile(f) }

// runUpdate streams apt-get update and restores on SIGINT/SIGTERM so an
// interrupted -Syy does not leave the lists directory empty.
func runUpdate() error {
	args := append(append([]string{}, deb.ExtraArgs...), "update")
	cmd := exec.Command("apt-get", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(ch)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case sig := <-ch:
		_ = cmd.Process.Signal(sig)
		<-done
		return fmt.Errorf("interrupted")
	}
}

// recoverLists restores a backup left by a killed -Syy before apt reads the
// lists again.
func recoverLists() {
	entries, err := os.ReadDir(listsBackup)
	if err != nil || len(entries) == 0 {
		return
	}
	restoreLists()
}

// repositoriesWithoutIndex reports configured repositories that produced no
// package index at all.
func repositoriesWithoutIndex() []string {
	var broken []string
	for _, group := range deb.ExpectedListGroups() {
		found := false
		for _, name := range group {
			if indexExists(name) {
				found = true
				break
			}
		}
		if !found && len(group) > 0 {
			broken = append(broken, group[0])
		}
	}
	return broken
}

const (
	listsDir    = "/var/lib/apt/lists"
	listsBackup = "/var/cache/pacdpkg/lists-backup"
)

// moveListsAside stashes the package lists so a forced refresh (-Syy) can be
// rolled back when apt fails.
func moveListsAside() (bool, error) { return moveListsFrom(listsDir, listsBackup) }

func moveListsFrom(dir, backup string) (bool, error) {
	// A leftover backup means a previous refresh was interrupted; recover it
	// instead of discarding the only copy of the indexes.
	if entries, err := os.ReadDir(backup); err == nil && len(entries) > 0 {
		restoreListsFrom(dir, backup)
	}
	if err := os.RemoveAll(backup); err != nil {
		return false, err
	}
	if err := os.MkdirAll(backup, 0o755); err != nil {
		return false, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	moved := false
	for _, e := range entries {
		name := e.Name()
		if !strings.Contains(name, "_Packages") && !strings.Contains(name, "_Release") &&
			!strings.Contains(name, "_InRelease") {
			continue
		}
		if err := moveFile(filepath.Join(dir, name), filepath.Join(backup, name)); err != nil {
			return moved, err
		}
		moved = true
	}
	if !moved {
		os.RemoveAll(backup)
	}
	return moved, nil
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}
	return os.Remove(src)
}

func restoreLists() { restoreListsFrom(listsDir, listsBackup) }

func restoreListsFrom(dir, backup string) {
	entries, err := os.ReadDir(backup)
	if err != nil {
		return
	}
	keep := map[string]bool{}
	for _, e := range entries {
		keep[e.Name()] = true
	}
	if current, err := os.ReadDir(dir); err == nil {
		for _, e := range current {
			name := e.Name()
			if keep[name] {
				continue
			}
			if strings.Contains(name, "_Packages") || strings.Contains(name, "_Release") ||
				strings.Contains(name, "_InRelease") {
				os.Remove(filepath.Join(dir, name))
			}
		}
	}
	for _, e := range entries {
		dst := filepath.Join(dir, e.Name())
		os.Remove(dst)
		if err := moveFile(filepath.Join(backup, e.Name()), dst); err != nil {
			Warnf("could not restore %s: %v", e.Name(), err)
		}
	}
	os.RemoveAll(backup)
}

func (a *App) syncClean() int {
	if a.cfg.Clean == 1 {
		fmt.Fprintln(os.Stdout, "Packages to keep:")
		fmt.Fprintln(os.Stdout, "  All locally installed packages")
		fmt.Fprintln(os.Stdout)
		fmt.Fprintf(os.Stdout, "Cache directory: %s\n", a.cacheDir())
		if !render.Question("Do you want to remove all other packages from cache?", true, a.cfg.NoConfirm) {
			fmt.Fprintln(os.Stdout)
			return 0
		}
		fmt.Fprintln(os.Stdout, "removing old packages from cache...")
		if err := cleanCache(a.cacheDir(), 1); err != nil {
			Errorf("%v", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(os.Stdout, "Cache directory: %s\n", a.cacheDir())
	if !render.Question("Do you want to remove ALL files from cache?", false, a.cfg.NoConfirm) {
		fmt.Fprintln(os.Stdout)
		return 0
	}
	fmt.Fprintln(os.Stdout, "removing all files from cache...")
	if err := cleanCache(a.cacheDir(), 2); err != nil {
		Errorf("%v", err)
		return 1
	}
	return 0
}

const archiveDir = "/var/cache/apt/archives"

func (a *App) cacheDir() string {
	if a.cfg.CacheDir != "" {
		return a.cfg.CacheDir
	}
	if a.conf.CacheDir != "" {
		return a.conf.CacheDir
	}
	return archiveDir
}

// cleanCache implements -Sc/-Scc. Level 1 keeps cached packages that are
// installed (pacman's default CleanMethod), level 2 removes everything.
func cleanCache(dir string, level int) error {
	lock, err := lockFile(filepath.Join(dir, "lock"))
	if err != nil {
		return fmt.Errorf("could not get the cache lock: %w", err)
	}
	defer unlockFile(lock)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	if level == 1 {
		db, err := deb.Open()
		if err != nil {
			return err
		}
		for _, p := range db.Pkgs {
			base := p.Name
			if i := strings.IndexByte(base, ':'); i >= 0 {
				base = base[:i]
			}
			keep[base+"|"+p.Version] = true
			keep[p.Name+"|"+p.Version] = true
		}
	}
	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(dir, name)
		if level > 1 {
			if e.IsDir() || name == "lock" {
				continue
			}
			if err := os.Remove(path); err != nil {
				return err
			}
			continue
		}
		if e.IsDir() || name == "lock" || !strings.HasSuffix(name, ".deb") {
			continue
		}
		pkg, version, ok := parseDebFileName(name)
		if !ok || keep[pkg+"|"+version] {
			continue
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

// parseDebFileName splits name_version_arch.deb. Debian package names and
// versions cannot contain underscores.
func parseDebFileName(name string) (string, string, bool) {
	base := strings.TrimSuffix(name, ".deb")
	archSep := strings.LastIndexByte(base, '_')
	if archSep < 0 {
		return "", "", false
	}
	rest := base[:archSep]
	verSep := strings.LastIndexByte(rest, '_')
	if verSep < 0 {
		return "", "", false
	}
	version := rest[verSep+1:]
	version = strings.ReplaceAll(version, "%3a", ":")
	version = strings.ReplaceAll(version, "%3A", ":")
	return rest[:verSep], version, true
}

// indexExists reports whether an apt index was downloaded, with or without
// compression.
func indexExists(name string) bool {
	for _, suf := range []string{"", ".lz4", ".gz", ".xz", ".zst", ".bz2"} {
		if _, err := os.Stat(filepath.Join(listsDir, name+suf)); err == nil {
			return true
		}
	}
	return false
}
