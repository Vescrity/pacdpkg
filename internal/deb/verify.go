package deb

import (
	"os"

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
	files, err := db.Files(p)
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

// OwnersOf lists the packages whose recorded file list contains path.
// The first call builds an index of every installed file.
func (db *DB) OwnersOf(path string) []*alpm.Pkg {
	path = trimDirSlash(path)
	if db.owners == nil {
		db.owners = map[string][]*alpm.Pkg{}
		for _, p := range db.Pkgs {
			files, err := db.Files(p)
			if err != nil {
				continue
			}
			for _, f := range files {
				key := trimDirSlash(f)
				db.owners[key] = append(db.owners[key], p)
			}
		}
	}
	return db.owners[path]
}

// DpkgVersion reports the installed dpkg version.
func DpkgVersion() string { return dpkgVersion() }
