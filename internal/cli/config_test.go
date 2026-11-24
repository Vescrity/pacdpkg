package cli

import "testing"

func TestApplyValidatesArguments(t *testing.T) {
	cases := []struct {
		key  string
		arg  string
		want string
	}{
		{"debug", "abc", "'abc' is not a valid debug level"},
		{"debug", "3", "'3' is not a valid debug level"},
		{"debug", "2", ""},
		{"ask", "abc", "'abc' is not a valid ask level"},
		{"ask", "4", ""},
		{"color", "foo", "invalid argument 'foo' for --color"},
		{"color", "auto", ""},
	}
	for _, c := range cases {
		cfg := &Config{}
		err := cfg.Apply(&OptDef{Key: c.key}, c.arg)
		if c.want == "" {
			if err != nil {
				t.Errorf("%s=%q: unexpected error %v", c.key, c.arg, err)
			}
			continue
		}
		if err == nil || err.Error() != c.want {
			t.Errorf("%s=%q: err = %v, want %q", c.key, c.arg, err, c.want)
		}
	}
}

func TestConfirmLastWins(t *testing.T) {
	cfg := &Config{}
	cfg.Apply(&OptDef{Key: "noconfirm"}, "")
	if !cfg.NoConfirm {
		t.Fatal("noconfirm not set")
	}
	cfg.Apply(&OptDef{Key: "confirm"}, "")
	if cfg.NoConfirm {
		t.Fatal("confirm did not clear noconfirm")
	}
}

func TestCheckArgs(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			"database asdeps/asexplicit",
			Config{Op: OpDatabase, AsDeps: true, AsExplicit: true},
			"invalid option: '--asdeps' and '--asexplicit' may not be used together",
		},
		{
			"database check/asdeps",
			Config{Op: OpDatabase, Check: 1, AsDeps: true},
			"invalid option: '--asdeps' and '--check' may not be used together",
		},
		{
			"sync clean/refresh",
			Config{Op: OpSync, Clean: 1, Refresh: 1},
			"invalid option: '--clean' and '--refresh' may not be used together",
		},
		{
			"remove nosave/print",
			Config{Op: OpRemove, NoSave: true, Print: true},
			"invalid option: '--nosave' and '--print' may not be used together",
		},
		{
			"upgrade print/dbonly",
			Config{Op: OpUpgrade, Print: true, DBOnly: true},
			"invalid option: '--print' and '--dbonly' may not be used together",
		},
		{"ok", Config{Op: OpSync, SysUpgrade: 1}, ""},
	}
	for _, c := range cases {
		err := c.cfg.CheckArgs()
		if c.want == "" {
			if err != nil {
				t.Errorf("%s: unexpected error %v", c.name, err)
			}
			continue
		}
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}
