package deb

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"pacdpkg/internal/alpm"
)

// SyncPkg is one package as it appears in a repository index (apt list).
type SyncPkg struct {
	Name        string
	Version     string
	Arch        string
	Repo        string
	Section     string
	Priority    string
	Maintainer  string
	Homepage    string
	Filename    string
	SHA256      string
	MD5         string
	Description string
	Source      *SourceRepo

	InstalledSize int64
	DownloadSize  int64

	Depends    []alpm.Dep
	PreDepends []alpm.Dep
	OptDepends []alpm.Dep
	Provides   []alpm.Dep
	Conflicts  []alpm.Dep
	Breaks     []alpm.Dep
	Replaces   []alpm.Dep
}

// Sync is the union of the apt package indexes.
type Sync struct {
	Pkgs     []*SyncPkg
	byName   map[string][]*SyncPkg
	Warnings []string
}

const listDir = "/var/lib/apt/lists"

// LoadSync parses the apt lists for the native architecture.
func LoadSync() (*Sync, error) {
	return LoadSyncArch(NativeArch())
}

// LoadSyncArch parses every apt list for one architecture.
func LoadSyncArch(arch string) (*Sync, error) {
	return LoadSyncArches([]string{arch})
}

// LoadSyncArches parses the apt lists for every requested architecture.
// Files may be plain text or compressed; compressed ones are decompressed
// in-process.
func LoadSyncArches(arches []string) (*Sync, error) {
	entries, err := os.ReadDir(listDir)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, arch := range arches {
		if arch != "" {
			want[arch] = true
		}
	}
	if len(want) == 0 {
		want[NativeArch()] = true
	}
	archList := make([]string, 0, len(want))
	for arch := range want {
		archList = append(archList, arch)
	}
	s := &Sync{byName: map[string][]*SyncPkg{}}
	repos := loadSourceReposArches(archList)
	seen := map[string]bool{}
	sort.SliceStable(entries, func(i, j int) bool {
		return listOrder(entries[i].Name(), repos) < listOrder(entries[j].Name(), repos)
	})
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !isPackagesList(name, want) {
			continue
		}
		base := trimCompression(name)
		src := repos[base]
		repo := ""
		if src != nil {
			repo = src.Label
		} else {
			repo = repoFromListName(base)
		}
		rc, err := openList(filepath.Join(listDir, name))
		if err != nil {
			s.Warnings = append(s.Warnings, fmt.Sprintf("could not read %s: %v", name, err))
			continue
		}
		err = parsePackagesList(rc, repo, src, s, seen)
		if cerr := rc.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			s.Warnings = append(s.Warnings, fmt.Sprintf("could not read %s: %v", name, err))
		}
	}
	if len(s.Pkgs) == 0 {
		return nil, fmt.Errorf("no usable package repositories configured.")
	}
	sort.Slice(s.Pkgs, func(i, j int) bool {
		if s.Pkgs[i].Repo != s.Pkgs[j].Repo {
			return s.Pkgs[i].Repo < s.Pkgs[j].Repo
		}
		return s.Pkgs[i].Name < s.Pkgs[j].Name
	})
	return s, nil
}

func isPackagesList(name string, arches map[string]bool) bool {
	base := name
	for _, suf := range []string{".lz4", ".gz", ".xz", ".zst", ".bz2"} {
		base = strings.TrimSuffix(base, suf)
	}
	if !strings.HasSuffix(base, "_Packages") {
		return false
	}
	base = strings.TrimSuffix(base, "_Packages")
	if i := strings.LastIndex(base, "_binary-"); i >= 0 {
		return arches[base[i+len("_binary-"):]]
	}
	return true
}

// repoFromListName turns an apt list filename into a pacman-style repo
// label: "<suite>/<component>" for dists repositories and the host for
// flat repositories.
func repoFromListName(name string) string {
	for _, suf := range []string{"_Packages.lz4", "_Packages.gz", "_Packages.xz", "_Packages"} {
		name = strings.TrimSuffix(name, suf)
	}
	if i := strings.Index(name, "_dists_"); i >= 0 {
		rest := name[i+len("_dists_"):]
		if j := strings.LastIndex(rest, "_binary-"); j >= 0 {
			rest = rest[:j]
		}
		if j := strings.LastIndex(rest, "_"); j >= 0 {
			return rest[:j] + "/" + rest[j+1:]
		}
		return rest
	}
	return strings.TrimSuffix(name, "_.")
}

type fileReader struct {
	io.Reader
	io.Closer
}

func openList(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(path, "_Packages") {
		return f, nil
	}
	r, err := decompress(filepath.Base(path), f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return fileReader{Reader: r, Closer: f}, nil
}

func parsePackagesList(r io.Reader, repo string, src *SourceRepo, s *Sync, seen map[string]bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)

	var p *SyncPkg
	var descDone bool
	flush := func() {
		if p == nil || p.Name == "" {
			return
		}
		p.Repo = repo
		p.Source = src
		key := repo + "|" + p.Name + "|" + p.Version
		if !seen[key] {
			seen[key] = true
			s.Pkgs = append(s.Pkgs, p)
			s.byName[p.Name] = append(s.byName[p.Name], p)
		}
		p = nil
	}

	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if p != nil && !descDone && strings.HasPrefix(line, " ") {
				descDone = true
			}
			continue
		}
		key, value, _ := strings.Cut(line, ":")
		value = strings.TrimSpace(value)
		if p == nil {
			p = &SyncPkg{}
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
			p.Description = value
			descDone = false
		case "Section":
			p.Section = value
		case "Priority":
			p.Priority = value
		case "Maintainer":
			p.Maintainer = value
		case "Homepage":
			p.Homepage = value
		case "Filename":
			p.Filename = value
		case "SHA256":
			p.SHA256 = value
		case "MD5sum":
			p.MD5 = value
		case "Size":
			p.DownloadSize = parseInt(value)
		case "Installed-Size":
			p.InstalledSize = parseInt(value) * 1024
		case "Depends":
			p.Depends = alpm.ParseDepList(value)
		case "Pre-Depends":
			p.PreDepends = append(p.PreDepends, alpm.ParseDepList(value)...)
		case "Recommends", "Suggests":
			p.OptDepends = append(p.OptDepends, alpm.ParseDepList(value)...)
		case "Provides":
			p.Provides = alpm.ParseDepList(value)
		case "Conflicts":
			p.Conflicts = append(p.Conflicts, alpm.ParseDepList(value)...)
		case "Breaks":
			p.Breaks = append(p.Breaks, alpm.ParseDepList(value)...)
		case "Replaces":
			p.Replaces = alpm.ParseDepList(value)
		}
	}
	flush()
	return sc.Err()
}

func parseInt(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// Lookup returns every indexed entry for a package name.
func (s *Sync) Lookup(name string) []*SyncPkg { return s.byName[name] }

// Find returns the entry matching name and version, if any.
func (s *Sync) Find(name, version string) *SyncPkg {
	for _, p := range s.byName[name] {
		if p.Version == version {
			return p
		}
	}
	return nil
}

// Repos returns the sorted repository labels in the index.
func (s *Sync) Repos() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range s.Pkgs {
		if !seen[p.Repo] {
			seen[p.Repo] = true
			out = append(out, p.Repo)
		}
	}
	sort.Strings(out)
	return out
}

func trimCompression(name string) string {
	for _, suf := range []string{".lz4", ".gz", ".xz", ".zst", ".bz2"} {
		if strings.HasSuffix(name, suf) {
			return strings.TrimSuffix(name, suf)
		}
	}
	return name
}

func listOrder(name string, repos map[string]*SourceRepo) int {
	if r := repos[trimCompression(name)]; r != nil {
		return r.Order
	}
	return 1 << 30
}
