package deb

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gzipBytes(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := io.WriteString(zw, s); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func indexEntries(t *testing.T, repo *ContentsRepo) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := repo.Scan(func(path string, pkgs []string) bool {
		out[path] = strings.Join(pkgs, ",")
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

const sampleContents = "" +
	"usr/bin/foo  utils/foo\n" +
	"usr/bin/foo  utils/extra\n" +
	"usr/bin/foo  utils/foo\n" +
	"usr/bin/bar  utils/foo,admin/bar\n" +
	"./usr/share/doc/foo/changelog.gz  doc/foo\n" +
	"/usr/bin/baz  utils/baz\n"

func TestWriteContentsIndex(t *testing.T) {
	var buf bytes.Buffer
	n, err := writeContentsIndex(strings.NewReader(sampleContents), &buf)
	if err != nil {
		t.Fatal(err)
	}
	want := "usr/bin/foo\tfoo,extra\n" +
		"usr/bin/bar\tfoo,bar\n" +
		"usr/share/doc/foo/changelog.gz\tfoo\n" +
		"usr/bin/baz\tbaz\n"
	if buf.String() != want {
		t.Fatalf("index mismatch:\ngot:\n%s\nwant:\n%s", buf.String(), want)
	}
	if n != 4 {
		t.Fatalf("entry count = %d, want 4", n)
	}
}

func TestWriteContentsIndexPlainText(t *testing.T) {
	var buf bytes.Buffer
	if _, err := writeContentsIndex(strings.NewReader(sampleContents), &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "usr/bin/bar\tfoo,bar\n") {
		t.Fatalf("unexpected index: %q", buf.String())
	}
}

func TestContentsURLs(t *testing.T) {
	cases := []struct {
		uri  string
		want []string
	}{
		{
			"https://host/debian/dists/trixie/main/binary-amd64/Packages",
			[]string{
				"https://host/debian/dists/trixie/main/Contents-amd64.gz",
				"https://host/debian/dists/trixie/Contents-amd64.gz",
			},
		},
		{
			"http://10.20.51.58:8000/./Packages",
			[]string{"http://10.20.51.58:8000/Contents-amd64.gz"},
		},
	}
	for _, c := range cases {
		got := contentsURLs(c.uri, "amd64")
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("contentsURLs(%q) = %v, want %v", c.uri, got, c.want)
		}
	}
}

func TestSourceContentsTargets(t *testing.T) {
	repos := []SourceRepo{
		{URI: "https://host/debian/", Suite: "trixie", Component: "main", Label: "trixie/main"},
		{URI: "http://10.20.51.58:8000/", Suite: "./", Label: "10.20.51.58:8000"},
	}
	if got := repos[0].ContentsURLs("amd64"); len(got) != 2 ||
		got[0] != "https://host/debian/dists/trixie/main/Contents-amd64.gz" ||
		got[1] != "https://host/debian/dists/trixie/Contents-amd64.gz" {
		t.Errorf("dists URLs = %v", got)
	}
	if got := repos[1].ContentsURLs("amd64"); len(got) != 1 ||
		got[0] != "http://10.20.51.58:8000/Contents-amd64.gz" {
		t.Errorf("flat URLs = %v", got)
	}
	if got := repos[0].PackageURL("pool/main/b/bash/bash.deb"); got != "https://host/debian/pool/main/b/bash/bash.deb" {
		t.Errorf("package URL = %q", got)
	}
	if got := repos[0].listPrefix("amd64"); got != "host_debian_dists_trixie_main_binary-amd64" {
		t.Errorf("list prefix = %q", got)
	}
}

func TestParseSourcesOneLine(t *testing.T) {
	entries := parseOneLine(`
deb https://host/debian trixie main contrib
deb [arch=amd64] http://localhost:8080/ ./
# comment
deb-src https://host/debian trixie main
`)
	if len(entries) != 2 {
		t.Fatalf("got %d entries", len(entries))
	}
	if entries[0].uri != "https://host/debian" || entries[0].suite != "trixie" ||
		strings.Join(entries[0].comps, ",") != "main,contrib" {
		t.Errorf("entry 0 = %+v", entries[0])
	}
	if !entries[1].flat {
		t.Errorf("entry 1 should be flat: %+v", entries[1])
	}
}

func TestParseSourcesDeb822(t *testing.T) {
	entries := parseDeb822(`
Types: deb
URIs: https://host/debian
Suites: trixie
Components: main
Enabled: yes

Types: deb-src
URIs: https://host/other
Suites: unstable
Components: main
`)
	if len(entries) != 1 {
		t.Fatalf("got %d entries", len(entries))
	}
	if entries[0].uri != "https://host/debian" || entries[0].suite != "trixie" {
		t.Errorf("entry = %+v", entries[0])
	}
}

func TestContentsArch(t *testing.T) {
	cases := map[string]string{
		"x86_64":  "amd64",
		"aarch64": "arm64",
		"amd64":   "amd64",
		"i686":    "i386",
	}
	for in, want := range cases {
		if got := ContentsArch(in); got != want {
			t.Errorf("ContentsArch(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestContentsFileNamePerLabel(t *testing.T) {
	a := contentsFileName("crimson/main")
	if a != "crimson_main.idx.gz" {
		t.Fatalf("unexpected name %q", a)
	}
	if !strings.HasSuffix(contentsFileName("unstable/crimson/main"), ".idx.gz") {
		t.Fatalf("unexpected name for slashed label")
	}
}

func TestRefreshContentsAndScan(t *testing.T) {
	payload := gzipBytes(t, sampleContents)
	var conditional, forced int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			conditional++
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if r.Header.Get("If-None-Match") == "" && r.Header.Get("If-Modified-Since") == "" {
			forced++
		}
		w.Header().Set("ETag", `"v1"`)
		w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	targets := []ContentsTarget{{
		Label: "test/main",
		URLs:  []string{srv.URL + "/dists/test/main/Contents-amd64.gz"},
	}}

	report, err := RefreshContents(dir, targets, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Updated != 1 || report.Failed != 0 {
		t.Fatalf("report = %+v", report)
	}

	db, err := OpenContents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(db.Repos) != 1 || db.Repos[0].Label != "test/main" {
		t.Fatalf("repos = %+v", db.Repos)
	}
	if st, err := os.Stat(db.Repos[0].File); err != nil {
		t.Fatal(err)
	} else if st.Mode().Perm() != 0o644 {
		t.Fatalf("index mode = %o, want 644", st.Mode().Perm())
	}
	entries := indexEntries(t, db.Repos[0])
	want := map[string]string{
		"usr/bin/foo":                    "foo,extra",
		"usr/bin/bar":                    "foo,bar",
		"usr/share/doc/foo/changelog.gz": "foo",
		"usr/bin/baz":                    "baz",
	}
	for path, pkgs := range want {
		if entries[path] != pkgs {
			t.Errorf("entry %q = %q, want %q", path, entries[path], pkgs)
		}
	}

	if report, err = RefreshContents(dir, targets, false); err != nil {
		t.Fatal(err)
	}
	if report.Unchanged != 1 || report.Updated != 0 {
		t.Fatalf("second refresh = %+v", report)
	}
	if conditional != 1 {
		t.Fatalf("server saw %d conditional requests", conditional)
	}

	if report, err = RefreshContents(dir, targets, true); err != nil {
		t.Fatal(err)
	}
	if report.Updated != 1 || report.Unchanged != 0 {
		t.Fatalf("forced refresh = %+v", report)
	}
	if forced != 2 {
		t.Fatalf("server saw %d unconditional requests, want 2", forced)
	}
}

func TestRefreshContentsFallbackURL(t *testing.T) {
	payload := gzipBytes(t, "usr/bin/foo  utils/foo\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/dists/test/main/") {
			http.NotFound(w, r)
			return
		}
		w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	targets := []ContentsTarget{{
		Label: "test/main",
		URLs: []string{
			srv.URL + "/dists/test/main/Contents-amd64.gz",
			srv.URL + "/dists/test/Contents-amd64.gz",
		},
	}}
	report, err := RefreshContents(dir, targets, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Updated != 1 || len(report.Warnings) != 0 {
		t.Fatalf("report = %+v", report)
	}
	db, err := OpenContents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(db.Repos) != 1 || !strings.HasSuffix(db.Repos[0].URL, "/dists/test/Contents-amd64.gz") {
		t.Fatalf("repos = %+v", db.Repos)
	}
}

func TestRefreshContentsMissing(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	dir := t.TempDir()
	targets := []ContentsTarget{{
		Label: "test/main",
		URLs: []string{
			srv.URL + "/one/Contents-amd64.gz",
			srv.URL + "/two/Contents-amd64.gz",
		},
	}}
	report, err := RefreshContents(dir, targets, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Updated != 0 || report.Failed != 1 {
		t.Fatalf("report = %+v", report)
	}
	if len(report.Warnings) != 2 {
		t.Fatalf("warnings = %v", report.Warnings)
	}
	if db, _ := OpenContents(dir); db != nil && len(db.Repos) != 0 {
		t.Fatalf("indexes written for missing repo: %+v", db.Repos)
	}
}

func TestRefreshContentsEmpty(t *testing.T) {
	payload := gzipBytes(t, "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()

	dir := t.TempDir()
	report, err := RefreshContents(dir, []ContentsTarget{{
		Label: "test/main",
		URLs:  []string{srv.URL + "/Contents-amd64.gz"},
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 1 || report.Updated != 0 {
		t.Fatalf("report = %+v", report)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*.idx.gz"))
	if len(matches) != 0 {
		t.Fatalf("unexpected index files: %v", matches)
	}
}

func TestRefreshContentsRejectsNonContents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>\n<body>404 not found</body>\n</html>\n"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	report, err := RefreshContents(dir, []ContentsTarget{{
		Label: "test/main",
		URLs:  []string{srv.URL + "/Contents-amd64.gz"},
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 1 || report.Updated != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestRefreshContentsFileURL(t *testing.T) {
	payload := gzipBytes(t, "usr/bin/local  utils/local\n")
	path := filepath.Join(t.TempDir(), "Contents-amd64.gz")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	report, err := RefreshContents(dir, []ContentsTarget{{
		Label: "local",
		URLs:  []string{"file:" + path},
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Updated != 1 {
		t.Fatalf("report = %+v", report)
	}
	db, err := OpenContents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(db.Repos) != 1 {
		t.Fatalf("repos = %+v", db.Repos)
	}
	if got := indexEntries(t, db.Repos[0])["usr/bin/local"]; got != "local" {
		t.Fatalf("entry = %q", got)
	}
}
