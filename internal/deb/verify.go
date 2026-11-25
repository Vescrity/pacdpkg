package deb

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"pacdpkg/internal/alpm"
)

// VerifyResult counts the files of a package that are missing or whose
// contents no longer match dpkg's recorded checksums.
type VerifyResult struct {
	Total        int
	Missing      int
	Altered      int
	MissingPaths []string
	AlteredPaths []string
}

func (db *DB) Verify(p *alpm.Pkg, full bool) (VerifyResult, error) {
	files, err := rawFileList(p)
	if err != nil {
		return VerifyResult{}, err
	}
	res := VerifyResult{Total: len(files)}
	if !full {
		for _, f := range files {
			path := trimDirSlash(f)
			if _, err := os.Lstat(path); err != nil {
				res.Missing++
				res.MissingPaths = append(res.MissingPaths, path)
			}
		}
		return res, nil
	}

	diversions := loadDiversions()
	conffiles := map[string]string{}
	for _, cf := range p.Conffiles {
		conffiles[cf.Path] = cf.Sum
	}
	sums := md5Sums(p)
	for _, f := range files {
		path := trimDirSlash(f)
		if _, ok := conffiles[path]; ok {
			continue
		}
		if _, ok := diversions[path]; ok {
			continue
		}
		st, err := os.Lstat(path)
		if err != nil {
			res.Missing++
			res.MissingPaths = append(res.MissingPaths, path)
			continue
		}
		if st.IsDir() || sums == nil {
			continue
		}
		if !st.Mode().IsRegular() {
			res.Altered++
			res.AlteredPaths = append(res.AlteredPaths, path)
			continue
		}
		if want, ok := sums[path]; ok {
			sum, err := md5FileErr(path)
			if err != nil {
				continue
			}
			if sum != want {
				res.Altered++
				res.AlteredPaths = append(res.AlteredPaths, path)
			}
		}
	}
	for _, cf := range p.Conffiles {
		st, err := os.Lstat(cf.Path)
		if err != nil {
			res.Missing++
			res.MissingPaths = append(res.MissingPaths, cf.Path)
			continue
		}
		if st.IsDir() || cf.Sum == "" {
			continue
		}
		if !st.Mode().IsRegular() {
			res.Altered++
			res.AlteredPaths = append(res.AlteredPaths, cf.Path)
			continue
		}
		sum, err := md5FileErr(cf.Path)
		if err != nil {
			continue
		}
		if sum != cf.Sum {
			res.Altered++
			res.AlteredPaths = append(res.AlteredPaths, cf.Path)
		}
	}
	return res, nil
}

func trimDirSlash(path string) string {
	if len(path) > 1 && path[len(path)-1] == '/' {
		return path[:len(path)-1]
	}
	return path
}

// OwnersOfAll reports which installed packages own any of the given paths.
// Every path and its merged-/usr counterpart are matched in a single
// streaming pass over dpkg's file lists.
func (db *DB) OwnersOfAll(paths []string) map[string][]*alpm.Pkg {
	type query struct {
		path  string
		names map[string]*alpm.Pkg
	}
	want := map[string][]*query{}

	addCandidate := func(key, path string) {
		key = normalizeOwnerPath(key)
		if key == "" {
			return
		}
		for _, q := range want[key] {
			if q.path == path {
				return
			}
		}
		want[key] = append(want[key], &query{path: path, names: map[string]*alpm.Pkg{}})
	}
	for _, path := range paths {
		addCandidate(path, path)
		for _, twin := range usrMergeTwins(normalizeOwnerPath(path)) {
			addCandidate(twin, path)
		}
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	jobs := make(chan string)
	workers := runtime.NumCPU()
	if workers > 8 {
		workers = 8
	}
	if workers < 1 {
		workers = 1
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range jobs {
				pkg := db.pkgForListFile(name)
				if pkg == nil {
					continue
				}
				f, err := os.Open(filepath.Join(infoDir, name))
				if err != nil {
					continue
				}
				sc := bufio.NewScanner(f)
				sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
				for sc.Scan() {
					line := strings.TrimSpace(sc.Text())
					if line == "" || line == "/." {
						continue
					}
					if !strings.HasPrefix(line, "/") {
						line = "/" + line
					}
					queries := want[line]
					if len(queries) == 0 {
						continue
					}
					mu.Lock()
					for _, q := range queries {
						q.names[pkg.Name] = pkg
					}
					mu.Unlock()
				}
				f.Close()
			}
		}()
	}
	for _, name := range listFiles() {
		jobs <- name
	}
	close(jobs)
	wg.Wait()

	out := make(map[string][]*alpm.Pkg, len(paths))
	for _, path := range paths {
		seen := map[string]bool{}
		var owners []*alpm.Pkg
		for _, queries := range want {
			for _, q := range queries {
				if q.path != path {
					continue
				}
				for _, pkg := range q.names {
					if seen[pkg.Name] {
						continue
					}
					seen[pkg.Name] = true
					owners = append(owners, pkg)
				}
			}
		}
		sort.Slice(owners, func(i, j int) bool { return owners[i].Name < owners[j].Name })
		out[path] = owners
	}
	return out
}

func (db *DB) pkgForListFile(name string) *alpm.Pkg {
	name = strings.TrimSuffix(name, ".list")
	if p := db.byName[name]; p != nil {
		return p
	}
	if i := strings.IndexByte(name, ':'); i > 0 {
		return db.byName[name[:i]]
	}
	return nil
}

func normalizeOwnerPath(path string) string {
	path = trimDirSlash(path)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

// DpkgVersion reports the installed dpkg version.
func DpkgVersion() string { return dpkgVersion() }

// usrMergeTwins returns the counterpart of a path under Debian's merged-/usr
// layout. Packages may record /bin/foo while the file is reachable as
// /usr/bin/foo, or the other way round.
func usrMergeTwins(path string) []string {
	pairs := [][2]string{
		{"/bin/", "/usr/bin/"},
		{"/sbin/", "/usr/sbin/"},
		{"/lib/", "/usr/lib/"},
		{"/lib64/", "/usr/lib64/"},
	}
	var out []string
	for _, pair := range pairs {
		if rest, ok := strings.CutPrefix(path, pair[0]); ok {
			out = append(out, pair[1]+rest)
		} else if rest, ok := strings.CutPrefix(path, pair[1]); ok {
			out = append(out, pair[0]+rest)
		}
	}
	return out
}
