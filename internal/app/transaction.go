package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pacdpkg/internal/alpm"
	"pacdpkg/internal/aptplan"
	"pacdpkg/internal/deb"
	"pacdpkg/internal/render"
)

func (a *App) unsupportedOptions() bool {
	for _, e := range []struct {
		used bool
		name string
	}{
		{a.cfg.DBOnly, "--dbonly"},
		{a.cfg.NoScriptlet, "--noscriptlet"},
		{a.cfg.Nodeps > 0, "--nodeps"},
		{len(a.cfg.AssumeInstalled) > 0, "--assume-installed"},
		{len(a.cfg.Overwrite) > 0, "--overwrite"},
	} {
		if e.used {
			Errorf("option '%s' has no equivalent with the deb backend", e.name)
			return true
		}
	}
	return false
}

func (a *App) syncTransaction() int {
	if a.unsupportedOptions() {
		return 1
	}
	sync := a.loadSync()
	if sync == nil {
		return 1
	}
	db, err := deb.Open()
	if err != nil {
		Errorf("could not read the dpkg database: %v", err)
		return 1
	}

	repos := sync.Repos()
	known := map[string]bool{}
	for _, r := range repos {
		known[r] = true
	}
	var targets []deb.InstallTarget
	overrideHeld := false
	for _, t := range a.targets {
		repo, name, version := splitSyncTarget(repos, t)
		if repo != "" && !known[repo] {
			Errorf("database not found: %s", repo)
			return 1
		}
		var cand *deb.SyncPkg
		if version != "" {
			cand = sync.Find(name, version)
			if cand != nil && repo != "" && cand.Repo != repo {
				cand = nil
			}
		} else {
			cand = sync.CandidateIn(repo, name)
		}
		if cand == nil {
			Errorf("target not found: %s", t)
			return 1
		}
		if local := db.Get(name); local != nil {
			switch alpm.CompareVersion(local.Version, cand.Version) {
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
					Warnf("downgrading package %s (%s => %s)", local.Name, local.Version, cand.Version)
				}
			}
		}
		if db.Held(name) {
			overrideHeld = true
			if !a.cfg.Print && !a.cfg.DownloadOnly {
				if !render.Question(name+" is in IgnorePkg/IgnoreGroup. Install anyway?", true, a.cfg.NoConfirm) {
					return 1
				}
			}
		}
		if a.ignored(name) || a.ignoredGroup(syncInfoPkg(cand)) {
			if !render.Question(name+" is in IgnorePkg/IgnoreGroup. Install anyway?", true, a.cfg.NoConfirm) {
				return 1
			}
		}
		targets = append(targets, deb.InstallTarget{Name: name, Repo: repo, Version: version})
	}

	if !a.cfg.Print {
		fmt.Fprintln(os.Stdout, "resolving dependencies...")
		fmt.Fprintln(os.Stdout, "looking for conflicting packages...")
	}
	if aptplan.Available() {
		var ats []aptplan.Target
		for _, t := range targets {
			ats = append(ats, aptplan.Target{Name: t.Name, Version: t.Version, Repo: t.Repo})
		}
		res, err := aptplan.Resolve(aptplan.OpInstall, ats, aptplan.Options{
			Needed:       a.cfg.Needed,
			AsDeps:       a.cfg.AsDeps,
			AsExplicit:   a.cfg.AsExplicit,
			OverrideHeld: overrideHeld,
			Lock:         !a.cfg.Print && !a.cfg.DownloadOnly,
		})
		if err != nil {
			Errorf("%v", err)
			return 1
		}
		if len(res.Problems) > 0 {
			a.reportPlanProblems(nil, res.Problems)
			return 1
		}
		enrichPlan(sync, db, res.Plan)
		if !a.confirmRemovals(res.Plan) {
			aptplan.Close(res.Session)
			return 1
		}
		return a.commitPlan(res.Plan, txOpts{})
	}
	plan, problems := sync.ResolveInstall(db, targets, false)
	if a.reportPlanProblems(plan, problems) {
		return 1
	}
	return a.commitPlan(plan, txOpts{})
}

func (a *App) syncUpgrade() int {
	if a.unsupportedOptions() {
		return 1
	}
	sync := a.loadSync()
	if sync == nil {
		return 1
	}
	db, err := deb.Open()
	if err != nil {
		Errorf("could not read the dpkg database: %v", err)
		return 1
	}

	if !a.cfg.Print {
		fmt.Fprintln(os.Stdout, ":: Starting full system upgrade...")
		fmt.Fprintln(os.Stdout, "resolving dependencies...")
		fmt.Fprintln(os.Stdout, "looking for conflicting packages...")
	}
	var targets []deb.InstallTarget
	if len(a.targets) > 0 {
		repos := sync.Repos()
		for _, t := range a.targets {
			repo, name, version := splitSyncTarget(repos, t)
			targets = append(targets, deb.InstallTarget{Name: name, Repo: repo, Version: version})
		}
	}
	if !a.cfg.Print {
		for _, p := range db.Pkgs {
			if cand := sync.CandidateIn("", p.Name); cand != nil &&
				db.Held(p.Name) && alpm.CompareVersion(cand.Version, p.Version) > 0 {
				Warnf("%s: ignoring package upgrade (held)", p.Name)
			}
		}
	}
	if aptplan.Available() && !a.hasIgnore() {
		op := aptplan.OpUpgrade
		var ats []aptplan.Target
		if len(targets) > 0 {
			op = aptplan.OpInstall
			for _, t := range targets {
				ats = append(ats, aptplan.Target{Name: t.Name, Version: t.Version, Repo: t.Repo})
			}
		}
		res, err := aptplan.Resolve(op, ats, aptplan.Options{
			Downgrade:   a.cfg.SysUpgrade > 1,
			UpgradeOnly: len(targets) > 0,
			Lock:        !a.cfg.Print && !a.cfg.DownloadOnly,
		})
		if err != nil {
			Errorf("%v", err)
			return 1
		}
		if len(res.Problems) > 0 {
			a.reportPlanProblems(nil, res.Problems)
			return 1
		}
		enrichPlan(sync, db, res.Plan)
		return a.commitPlan(res.Plan, txOpts{})
	}
	plan, problems := sync.ResolveInstall(db, targets, true)
	if a.reportPlanProblems(plan, problems) {
		return 1
	}

	var kept []*deb.PlannedPkg
	dropped := map[string]bool{}
	for _, p := range plan.Install {
		if p.OldVersion != "" && (a.ignored(p.Name) || a.ignoredGroup(syncInfoPkg(p.Sync))) {
			Warnf("%s: ignoring package upgrade (%s => %s)", p.Name, p.OldVersion, p.NewVersion)
			dropped[p.Name] = true
			continue
		}
		kept = append(kept, p)
	}
	plan.Install = kept
	for _, p := range kept {
		if p.Sync == nil {
			continue
		}
		for _, dep := range append(append([]alpm.Dep{}, p.Sync.Depends...), p.Sync.PreDepends...) {
			for _, alt := range dep.Alts {
				if dropped[alt.Name] {
					Errorf("cannot resolve dependency '%s' required by '%s' (ignored package)",
						dep.String(), p.Name)
					return 1
				}
			}
		}
	}
	return a.commitPlan(plan, txOpts{})
}

func (a *App) reportPlanProblems(plan *deb.Plan, problems []string) bool {
	if len(problems) == 0 {
		return false
	}
	for _, p := range problems {
		Errorf("%s", p)
	}
	return true
}

type txOpts struct {
	question  string
	purge     bool
	forceDeps bool
	paths     map[string]string
}

// commitPlan renders a plan, asks for confirmation and executes it.
func (a *App) commitPlan(plan *deb.Plan, o txOpts) int {
	defer aptplan.Close(plan.Session)
	if plan.Empty() {
		if !a.cfg.Print {
			fmt.Fprintln(os.Stdout, " there is nothing to do")
		}
		return 0
	}
	if a.cfg.Print {
		a.printPlan(plan, o.paths)
		return 0
	}

	tx := make([]render.TxPkg, 0, len(plan.Install)+len(plan.Remove))
	for _, p := range plan.Install {
		entry := render.TxPkg{
			Name: p.Name, OldVersion: p.OldVersion, NewVersion: p.NewVersion,
			Repo: p.Repo, Explicit: p.Explicit,
		}
		if p.Sync != nil {
			entry.DownloadSize = p.Sync.DownloadSize
			entry.InstalledSize = p.Sync.InstalledSize
		}
		if p.Local != nil {
			entry.OldInstalledSize = p.Local.InstalledSize
		}
		tx = append(tx, entry)
	}
	for _, p := range plan.Remove {
		tx = append(tx, render.TxPkg{Name: p.Name, OldVersion: p.Version, InstalledSize: p.InstalledSize})
	}

	render.Transaction(os.Stdout, tx, a.conf.VerbosePkgLists, a.cfg.DownloadOnly, getCols())
	question := o.question
	if question == "" {
		question = "Proceed with installation?"
		if a.cfg.DownloadOnly {
			question = "Proceed with download?"
		}
	}
	if !render.Question(question, true, a.cfg.NoConfirm) {
		return 1
	}

	if a.cfg.DownloadOnly {
		fmt.Fprintln(os.Stdout, ":: Retrieving packages...")
	} else {
		fmt.Fprintln(os.Stdout, ":: Processing package changes...")
		a.showChanges(tx)
	}

	if plan.Session != 0 {
		if err := aptplan.Execute(plan.Session, a.cfg.DownloadOnly); err != nil {
			Errorf("%v", err)
			return 1
		}
		return 0
	}

	previous, reasonsOK := a.installReasons()
	if err := a.executePlan(plan, o); err != nil {
		Errorf("%v", err)
		return 1
	}
	if !a.cfg.DownloadOnly && reasonsOK {
		a.applyReasons(plan, previous)
	}
	return 0
}

// installReasons snapshots apt's manual/auto state before a transaction.
func (a *App) installReasons() (map[string]bool, bool) {
	db, err := deb.Open()
	if err != nil {
		return nil, false
	}
	db.EnsureReasons()
	out := make(map[string]bool, len(db.Pkgs))
	for _, p := range db.Pkgs {
		out[p.Name] = p.Explicit
	}
	return out, db.ReasonsKnown()
}

func (a *App) executePlan(plan *deb.Plan, o txOpts) error {
	if len(plan.Install) == 0 {
		purge := o.purge || a.cfg.NoSave
		if o.forceDeps {
			// apt would solve dependencies and remove dependents; dpkg removes
			// exactly the requested set, like pacman -Rdd.
			verb := "-r"
			if purge {
				verb = "-P"
			}
			args := []string{verb, "--force-depends"}
			for _, p := range plan.Remove {
				args = append(args, p.Name)
			}
			return deb.RunStream("dpkg", args...)
		}
		verb := "remove"
		if purge {
			verb = "purge"
		}
		args := []string{verb, "-y", "--"}
		for _, p := range plan.Remove {
			args = append(args, p.Name)
		}
		return deb.RunStream("apt-get", args...)
	}

	if o.forceDeps && allLocalDebs(plan) {
		args := []string{"-i", "--force-depends"}
		for _, p := range plan.Install {
			args = append(args, aptTarget(p.DebPath))
		}
		return deb.RunStream("dpkg", args...)
	}
	args := []string{"install", "-y", "--no-install-recommends"}
	if len(plan.Remove) == 0 {
		args = append(args, "--no-remove")
	}
	if planHasHeld(plan) {
		args = append(args, "--allow-change-held-packages")
	}
	if planHasDowngrade(plan) {
		args = append(args, "-o", "APT::Get::allow-downgrades=true")
	}
	if planHasReinstall(plan) {
		args = append(args, "--reinstall")
	}
	if a.cfg.DownloadOnly {
		args = append(args, "--download-only")
	}
	if o.forceDeps {
		args = append(args, "-o", "Dpkg::Options::=--force-depends")
	}
	args = append(args, "-o", "APT::AutoRemove::RecommendsImportant=false")
	args = append(args, "--")
	for _, p := range plan.Install {
		switch {
		case p.DebPath != "":
			args = append(args, aptTarget(p.DebPath))
		case p.Sync != nil:
			args = append(args, p.Name+"="+p.NewVersion)
		default:
			args = append(args, p.Name)
		}
	}
	for _, p := range plan.Remove {
		args = append(args, p.Name+"-")
	}
	return deb.RunStream("apt-get", args...)
}

func planHasDowngrade(plan *deb.Plan) bool {
	for _, p := range plan.Install {
		if p.OldVersion != "" && alpm.CompareVersion(p.NewVersion, p.OldVersion) < 0 {
			return true
		}
	}
	return false
}

func allLocalDebs(plan *deb.Plan) bool {
	if len(plan.Install) == 0 || len(plan.Remove) > 0 {
		return false
	}
	for _, p := range plan.Install {
		if p.DebPath == "" {
			return false
		}
	}
	return true
}

func planHasHeld(plan *deb.Plan) bool {
	for _, p := range plan.Install {
		if p.Held {
			return true
		}
	}
	return false
}

func planHasReinstall(plan *deb.Plan) bool {
	for _, p := range plan.Install {
		if p.Reinstall {
			return true
		}
	}
	return false
}

// applyReasons restores apt's install-reason bookkeeping. Passing every
// planned package to apt would otherwise mark dependencies as manual.
func (a *App) applyReasons(plan *deb.Plan, previous map[string]bool) {
	var auto, manual []string
	for _, p := range plan.Install {
		if a.cfg.AsDeps {
			auto = append(auto, p.Name)
			continue
		}
		if a.cfg.AsExplicit {
			manual = append(manual, p.Name)
			continue
		}
		if p.Explicit {
			manual = append(manual, p.Name)
			continue
		}
		if wasExplicit, existed := previous[p.Name]; existed {
			if wasExplicit {
				manual = append(manual, p.Name)
			} else {
				auto = append(auto, p.Name)
			}
			continue
		}
		auto = append(auto, p.Name)
	}
	if len(auto) > 0 {
		if err := deb.Run("apt-mark", append([]string{"auto"}, auto...)...); err != nil {
			Warnf("could not mark install reason: %v", err)
		}
	}
	if len(manual) > 0 {
		if err := deb.Run("apt-mark", append([]string{"manual"}, manual...)...); err != nil {
			Warnf("could not mark install reason: %v", err)
		}
	}
}

func (a *App) showChanges(tx []render.TxPkg) {
	for _, t := range tx {
		switch {
		case t.NewVersion == "":
			fmt.Fprintf(os.Stdout, "removing %s...\n", t.Name)
		case t.OldVersion == "":
			fmt.Fprintf(os.Stdout, "installing %s...\n", t.Name)
		case t.NewVersion == t.OldVersion:
			fmt.Fprintf(os.Stdout, "reinstalling %s...\n", t.Name)
		case alpm.CompareVersion(t.NewVersion, t.OldVersion) < 0:
			fmt.Fprintf(os.Stdout, "downgrading %s...\n", t.Name)
		default:
			fmt.Fprintf(os.Stdout, "upgrading %s...\n", t.Name)
		}
	}
}

// printPlan implements -Sp/-Rp/-Up and --print-format without asking apt.
func (a *App) printPlan(plan *deb.Plan, paths map[string]string) {
	format := a.cfg.PrintFormat
	if format == "" {
		for _, p := range plan.Install {
			fmt.Fprintln(os.Stdout, a.pkgLocation(p, paths))
		}
		for _, p := range plan.Remove {
			fmt.Fprintf(os.Stdout, "%s-%s\n", p.Name, p.Version)
		}
		return
	}
	for _, p := range plan.Install {
		line := format
		repos := p.Repo
		size := ""
		if p.DebPath != "" {
			repos = "local"
			if st, err := os.Stat(p.DebPath); err == nil {
				size = fmt.Sprintf("%d", st.Size())
			}
		} else if p.Sync != nil {
			size = fmt.Sprintf("%d", p.Sync.DownloadSize)
		}
		repl := map[string]string{
			"%n": p.Name,
			"%v": p.NewVersion,
			"%r": repos,
			"%l": a.pkgLocation(p, paths),
			"%s": "",
			"%a": "",
			"%d": "",
			"%p": "",
			"%u": "",
			"%f": "",
			"%h": "",
		}
		repl["%s"] = size
		if p.Sync != nil {
			repl["%a"] = p.Sync.Arch
			repl["%d"] = p.Sync.Description
			repl["%p"] = p.Sync.Maintainer
			repl["%u"] = p.Sync.Homepage
			repl["%f"] = filepath.Base(p.Sync.Filename)
			repl["%h"] = p.Sync.SHA256
		}
		pairs := make([]string, 0, len(repl)*2)
		for _, k := range []string{"%a", "%d", "%f", "%h", "%n", "%p", "%v", "%l", "%r", "%s", "%u"} {
			pairs = append(pairs, k, repl[k])
		}
		line = strings.NewReplacer(pairs...).Replace(line)
		fmt.Fprintln(os.Stdout, line)
	}
	for _, p := range plan.Remove {
		line := strings.NewReplacer(
			"%n", p.Name,
			"%v", p.Version,
			"%r", "local",
			"%l", p.Name+"-"+p.Version,
			"%s", "", "%a", "", "%d", "", "%p", "", "%u", "", "%f", "", "%h", "",
		).Replace(format)
		fmt.Fprintln(os.Stdout, line)
	}
}

func (a *App) pkgLocation(p *deb.PlannedPkg, paths map[string]string) string {
	if paths != nil {
		if path, ok := paths[p.Name]; ok {
			return path
		}
	}
	if p.URL != "" {
		return p.URL
	}
	if p.DebPath != "" {
		return p.DebPath
	}
	if p.Sync != nil && p.Sync.Source != nil && p.Sync.Filename != "" {
		return p.Sync.Source.PackageURL(p.Sync.Filename)
	}
	if p.Sync != nil && p.Sync.Filename != "" {
		return p.Sync.Filename
	}
	return p.Name + "-" + p.NewVersion
}

func (a *App) ignored(name string) bool {
	for _, pat := range append(append([]string{}, a.cfg.IgnorePkg...), a.conf.IgnorePkg...) {
		if ok, err := filepath.Match(pat, name); err == nil && ok {
			return true
		}
	}
	return false
}

func (a *App) ignoredGroup(p *alpm.Pkg) bool {
	groups := append(append([]string{}, a.cfg.IgnoreGroup...), a.conf.IgnoreGroup...)
	for _, g := range p.Groups {
		for _, pat := range groups {
			if ok, err := filepath.Match(pat, g); err == nil && ok {
				return true
			}
		}
	}
	return false
}

func (a *App) hasIgnore() bool {
	return len(a.cfg.IgnorePkg)+len(a.conf.IgnorePkg)+len(a.cfg.IgnoreGroup)+len(a.conf.IgnoreGroup) > 0
}

// confirmRemovals asks pacman's conflict and replacement questions for
// packages the resolver removes without being asked.
func (a *App) confirmRemovals(plan *deb.Plan) bool {
	if plan.Removals == nil || a.cfg.Print || a.cfg.DownloadOnly {
		return true
	}
	for _, p := range plan.Remove {
		info := plan.Removals[p.Name]
		switch info.Reason {
		case "conflict":
			if !render.Question(fmt.Sprintf("%s and %s are in conflict. Remove %s?",
				info.Other, p.Name, p.Name), false, a.cfg.NoConfirm) {
				return false
			}
		case "replace":
			target := info.Other
			if info.Repo != "" {
				target = info.Repo + "/" + target
			}
			if !render.Question(fmt.Sprintf("Replace %s with %s?", p.Name, target), true, a.cfg.NoConfirm) {
				return false
			}
		default:
			Errorf("cannot remove %s: the resolver needs it for dependency resolution", p.Name)
			return false
		}
	}
	return true
}

// enrichPlan fills in the repository URL and display metadata that libapt's
// cache does not expose through its public iterators.
func enrichPlan(sync *deb.Sync, db *deb.DB, plan *deb.Plan) {
	if sync == nil || plan == nil {
		return
	}
	for _, p := range plan.Install {
		if db != nil && p.OldVersion != "" {
			if local := db.Get(p.Name); local != nil {
				p.Local = local
			}
		}
		if p.DebPath != "" {
			continue
		}
		sp := sync.Find(p.Name, p.NewVersion)
		if sp == nil {
			continue
		}
		if sp.Repo != "" {
			p.Repo = sp.Repo
		}
		if p.Sync != nil {
			p.Sync.Repo = sp.Repo
			p.Sync.Filename = sp.Filename
			p.Sync.Source = sp.Source
			if sp.DownloadSize > 0 {
				p.Sync.DownloadSize = sp.DownloadSize
			}
			if sp.InstalledSize > 0 {
				p.Sync.InstalledSize = sp.InstalledSize
			}
		}
		if sp.Source != nil && sp.Filename != "" {
			p.URL = sp.Source.PackageURL(sp.Filename)
		}
	}
}
