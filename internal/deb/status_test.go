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

func TestReleaseMatchesComponentAndHost(t *testing.T) {
	cand := &SyncPkg{
		Name: "foo", Version: "1.0", Repo: "crimson/main",
		Source:  &SourceRepo{URI: "https://example.org/debian", Component: "main", Suite: "crimson"},
		Release: &ReleaseInfo{Suite: "crimson", Origin: "Linux Deepin", Label: "Deepin"},
	}
	if !(Pin{Package: "*", Pin: "release c=main", Priority: 900}).matches(cand) {
		t.Fatal("component pin did not match")
	}
	if (Pin{Package: "*", Pin: "origin \"Linux Deepin\"", Priority: 900}).matches(cand) {
		t.Fatal("origin pin should match the host, not Origin")
	}
	if !(Pin{Package: "*", Pin: "origin \"example.org\"", Priority: 900}).matches(cand) {
		t.Fatal("host origin pin did not match")
	}
	if prio, ok := pinPriority(cand, []Pin{
		{Package: "*", Pin: "release a=crimson", Priority: 900},
		{Package: "*", Pin: "release a=crimson", Priority: 950},
	}); !ok || prio != 950 {
		t.Fatalf("general pins should use max, got %d ok=%v", prio, ok)
	}
	if prio, ok := pinPriority(cand, []Pin{
		{Package: "foo", Pin: "release a=crimson", Priority: 901},
		{Package: "foo", Pin: "release a=crimson", Priority: 902},
	}); !ok || prio != 901 {
		t.Fatalf("specific pins should use the first match, got %d ok=%v", prio, ok)
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
