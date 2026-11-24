package cli

import (
	"strings"
	"testing"
)

func keys(toks []Token, op Op) []string {
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		if d := t.For(op); d != nil {
			out = append(out, d.Key)
		} else {
			out = append(out, "<invalid>")
		}
	}
	return out
}

func TestParseCluster(t *testing.T) {
	toks, targets, err := Parse("pacdpkg", []string{"-Syu", "bash"})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(keys(toks, OpSync), ",")
	if got != "op-S,refresh,sysupgrade" {
		t.Errorf("keys = %s", got)
	}
	if len(targets) != 1 || targets[0] != "bash" {
		t.Errorf("targets = %v", targets)
	}
}

func TestParseRepeated(t *testing.T) {
	toks, _, err := Parse("pacdpkg", []string{"-Qii"})
	if err != nil {
		t.Fatal(err)
	}
	if len(toks) != 3 || toks[1].Primary().Key != "info" || toks[2].Primary().Key != "info" {
		t.Errorf("unexpected tokens: %+v", keys(toks, OpQuery))
	}
}

func TestParseAbbrev(t *testing.T) {
	toks, _, err := Parse("pacdpkg", []string{"--quer"})
	if err != nil {
		t.Fatal(err)
	}
	if len(toks) != 1 || toks[0].Primary().Key != "op-Q" {
		t.Errorf("unexpected tokens: %+v", keys(toks, OpQuery))
	}
}

func TestParseAmbiguous(t *testing.T) {
	_, _, err := Parse("pacdpkg", []string{"--q"})
	if err == nil || err.Error() != "pacdpkg: option '--q' is ambiguous; possibilities: '--query' '--quiet'" {
		t.Errorf("err = %v", err)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"-Z"}, "pacdpkg: invalid option -- 'Z'"},
		{[]string{"--bogus"}, "pacdpkg: unrecognized option '--bogus'"},
		{[]string{"-b"}, "pacdpkg: option requires an argument -- 'b'"},
		{[]string{"--dbpath"}, "pacdpkg: option '--dbpath' requires an argument"},
		{[]string{"--query=x"}, "pacdpkg: option '--query' doesn't allow an argument"},
	}
	for _, c := range cases {
		_, _, err := Parse("pacdpkg", c.args)
		if err == nil || err.Error() != c.want {
			t.Errorf("Parse(%v) err = %v, want %q", c.args, err, c.want)
		}
	}
}

func TestParseArgsAndTerminator(t *testing.T) {
	_, targets, err := Parse("pacdpkg", []string{"-Q", "--dbpath", "/tmp/x", "bash", "--", "-pkg"})
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0] != "bash" || targets[1] != "-pkg" {
		t.Errorf("targets = %v", targets)
	}
}

func TestUsageMentionsProgram(t *testing.T) {
	text := Usage(nil, "pacdpkg", OpQuery)
	if !strings.Contains(text, "pacdpkg {-Q --query}") {
		t.Errorf("usage does not use program name:\n%s", text)
	}
}
