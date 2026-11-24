package deb

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"pacdpkg/internal/alpm"
)

// PackageFile is a .deb archive queried outside any database.
type PackageFile struct {
	Pkg            *alpm.Pkg
	Files          []string
	CompressedSize int64
}

// OpenPackageFile reads control metadata and the file list of a .deb
// directly from the ar/tar archive.
func OpenPackageFile(path string) (*PackageFile, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("could not find or read package")
	}
	ar, err := openDeb(path)
	if err != nil {
		return nil, err
	}
	p, err := parseDebControl(ar.control["control"])
	if err != nil {
		return nil, err
	}
	p.Validated = "None"
	p.DownloadSize = st.Size()
	for _, script := range []string{"preinst", "postinst", "prerm", "postrm", "config"} {
		if _, ok := ar.control[script]; ok {
			p.HasScriptlet = true
			break
		}
	}
	files := append([]string(nil), ar.files...)
	sort.Strings(files)
	return &PackageFile{Pkg: p, Files: files, CompressedSize: st.Size()}, nil
}

func parseControl(out string) (*alpm.Pkg, error) {
	p := &alpm.Pkg{}
	var descDone bool
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			descDone = true
			continue
		}
		key, value, _ := strings.Cut(line, ":")
		value = strings.TrimSpace(value)
		if key == "Description" {
			descDone = false
		}
		switch key {
		case "Package":
			p.Name = value
		case "Version":
			p.Version = value
		case "Architecture":
			p.Arch = value
		case "Description":
			if !descDone {
				p.Description = value
			}
		case "Homepage":
			p.URL = value
		case "Maintainer":
			p.Packager = value
		case "Section":
			p.Groups = nonEmptyList(value)
		case "License":
			p.Licenses = nonEmptyList(value)
		case "Installed-Size":
			p.InstalledSize = parseInt(value) * 1024
		case "Depends":
			p.Depends = alpm.ParseDepList(value)
		case "Pre-Depends":
			p.Depends = append(p.Depends, alpm.ParseDepList(value)...)
		case "Recommends", "Suggests":
			p.OptDepends = append(p.OptDepends, alpm.ParseDepList(value)...)
		case "Provides":
			p.Provides = alpm.ParseDepList(value)
		case "Conflicts", "Breaks":
			p.Conflicts = append(p.Conflicts, alpm.ParseDepList(value)...)
		case "Replaces":
			p.Replaces = alpm.ParseDepList(value)
		}
		seen[key] = true
	}
	if p.Name == "" {
		return nil, fmt.Errorf("could not find or read package")
	}
	return p, nil
}

func nonEmptyList(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}
