package deb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArchFromDpkgArch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "arch")
	if err := os.WriteFile(path, []byte("amd64\ni386\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := archFromDpkgArch(path); got != "amd64" {
		t.Fatalf("archFromDpkgArch = %q, want amd64", got)
	}
	if err := os.WriteFile(path, []byte("all\narm64\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := archFromDpkgArch(path); got != "arm64" {
		t.Fatalf("archFromDpkgArch skipped all but got %q", got)
	}
	if got := archFromDpkgArch(filepath.Join(dir, "missing")); got != "" {
		t.Fatalf("missing arch file = %q", got)
	}
}

func TestDebArch(t *testing.T) {
	cases := map[string]string{
		"x86_64":  "amd64",
		"aarch64": "arm64",
		"i686":    "i386",
		"armv7l":  "armhf",
		"amd64":   "amd64",
	}
	for in, want := range cases {
		if got := DebArch(in); got != want {
			t.Errorf("DebArch(%q) = %q, want %q", in, got, want)
		}
	}
}
