package deb

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Contents is Debian's file database: dists/<suite>/<component>/Contents-<arch>.gz.
// pacman keeps one files database per repository; we keep one index file per
// repository in a shared cache directory.

const contentsMarker = "#pacdpkg-files"

// contentsMeta is stored as the first line of every index file so that the
// cache directory is self-describing and needs no separate manifest.
type contentsMeta struct {
	Label        string `json:"label"`
	URL          string `json:"url"`
	Updated      int64  `json:"updated"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
}

// ContentsRepo is one repository's file index.
type ContentsRepo struct {
	Label        string
	File         string
	URL          string
	Updated      int64
	ETag         string
	LastModified string
}

// ContentsDB is every files index found in a cache directory.
type ContentsDB struct {
	Dir   string
	Repos []*ContentsRepo
}

// ContentsTarget is a repository to synchronize. Local indexes maintained by
// apt are preferred; URLs are fallbacks derived from the Release file.
type ContentsTarget struct {
	Label string
	Local []string
	URLs  []string
}

// ContentsReport summarizes one refresh run.
type ContentsReport struct {
	Updated   int
	Unchanged int
	Failed    int
	Warnings  []string
}

// Scan streams every entry of the index. It stops early when fn returns false.
func (r *ContentsRepo) Scan(fn func(path string, pkgs []string) bool) error {
	f, err := os.Open(r.File)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer zr.Close()
	br := bufio.NewReaderSize(zr, 1<<16)
	if _, err := br.ReadString('\n'); err != nil {
		return err
	}
	sc := bufio.NewScanner(br)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		tab := strings.IndexByte(line, '\t')
		if tab <= 0 || tab == len(line)-1 {
			continue
		}
		if !fn(line[:tab], strings.Split(line[tab+1:], ",")) {
			return nil
		}
	}
	return sc.Err()
}

// ContentsWriteDir returns the first writable cache directory.
func ContentsWriteDir() string {
	if dir := os.Getenv("PACDPKG_FILES_DIR"); dir != "" {
		return dir
	}
	for _, dir := range contentsDirCandidates() {
		if dirWritable(dir) {
			return dir
		}
	}
	return ""
}

// DefaultContentsDir returns the directory holding the files indexes. A
// writable system cache wins for refreshes, while queries prefer a directory
// that already has indexes (which may have been built by root).
func DefaultContentsDir() string {
	candidates := contentsDirCandidates()
	for _, dir := range candidates {
		if hasContentsIndex(dir) {
			return dir
		}
	}
	for _, dir := range candidates {
		if dirWritable(dir) {
			return dir
		}
	}
	return candidates[len(candidates)-1]
}

func contentsDirCandidates() []string {
	if dir := os.Getenv("PACDPKG_FILES_DIR"); dir != "" {
		return []string{dir}
	}
	var out []string
	out = append(out, "/var/cache/pacdpkg/files")
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		out = append(out, filepath.Join(xdg, "pacdpkg", "files"))
	} else if home := os.Getenv("HOME"); home != "" {
		out = append(out, filepath.Join(home, ".cache", "pacdpkg", "files"))
	}
	return out
}

func hasContentsIndex(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".idx.gz") {
			return true
		}
	}
	return false
}

func dirWritable(dir string) bool {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// ContentsArch maps pacman-style architecture names to Debian ones.
func ContentsArch(configured string) string {
	if configured == "auto" {
		return ""
	}
	return DebArch(configured)
}

// OpenContents reads the headers of every index file in dir.
func OpenContents(dir string) (*ContentsDB, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	db := &ContentsDB{Dir: dir}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".idx.gz") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		meta, err := readContentsMeta(f)
		f.Close()
		if err != nil || meta.Label == "" {
			continue
		}
		db.Repos = append(db.Repos, &ContentsRepo{
			Label:        meta.Label,
			File:         path,
			URL:          meta.URL,
			Updated:      meta.Updated,
			ETag:         meta.ETag,
			LastModified: meta.LastModified,
		})
	}
	sort.Slice(db.Repos, func(i, j int) bool {
		if db.Repos[i].Label != db.Repos[j].Label {
			return db.Repos[i].Label < db.Repos[j].Label
		}
		return db.Repos[i].URL < db.Repos[j].URL
	})
	return db, nil
}

func readContentsMeta(r io.Reader) (*contentsMeta, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	line, err := bufio.NewReader(zr).ReadString('\n')
	if err != nil && line == "" {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	rest, ok := strings.CutPrefix(line, contentsMarker+" ")
	if !ok {
		return nil, errors.New("not a pacdpkg files index")
	}
	var meta contentsMeta
	if err := json.Unmarshal([]byte(rest), &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// ContentsTargets derives the Contents index location of every configured
// repository from apt's sources, without asking apt.
func ContentsTargets(arch string) ([]ContentsTarget, error) {
	repos := loadSourceRepos(arch)
	names := make([]string, 0, len(repos))
	for name := range repos {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return repos[names[i]].Order < repos[names[j]].Order
	})

	type group struct {
		local []string
		urls  []string
	}
	groups := map[string]*group{}
	var labels []string
	for _, name := range names {
		repo := repos[name]
		g := groups[repo.Label]
		if g == nil {
			g = &group{}
			groups[repo.Label] = g
			labels = append(labels, repo.Label)
		}
		g.local = append(g.local, localContentsFiles(repo, arch)...)
		urls, releaseKnown := releaseContentsURLs(name, repo, arch)
		if len(urls) == 0 && !releaseKnown {
			urls = conventionalContentsURLs(repo, arch)
		}
		g.urls = append(g.urls, urls...)
	}
	sort.Strings(labels)

	var out []ContentsTarget
	for _, label := range labels {
		g := groups[label]
		out = append(out, ContentsTarget{Label: label, Local: dedupeStrings(g.local), URLs: dedupeStrings(g.urls)})
	}
	return out, nil
}

// localContentsFiles lists the Contents indexes apt already maintains for a
// repository (50apt-file.conf enables them).
func localContentsFiles(r *SourceRepo, arch string) []string {
	prefix := strings.TrimSuffix(r.listPrefix(arch), "_binary-"+arch) + "_Contents-" + arch
	var out []string
	for _, ext := range []string{".lz4", ".gz", ".xz", ".zst", ".bz2", ""} {
		path := filepath.Join(listDir, prefix+ext)
		if _, err := os.Stat(path); err == nil {
			out = append(out, path)
		}
	}
	return out
}

// releaseContentsURLs derives Contents URLs from the repository's Release
// file, honouring the compression the archive actually publishes.
func releaseContentsURLs(listName string, r *SourceRepo, arch string) ([]string, bool) {
	base := strings.TrimSuffix(listName, "_Packages")
	if !r.flat() {
		base = strings.TrimSuffix(base, "_"+r.Component+"_binary-"+arch)
	}
	var text string
	for _, suffix := range []string{"_InRelease", "_Release"} {
		if data, err := os.ReadFile(filepath.Join(listDir, base+suffix)); err == nil {
			text = string(data)
			break
		}
	}
	if text == "" {
		return nil, false
	}
	seen := map[string]bool{}
	var paths []string
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		path := fields[len(fields)-1]
		if !strings.Contains(path, "Contents-"+arch) {
			continue
		}
		if r.Component != "" && !strings.HasPrefix(path, r.Component+"/") &&
			!strings.Contains(path, "/"+r.Component+"/") {
			continue
		}
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	sort.Slice(paths, func(i, j int) bool {
		return compressionRank(paths[i]) < compressionRank(paths[j])
	})
	root := strings.TrimSuffix(r.URI, "/")
	var out []string
	for _, path := range paths {
		if r.flat() {
			out = append(out, root+"/"+path)
		} else {
			out = append(out, root+"/dists/"+r.Suite+"/"+path)
		}
	}
	return out, true
}

func compressionRank(path string) int {
	switch {
	case strings.HasSuffix(path, ".bz2"):
		return 1
	case strings.HasSuffix(path, ".xz"):
		return 2
	case strings.HasSuffix(path, ".zst"):
		return 3
	case strings.HasSuffix(path, ".gz"):
		return 4
	case strings.HasSuffix(path, ".lz4"):
		return 5
	}
	return 6
}

// conventionalContentsURLs probes the usual names when no Release file has
// been downloaded yet.
func conventionalContentsURLs(r *SourceRepo, arch string) []string {
	base := strings.TrimSuffix(r.URI, "/")
	var out []string
	for _, ext := range []string{".bz2", ".xz", ".zst", ".gz", ".lz4", ""} {
		if r.flat() {
			out = append(out, base+"/Contents-"+arch+ext)
			continue
		}
		out = append(out, base+"/dists/"+r.Suite+"/"+r.Component+"/Contents-"+arch+ext)
		out = append(out, base+"/dists/"+r.Suite+"/Contents-"+arch+ext)
	}
	return out
}

func contentsURLs(uri, arch string) []string {
	slash := strings.LastIndexByte(uri, '/')
	if slash < 0 {
		return nil
	}
	dir := uri[:slash+1]
	file := "Contents-" + arch + ".gz"

	if !strings.HasSuffix(dir, "/binary-"+arch+"/") {
		if strings.HasSuffix(dir, "/./") {
			dir = strings.TrimSuffix(dir, "./")
		}
		return []string{dir + file}
	}

	component := strings.TrimSuffix(dir, "/binary-"+arch+"/")
	urls := []string{component + "/" + file}
	if i := strings.LastIndex(component, "/dists/"); i >= 0 {
		rest := component[i+len("/dists/"):]
		if j := strings.IndexByte(rest, '/'); j > 0 {
			suite := component[:i] + "/dists/" + rest[:j]
			if u := suite + "/" + file; u != urls[0] {
				urls = append(urls, u)
			}
		}
	}
	return urls
}

// RefreshContents downloads every target and rewrites its index. With force
// the cached validators are ignored (-Fyy); otherwise unchanged indexes are
// kept (-Fy).
func RefreshContents(dir string, targets []ContentsTarget, force bool) (*ContentsReport, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	old := map[string]*ContentsRepo{}
	if db, err := OpenContents(dir); err == nil {
		for _, r := range db.Repos {
			old[r.URL] = r
		}
	}

	report := &ContentsReport{}
	for _, t := range targets {
		status, warnings := refreshContentsTarget(dir, t, force, old)
		switch status {
		case 1:
			report.Updated++
		case 0:
			report.Unchanged++
		default:
			report.Failed++
			report.Warnings = append(report.Warnings, warnings...)
		}
	}
	return report, nil
}

func refreshContentsTarget(dir string, t ContentsTarget, force bool, old map[string]*ContentsRepo) (int, []string) {
	var warnings []string
	if len(t.Local) == 0 && len(t.URLs) == 0 {
		return -1, []string{fmt.Sprintf("repository '%s' publishes no file index", t.Label)}
	}
	for _, path := range t.Local {
		status, err := fetchLocalContents(dir, t.Label, path)
		if err == nil {
			return status, nil
		}
		warnings = append(warnings, fmt.Sprintf("failed reading '%s': %v", path, err))
	}
	for _, url := range t.URLs {
		var prev *ContentsRepo
		if !force {
			prev = old[url]
		}
		status, err := fetchContentsIndex(dir, t.Label, url, prev)
		if err == nil {
			return status, nil
		}
		warnings = append(warnings, fmt.Sprintf(
			"failed retrieving file '%s' from %s: %v", filepath.Base(url), url, err))
	}
	return -1, warnings
}

func fetchLocalContents(dir, label, path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return -1, err
	}
	defer f.Close()
	reader, err := decompress(filepath.Base(path), f)
	if err != nil {
		return -1, err
	}
	meta := &contentsMeta{Label: label, URL: "file:" + path, Updated: time.Now().Unix()}
	return writeContentsIndexFile(dir, label, meta, reader)
}

func fetchContentsIndex(dir, label, url string, old *ContentsRepo) (int, error) {
	body, meta, notModified, err := openContentsStream(url, old)
	if err != nil {
		return -1, err
	}
	if notModified {
		return 0, nil
	}
	defer body.Close()

	reader, err := decompress(filepath.Base(url), body)
	if err != nil {
		return -1, err
	}
	meta.Label = label
	meta.URL = url
	meta.Updated = time.Now().Unix()
	return writeContentsIndexFile(dir, label, meta, reader)
}

func writeContentsIndexFile(dir, label string, meta *contentsMeta, body io.Reader) (int, error) {
	tmp, err := os.CreateTemp(dir, ".tmp-*.idx.gz")
	if err != nil {
		return -1, err
	}
	tmpName := tmp.Name()
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return -1, err
	}
	fail := func(err error) (int, error) {
		tmp.Close()
		os.Remove(tmpName)
		return -1, err
	}

	zw := gzip.NewWriter(tmp)
	bw := bufio.NewWriterSize(zw, 1<<20)
	header, _ := json.Marshal(meta)
	if _, err := fmt.Fprintf(bw, "%s %s\n", contentsMarker, header); err != nil {
		return fail(err)
	}
	n, err := writeContentsIndex(body, bw)
	if err != nil {
		return fail(err)
	}
	if n == 0 {
		return fail(errors.New("no entries in contents file"))
	}
	if err := bw.Flush(); err != nil {
		return fail(err)
	}
	if err := zw.Close(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		return fail(err)
	}
	if err := os.Rename(tmpName, filepath.Join(dir, contentsFileName(label))); err != nil {
		os.Remove(tmpName)
		return -1, err
	}
	return 1, nil
}

var contentsHTTPClient = &http.Client{Timeout: 20 * time.Minute}

func openContentsStream(url string, old *ContentsRepo) (io.ReadCloser, *contentsMeta, bool, error) {
	if strings.HasPrefix(url, "file:") {
		path := strings.TrimPrefix(url, "file://")
		if path == url {
			path = strings.TrimPrefix(url, "file:")
		}
		f, err := os.Open(path)
		if err != nil {
			return nil, nil, false, err
		}
		return f, &contentsMeta{}, false, nil
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, false, err
	}
	req.Header.Set("User-Agent", "pacdpkg/0.1")
	if old != nil {
		if old.ETag != "" {
			req.Header.Set("If-None-Match", old.ETag)
		}
		if old.LastModified != "" {
			req.Header.Set("If-Modified-Since", old.LastModified)
		}
	}
	resp, err := contentsHTTPClient.Do(req)
	if err != nil {
		return nil, nil, false, err
	}
	switch resp.StatusCode {
	case http.StatusNotModified:
		resp.Body.Close()
		return nil, nil, true, nil
	case http.StatusOK:
		meta := &contentsMeta{
			ETag:         resp.Header.Get("ETag"),
			LastModified: resp.Header.Get("Last-Modified"),
		}
		return resp.Body, meta, false, nil
	default:
		resp.Body.Close()
		return nil, nil, false, fmt.Errorf("The requested URL returned error: %d %s",
			resp.StatusCode, http.StatusText(resp.StatusCode))
	}
}

func contentsFileName(label string) string {
	return sanitizeContentsName(label) + ".idx.gz"
}

func sanitizeContentsName(label string) string {
	var b strings.Builder
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "repo"
	}
	return b.String()
}

// writeContentsIndex turns a Contents stream into the compact tab-separated
// index. Consecutive entries for the same path are merged.
func writeContentsIndex(r io.Reader, w io.Writer) (int, error) {
	br := bufio.NewReaderSize(r, 1<<16)
	var src io.Reader = br
	if magic, _ := br.Peek(2); len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return 0, err
		}
		defer zr.Close()
		src = zr
	}

	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)

	var lastPath string
	var lastPkgs []string
	count := 0
	flush := func() error {
		if lastPath == "" {
			return nil
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\n", lastPath, strings.Join(lastPkgs, ",")); err != nil {
			return err
		}
		count++
		return nil
	}

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == '<' {
			continue
		}
		sep := strings.IndexAny(line, " \t")
		if sep <= 0 || sep == len(line)-1 {
			continue
		}
		path := normalizeContentsPath(line[:sep])
		if path == "" {
			continue
		}
		pkgs := contentsPkgList(line[sep+1:])
		if len(pkgs) == 0 {
			continue
		}
		if path == lastPath {
			lastPkgs = mergePkgNames(lastPkgs, pkgs)
			continue
		}
		if err := flush(); err != nil {
			return count, err
		}
		lastPath, lastPkgs = path, pkgs
	}
	if err := sc.Err(); err != nil {
		return count, err
	}
	if err := flush(); err != nil {
		return count, err
	}
	return count, nil
}

func normalizeContentsPath(path string) string {
	path = strings.TrimPrefix(path, "./")
	path = strings.TrimLeft(path, "/")
	return path
}

func contentsPkgList(field string) []string {
	var out []string
	for _, item := range strings.Split(field, ",") {
		item = strings.TrimSpace(item)
		if i := strings.LastIndexByte(item, '/'); i >= 0 {
			item = item[i+1:]
		}
		if validPkgName(item) {
			out = append(out, item)
		}
	}
	return out
}

func validPkgName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '+', r == '-', r == '.', r == '_':
		default:
			return false
		}
	}
	return true
}

func mergePkgNames(dst, src []string) []string {
	for _, name := range src {
		found := false
		for _, have := range dst {
			if have == name {
				found = true
				break
			}
		}
		if !found {
			dst = append(dst, name)
		}
	}
	return dst
}
