package app

import "testing"

func TestAptTarget(t *testing.T) {
	cases := map[string]string{
		"/tmp/pkg.deb": "/tmp/pkg.deb",
		"./pkg.deb":    "./pkg.deb",
		"dir/pkg.deb":  "dir/pkg.deb",
		"pkg.deb":      "./pkg.deb",
	}
	for in, want := range cases {
		if got := aptTarget(in); got != want {
			t.Errorf("aptTarget(%q) = %q, want %q", in, got, want)
		}
	}
}
