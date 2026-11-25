package deb

import (
	"strings"
	"testing"
)

func TestParseConffileVariants(t *testing.T) {
	cf, ok := parseConffile(" /etc/foo 0123456789abcdef0123456789abcdef")
	if !ok || cf.Path != "/etc/foo" || cf.Sum != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("hashed conffile parsed as %+v ok=%v", cf, ok)
	}
	cf, ok = parseConffile(" /etc/bar newconffile remove-on-upgrade")
	if !ok || cf.Path != "/etc/bar" || cf.Sum != "" {
		t.Fatalf("unhashed conffile parsed as %+v ok=%v", cf, ok)
	}
	if _, ok := parseConffile(" not-a-path x"); ok {
		t.Fatal("relative path accepted")
	}
}

func TestMd5SumsPathsWithSpaces(t *testing.T) {
	want := "0123456789abcdef0123456789abcdef"
	content := want + "  usr/share/Amazon Music.format\n"
	sums := parseMd5Sums([]byte(content))
	if sums["/usr/share/Amazon Music.format"] != want {
		t.Fatalf("sums = %v", sums)
	}
}

func TestSourcesFilter(t *testing.T) {
	entries := parseOneLine("deb https://h/debian trixie main # note\ndeb-src https://h trixie main\n")
	if len(entries) != 1 {
		t.Fatalf("entries = %+v", entries)
	}
	if strings.Join(entries[0].comps, ",") != "main" {
		t.Fatalf("components = %v", entries[0].comps)
	}
}
