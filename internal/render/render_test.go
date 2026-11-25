package render

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"pacdpkg/internal/alpm"
)

func TestHumanSize(t *testing.T) {
	cases := []struct {
		bytes int64
		unit  byte
		wantV float64
		wantU string
	}{
		{2001850, 0, 1954.93, "KiB"},
		{10055844, 0, 9.59, "MiB"},
		{10882312, 0, 10.38, "MiB"},
		{24559, 0, 23.98, "KiB"},
		{809, 0, 809, "B"},
		{2097152, 0, 2048.00, "KiB"},
		{2096906, 0, 2047.76, "KiB"},
		{2001850, 'K', 1954.93, "KiB"},
		{10055844, 'K', 9820.16, "KiB"},
	}
	for _, c := range cases {
		v, u := HumanSize(c.bytes, c.unit, 2)
		if u != c.wantU || v-c.wantV > 0.005 || c.wantV-v > 0.005 {
			t.Errorf("HumanSize(%d, %q) = %.2f %s, want %.2f %s", c.bytes, c.unit, v, u, c.wantV, c.wantU)
		}
	}
}

func TestDate(t *testing.T) {
	ts := time.Date(2026, 9, 21, 11, 26, 5, 0, time.Local).Unix()
	if got, want := Date(ts), "Mon Sep 21 11:26:05 2026"; got != want {
		t.Errorf("Date() = %q, want %q", got, want)
	}
	if got := Date(0); got != "" {
		t.Errorf("Date(0) = %q, want empty", got)
	}
}

func TestPkgInfoLayout(t *testing.T) {
	p := &alpm.Pkg{
		Name:          "bash",
		Version:       "5.2.37-2deepin1",
		Description:   "GNU Bourne Again SHell",
		Arch:          "amd64",
		URL:           "http://example.invalid",
		Licenses:      []string{"GPL-3+"},
		Groups:        []string{"shells"},
		Depends:       alpm.ParseDepList("libc6 (>= 2.38), libtinfo6 (>= 6)"),
		Packager:      "m <m@example.invalid>",
		InstalledSize: 7175 * 1024,
		InstallDate:   time.Date(2026, 8, 13, 20, 44, 56, 0, time.Local).Unix(),
		Explicit:      true,
		ReasonKnown:   true,
		HasScriptlet:  true,
		Validated:     "Unknown",
	}
	var b bytes.Buffer
	PkgInfo(&b, p, InfoOpts{Origin: FromLocal})
	want := "" +
		"Name            : bash\n" +
		"Version         : 5.2.37-2deepin1\n" +
		"Description     : GNU Bourne Again SHell\n" +
		"Architecture    : amd64\n" +
		"URL             : http://example.invalid\n" +
		"Licenses        : GPL-3+\n" +
		"Groups          : shells\n" +
		"Provides        : None\n" +
		"Depends On      : libc6>=2.38  libtinfo6>=6\n" +
		"Optional Deps   : None\n" +
		"Required By     : None\n" +
		"Optional For    : None\n" +
		"Conflicts With  : None\n" +
		"Replaces        : None\n" +
		"Installed Size  : 7.01 MiB\n" +
		"Packager        : m <m@example.invalid>\n" +
		"Build Date      : None\n" +
		"Install Date    : Thu Aug 13 20:44:56 2026\n" +
		"Install Reason  : Explicitly installed\n" +
		"Install Script  : Yes\n" +
		"Validated By    : Unknown\n" +
		"\n"
	if b.String() != want {
		t.Errorf("PkgInfo mismatch:\ngot:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestSearchResultLayout(t *testing.T) {
	p := &alpm.Pkg{Name: "bash", Version: "1.0", Description: "The shell"}
	var b bytes.Buffer
	SearchResult(&b, p, "local", "", false, 0)
	want := "local/bash 1.0\n    The shell\n"
	if b.String() != want {
		t.Errorf("SearchResult = %q, want %q", b.String(), want)
	}
}

func TestUpgradeLine(t *testing.T) {
	p := &alpm.Pkg{Name: "bash", Version: "1.0"}
	var b bytes.Buffer
	UpgradeLine(&b, p, "1.1", false, false)
	if got, want := b.String(), "bash 1.0 -> 1.1\n"; got != want {
		t.Errorf("UpgradeLine = %q, want %q", got, want)
	}
	b.Reset()
	UpgradeLine(&b, p, "1.1", true, false)
	if got, want := b.String(), "bash 1.0 -> 1.1 [ignored]\n"; got != want {
		t.Errorf("UpgradeLine ignored = %q, want %q", got, want)
	}
}

func TestColors(t *testing.T) {
	EnableColors(false)
	if got := PkgLineString(&alpm.Pkg{Name: "bash", Version: "1.0"}); strings.Contains(got, "\033") {
		t.Fatalf("colors leaked when disabled: %q", got)
	}
	EnableColors(true)
	defer EnableColors(false)
	got := PkgLineString(&alpm.Pkg{Name: "bash", Version: "1.0"})
	want := "\033[0;1mbash\033[0m \033[1;32m1.0\033[0m\n"
	if got != want {
		t.Fatalf("PkgLine = %q, want %q", got, want)
	}
	if !strings.Contains(ErrorPrefix(), "\033[1;31merror: \033[0m") {
		t.Fatalf("error prefix = %q", ErrorPrefix())
	}
	if !strings.Contains(PromptStart(), "\033[1;34m::\033[0;1m ") {
		t.Fatalf("prompt = %q", PromptStart())
	}
}

func PkgLineString(p *alpm.Pkg) string {
	var b bytes.Buffer
	PkgLine(&b, p, false)
	return b.String()
}
