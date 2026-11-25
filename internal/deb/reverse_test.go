package deb

import "testing"

func TestVersionSatisfiesStrictOps(t *testing.T) {
	cases := []struct {
		version string
		op      string
		want    string
		ok      bool
	}{
		{"1.0", ">>", "1.0", false},
		{"1.0", ">>", "0.9", true},
		{"1.0", "<<", "1.0", false},
		{"1.0", "<<", "1.1", true},
		{"1.0", ">=", "1.0", true},
		{"1.0", "<=", "1.0", true},
	}
	for _, tc := range cases {
		if got := versionSatisfies(tc.version, tc.op, tc.want); got != tc.ok {
			t.Errorf("versionSatisfies(%q, %q, %q) = %v, want %v", tc.version, tc.op, tc.want, got, tc.ok)
		}
	}
}
