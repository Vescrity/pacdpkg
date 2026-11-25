package app

import (
	"pacdpkg/internal/aptplan"
	"pacdpkg/internal/deb"
)

// effectiveArch resolves the architecture used for indexes and Contents.
// CLI/config --arch wins, then libapt's configured native architecture, then
// dpkg's arch file, then the Go runtime.
func (a *App) effectiveArch() string {
	if a.cfg.Arch != "" && a.cfg.Arch != "auto" {
		return deb.DebArch(a.cfg.Arch)
	}
	if aptplan.Available() {
		if arch, err := aptplan.NativeArch(); err == nil && arch != "" {
			return deb.DebArch(arch)
		}
	}
	return deb.NativeArch()
}

func (a *App) effectiveArchitectures() []string {
	if len(a.cfg.ArchList) == 0 {
		return nil
	}
	out := make([]string, 0, len(a.cfg.ArchList))
	for _, arch := range a.cfg.ArchList {
		out = append(out, deb.DebArch(arch))
	}
	return out
}
