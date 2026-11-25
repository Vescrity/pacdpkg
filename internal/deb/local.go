package deb

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"pacdpkg/internal/alpm"
)

// DB is the local dpkg database plus the bits of apt state we need.
type DB struct {
	Pkgs          []*alpm.Pkg
	byName        map[string]*alpm.Pkg
	provided      map[string][]*alpm.Pkg
	manual        map[string]bool
	held          map[string]bool
	reasonsLoaded bool
	reasonOK      bool
}

// ExtraArgs are injected into every apt command, mirroring pacman's
// global options (--cachedir, --arch).
var ExtraArgs []string

func Run(name string, args ...string) error {
	if len(ExtraArgs) > 0 && strings.HasPrefix(filepath.Base(name), "apt") {
		args = append(append([]string{}, ExtraArgs...), args...)
	}
	cmd := exec.Command(name, args...)
	var errb bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		if errb.Len() > 0 {
			return fmt.Errorf("%s", strings.TrimSpace(errb.String()))
		}
		return err
	}
	return nil
}

// RunStream runs a command attached to the user's terminal, for package
// transactions where apt/dpkg must be able to ask questions and show
// progress.
func RunStream(name string, args ...string) error {
	if len(ExtraArgs) > 0 && strings.HasPrefix(filepath.Base(name), "apt") {
		args = append(append([]string{}, ExtraArgs...), args...)
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Open reads dpkg's own databases directly; no dpkg/apt process is spawned.
func Open() (*DB, error) {
	db := &DB{
		byName:   map[string]*alpm.Pkg{},
		provided: map[string][]*alpm.Pkg{},
		manual:   map[string]bool{},
		held:     map[string]bool{},
	}

	pkgs, err := loadLocal()
	if err != nil {
		return nil, err
	}
	for _, p := range pkgs {
		if _, exists := db.byName[p.Name]; exists {
			p.Name = p.Binary
		}
		db.Pkgs = append(db.Pkgs, p)
		db.byName[p.Name] = p
	}
	sort.Slice(db.Pkgs, func(i, j int) bool { return db.Pkgs[i].Name < db.Pkgs[j].Name })

	for _, p := range db.Pkgs {
		for _, prov := range p.Provides {
			for _, alt := range prov.Alts {
				db.provided[alt.Name] = append(db.provided[alt.Name], p)
			}
		}
	}
	db.computeReverse()
	return db, nil
}

// EnsureReasons loads apt's auto-install state and dpkg's hold flag on demand.
func (db *DB) EnsureReasons() {
	if db.reasonsLoaded {
		return
	}
	db.reasonsLoaded = true
	if _, err := os.Stat(extStates); err != nil {
		return
	}
	auto := loadAutoInstalled()
	db.reasonOK = true
	for _, p := range db.Pkgs {
		p.Explicit = !auto[p.Name] && !auto[p.Binary]
		p.ReasonKnown = true
		if p.StateWant == "hold" {
			db.held[p.Name] = true
			db.held[p.Binary] = true
		}
	}
}

func (db *DB) Get(name string) *alpm.Pkg {
	name = strings.TrimPrefix(name, "local/")
	return db.byName[name]
}

// ReasonsKnown reports whether apt's install-reason state could be read.
func (db *DB) ReasonsKnown() bool {
	db.EnsureReasons()
	return db.reasonOK
}

func (db *DB) Held(name string) bool {
	db.EnsureReasons()
	return db.held[name]
}

func (db *DB) targets(dep alpm.Dep) []string {
	var names []string
	for _, alt := range dep.Alts {
		names = append(names, alt.Name)
		for _, p := range db.provided[alt.Name] {
			names = append(names, p.Name)
		}
	}
	return names
}

func (db *DB) computeReverse() {
	req := map[string][]string{}
	opt := map[string][]string{}
	for _, p := range db.Pkgs {
		for _, d := range p.Depends {
			for _, n := range db.targets(d) {
				req[n] = append(req[n], p.Name)
			}
		}
		for _, d := range p.OptDepends {
			for _, n := range db.targets(d) {
				opt[n] = append(opt[n], p.Name)
			}
		}
	}
	for _, p := range db.Pkgs {
		p.RequiredBy = sortedUnique(req[p.Name])
		p.OptionalFor = sortedUnique(opt[p.Name])
	}
}

func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Files returns the installed file list, directories with a trailing slash.
func (db *DB) Files(p *alpm.Pkg) ([]string, error) {
	if p.Files != nil {
		return p.Files, nil
	}
	raw, err := rawFileList(p)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(raw))
	for _, path := range raw {
		if st, err := os.Lstat(path); err == nil && st.IsDir() {
			path = strings.TrimSuffix(path, "/") + "/"
		}
		files = append(files, path)
	}
	sort.Strings(files)
	p.Files = files
	return files, nil
}

// Enrich fills the fields that are only needed for --info.
func (db *DB) Enrich(p *alpm.Pkg) {
	db.EnsureReasons()
	if p.Licenses == nil {
		p.Licenses = licenses(p.Name)
	}
	for i := range p.Conffiles {
		cf := &p.Conffiles[i]
		st, err := os.Lstat(cf.Path)
		if err != nil {
			cf.Missing = true
			continue
		}
		if st.IsDir() || cf.Sum == "" {
			continue
		}
		if sum, err := md5FileErr(cf.Path); err == nil {
			cf.Modified = sum != cf.Sum
		}
	}
}

func licenses(name string) []string {
	data, err := os.ReadFile(filepath.Join("/usr/share/doc", name, "copyright"))
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "License:"); ok {
			if v := strings.TrimSpace(rest); v != "" {
				out = append(out, v)
			}
		}
	}
	return sortedUnique(out)
}

func md5File(path string) string {
	sum, _ := md5FileErr(path)
	return sum
}

func md5FileErr(path string) (string, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Changelog returns the Debian changelog of an installed package.
func Changelog(name string) (string, bool) {
	for _, f := range []string{"changelog.Debian.gz", "changelog.gz"} {
		fh, err := os.Open(filepath.Join("/usr/share/doc", name, f))
		if err != nil {
			continue
		}
		defer fh.Close()
		zr, err := gzip.NewReader(fh)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(zr)
		if err != nil {
			continue
		}
		return string(data), true
	}
	return "", false
}
