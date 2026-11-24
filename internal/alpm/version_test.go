package alpm

import (
	"fmt"
	"math/rand"
	"os/exec"
	"testing"
)

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0", "1.0", 0},
		{"1.0", "1.1", -1},
		{"1.1", "1.0", 1},
		{"1.0-1", "1.0-2", -1},
		{"1.0-1", "1.0-1", 0},
		{"1:1.0", "2.0", 1},
		{"2.0", "1:1.0", -1},
		{"0:1.0", "1.0", 0},
		{"1.0~rc1", "1.0", -1},
		{"1.0~rc1", "1.0~rc2", -1},
		{"1.0~~", "1.0~", -1},
		{"1.0", "1.0~", 1},
		{"1.0a", "1.0", 1},
		{"1.0+b1", "1.0", 1},
		{"1.0.10", "1.0.9", 1},
		{"1.0.9", "1.0.10", -1},
		{"0.9", "1.0", -1},
		{"1.0-1~", "1.0-1", -1},
		{"1.0-1+b1", "1.0-1", 1},
		{"2.8.4", "2.8.4deepin1", -1},
		{"5.2.37-2deepin1", "5.2.37-2", 1},
		{"1.2.3-4ubuntu5", "1.2.3-4", 1},
		{"7.0.0", "7.0.0.0", -1},
	}
	for _, c := range cases {
		if got := CompareVersion(c.a, c.b); got != c.want {
			t.Errorf("CompareVersion(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := CompareVersion(c.b, c.a); got != -c.want {
			t.Errorf("CompareVersion(%q, %q) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}

// TestCompareVersionAgainstDpkg cross-checks the implementation with the
// real dpkg, when it is installed.
func TestCompareVersionAgainstDpkg(t *testing.T) {
	if _, err := exec.LookPath("dpkg"); err != nil {
		t.Skip("dpkg not installed")
	}
	rng := rand.New(rand.NewSource(42))
	alphabet := []byte("0123456789.+~abcde")
	gen := func() string {
		n := 1 + rng.Intn(10)
		b := make([]byte, n)
		for i := range b {
			b[i] = alphabet[rng.Intn(len(alphabet))]
		}
		if rng.Intn(4) == 0 {
			return fmt.Sprintf("%d:", rng.Intn(3)) + string(b)
		}
		if rng.Intn(3) == 0 {
			b = append(b, '-')
			for i := 0; i < 1+rng.Intn(3); i++ {
				b = append(b, alphabet[rng.Intn(len(alphabet))])
			}
		}
		return string(b)
	}
	dpkgCmp := func(a, b string) int {
		for _, tc := range [][2]string{{"lt", "-1"}, {"eq", "0"}, {"gt", "1"}} {
			cmd := exec.Command("dpkg", "--compare-versions", a, tc[0], b)
			if err := cmd.Run(); err == nil {
				if tc[1] == "-1" {
					return -1
				}
				if tc[1] == "0" {
					return 0
				}
				return 1
			}
		}
		t.Fatalf("dpkg refuses to compare %q and %q", a, b)
		return 0
	}
	for i := 0; i < 300; i++ {
		a, b := gen(), gen()
		want := dpkgCmp(a, b)
		got := CompareVersion(a, b)
		if sign(got) != want {
			t.Fatalf("CompareVersion(%q, %q) = %d, dpkg says %d", a, b, got, want)
		}
	}
}
