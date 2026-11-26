package deb

import "pacdpkg/internal/alpm"

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
