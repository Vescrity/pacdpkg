package app

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"pacdpkg/internal/cli"
	"pacdpkg/internal/deb"
)

const filesFixture = "" +
	"usr/bin/foo  utils/foo\n" +
	"usr/bin/bar  utils/foo,utils/bar\n" +
	"usr/bin/foobar  utils/foobar\n" +
	"usr/share/doc/foo/readme  doc/foo\n" +
	"usr/lib/libfoo.so  libs/libfoo\n"

func filesSyncFixture() *deb.Sync {
	return &deb.Sync{Pkgs: []*deb.SyncPkg{
		{Name: "foo", Version: "1.0-1", Repo: "test/main"},
		{Name: "bar", Version: "2.0-1", Repo: "test/main"},
		{Name: "foobar", Version: "0.5-1", Repo: "test/main"},
		{Name: "libfoo", Version: "3.0-1", Repo: "test/main"},
	}}
}

// filesTestEnv points the files index at a scratch directory and stubs the
// apt/dpkg lookups.
func filesTestEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PACDPKG_FILES_DIR", dir)

	oldSync, oldDB := loadFilesSync, openLocalDB
	loadFilesSync = func() (*deb.Sync, error) { return filesSyncFixture(), nil }
	openLocalDB = func() (*deb.DB, error) { return nil, errors.New("no dpkg database") }
	t.Cleanup(func() {
		loadFilesSync, openLocalDB = oldSync, oldDB
	})
	return dir
}

func addFilesRepo(t *testing.T, dir string) {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := io.WriteString(zw, filesFixture); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	report, err := deb.RefreshContents(dir, []deb.ContentsTarget{{
		Label: "test/main",
		URLs:  []string{srv.URL + "/Contents-amd64.gz"},
	}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Updated != 1 {
		t.Fatalf("fixture refresh = %+v", report)
	}
}

func runFiles(t *testing.T, cfg *cli.Config, targets ...string) (string, string, int) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	ret := (&App{cfg: cfg, targets: targets}).files()
	wOut.Close()
	wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr

	out, _ := io.ReadAll(rOut)
	errOut, _ := io.ReadAll(rErr)
	return string(out), string(errOut), ret
}

func TestFilesNoDatabase(t *testing.T) {
	filesTestEnv(t)

	out, errOut, ret := runFiles(t, &cli.Config{}, "foo")
	if ret != 1 {
		t.Fatalf("ret = %d, want 1", ret)
	}
	if out != "" {
		t.Fatalf("stdout = %q", out)
	}
	want := "error: you do not have a files database. Try running 'pacman -Fy'\n"
	if errOut != want {
		t.Fatalf("stderr = %q, want %q", errOut, want)
	}
}

func TestFilesNoTargets(t *testing.T) {
	dir := filesTestEnv(t)
	addFilesRepo(t, dir)

	_, errOut, ret := runFiles(t, &cli.Config{})
	if ret != 1 {
		t.Fatalf("ret = %d, want 1", ret)
	}
	if errOut != "error: no targets specified (use -h for help)\n" {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestFilesSearchQuiet(t *testing.T) {
	dir := filesTestEnv(t)
	addFilesRepo(t, dir)

	out, errOut, ret := runFiles(t, &cli.Config{Quiet: true}, "foo")
	if ret != 0 || errOut != "" {
		t.Fatalf("ret = %d, stderr = %q", ret, errOut)
	}
	if out != "test/main/foo\n" {
		t.Fatalf("stdout = %q", out)
	}
}

func TestFilesSearchDefault(t *testing.T) {
	dir := filesTestEnv(t)
	addFilesRepo(t, dir)

	out, errOut, ret := runFiles(t, &cli.Config{}, "foo")
	if ret != 0 || errOut != "" {
		t.Fatalf("ret = %d, stderr = %q", ret, errOut)
	}
	want := "test/main/foo 1.0-1\n    usr/bin/foo\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

func TestFilesExactPath(t *testing.T) {
	dir := filesTestEnv(t)
	addFilesRepo(t, dir)

	out, errOut, ret := runFiles(t, &cli.Config{}, "/usr/bin/foo")
	if ret != 0 || errOut != "" {
		t.Fatalf("ret = %d, stderr = %q", ret, errOut)
	}
	want := "usr/bin/foo is owned by test/main/foo 1.0-1\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

func TestFilesRegex(t *testing.T) {
	dir := filesTestEnv(t)
	addFilesRepo(t, dir)

	out, errOut, ret := runFiles(t, &cli.Config{FileRegex: true}, "usr/lib/lib.*\\.so$")
	if ret != 0 || errOut != "" {
		t.Fatalf("ret = %d, stderr = %q", ret, errOut)
	}
	want := "usr/lib/libfoo.so is owned by test/main/libfoo 3.0-1\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

func TestFilesInvalidRegex(t *testing.T) {
	dir := filesTestEnv(t)
	addFilesRepo(t, dir)

	_, errOut, ret := runFiles(t, &cli.Config{FileRegex: true}, "[")
	if ret != 1 {
		t.Fatalf("ret = %d, want 1", ret)
	}
	if errOut != "error: invalid regular expression '['\n" {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestFilesInvalidRegexMultiple(t *testing.T) {
	dir := filesTestEnv(t)
	addFilesRepo(t, dir)

	out, errOut, ret := runFiles(t, &cli.Config{FileRegex: true}, "[", "(")
	if ret != 1 {
		t.Fatalf("ret = %d, want 1", ret)
	}
	if out != "" {
		t.Fatalf("stdout = %q", out)
	}
	want := "error: invalid regular expression '['\nerror: invalid regular expression '('\n"
	if errOut != want {
		t.Fatalf("stderr = %q, want %q", errOut, want)
	}
}

func TestFilesRegexBasename(t *testing.T) {
	dir := filesTestEnv(t)
	addFilesRepo(t, dir)

	out, errOut, ret := runFiles(t, &cli.Config{FileRegex: true}, "^foo$")
	if ret != 0 || errOut != "" {
		t.Fatalf("ret = %d, stderr = %q", ret, errOut)
	}
	want := "test/main/foo 1.0-1\n    usr/bin/foo\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

func TestFilesSearchMachineReadable(t *testing.T) {
	dir := filesTestEnv(t)
	addFilesRepo(t, dir)

	out, errOut, ret := runFiles(t, &cli.Config{MachineReadable: true}, "foo")
	if ret != 0 || errOut != "" {
		t.Fatalf("ret = %d, stderr = %q", ret, errOut)
	}
	want := "test/main\x00foo\x001.0-1\x00usr/bin/foo\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

func TestFilesSearchMachineReadableQuiet(t *testing.T) {
	dir := filesTestEnv(t)
	addFilesRepo(t, dir)

	out, errOut, ret := runFiles(t, &cli.Config{MachineReadable: true, Quiet: true}, "foo")
	if ret != 0 || errOut != "" {
		t.Fatalf("ret = %d, stderr = %q", ret, errOut)
	}
	want := "test/main\x00foo\x001.0-1\x00usr/bin/foo\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

func TestFilesSearchNoMatch(t *testing.T) {
	dir := filesTestEnv(t)
	addFilesRepo(t, dir)

	out, _, ret := runFiles(t, &cli.Config{Quiet: true}, "missing")
	if ret != 1 {
		t.Fatalf("ret = %d, want 1", ret)
	}
	if out != "" {
		t.Fatalf("stdout = %q", out)
	}
}

func TestFilesList(t *testing.T) {
	dir := filesTestEnv(t)
	addFilesRepo(t, dir)

	out, errOut, ret := runFiles(t, &cli.Config{List: true}, "foo")
	if ret != 0 || errOut != "" {
		t.Fatalf("ret = %d, stderr = %q", ret, errOut)
	}
	want := "foo usr/bin/foo\n" +
		"foo usr/bin/bar\n" +
		"foo usr/share/doc/foo/readme\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

func TestFilesListQuiet(t *testing.T) {
	dir := filesTestEnv(t)
	addFilesRepo(t, dir)

	out, _, ret := runFiles(t, &cli.Config{List: true, Quiet: true}, "foo")
	if ret != 0 {
		t.Fatalf("ret = %d", ret)
	}
	want := "usr/bin/foo\nusr/bin/bar\nusr/share/doc/foo/readme\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

func TestFilesListRepoFilter(t *testing.T) {
	dir := filesTestEnv(t)
	addFilesRepo(t, dir)

	out, _, ret := runFiles(t, &cli.Config{List: true, Quiet: true}, "test/main/foo")
	if ret != 0 {
		t.Fatalf("ret = %d", ret)
	}
	if !strings.Contains(out, "usr/bin/foo\n") {
		t.Fatalf("stdout = %q", out)
	}

	out, errOut, ret := runFiles(t, &cli.Config{List: true}, "other/main/foo")
	if ret != 1 {
		t.Fatalf("ret = %d, want 1", ret)
	}
	if out != "" {
		t.Fatalf("stdout = %q", out)
	}
	if errOut != "error: package 'other/main/foo' was not found\n" {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestFilesListInvalidTarget(t *testing.T) {
	dir := filesTestEnv(t)
	addFilesRepo(t, dir)

	_, errOut, ret := runFiles(t, &cli.Config{List: true}, "test/main/")
	if ret != 1 {
		t.Fatalf("ret = %d, want 1", ret)
	}
	if errOut != "error: invalid package: 'test/main/'\n" {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestFilesListMachineReadable(t *testing.T) {
	dir := filesTestEnv(t)
	addFilesRepo(t, dir)

	out, _, ret := runFiles(t, &cli.Config{List: true, MachineReadable: true}, "test/main/foo")
	if ret != 0 {
		t.Fatalf("ret = %d", ret)
	}
	want := "test/main\x00foo\x001.0-1\x00usr/bin/foo\n" +
		"test/main\x00foo\x001.0-1\x00usr/bin/bar\n" +
		"test/main\x00foo\x001.0-1\x00usr/share/doc/foo/readme\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

func TestFilesMixedTargets(t *testing.T) {
	dir := filesTestEnv(t)
	addFilesRepo(t, dir)

	out, _, ret := runFiles(t, &cli.Config{Quiet: true}, "foo", "missing")
	if ret != 1 {
		t.Fatalf("ret = %d, want 1", ret)
	}
	if out != "test/main/foo\n" {
		t.Fatalf("stdout = %q", out)
	}
}
