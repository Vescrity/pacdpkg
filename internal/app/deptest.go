package app

import (
	"fmt"
	"os"
	"strings"

	"pacdpkg/internal/deb"
)

// deptest implements -T: print unsatisfied dependencies and exit 127.
func (a *App) deptest() int {
	db, err := deb.Open()
	if err != nil {
		Errorf("could not read the dpkg database: %v", err)
		return 1
	}
	missing := 0
	for _, t := range a.targets {
		name, op, ver := parseConstraint(t)
		if !db.Satisfies(name, op, ver) {
			fmt.Fprintln(os.Stdout, t)
			missing++
		}
	}
	if missing > 0 {
		return 127
	}
	return 0
}

// parseConstraint splits pacman's dependency syntax: name, name=1.0,
// name>=1.0, name<=1.0, name>1.0, name<1.0.
func parseConstraint(s string) (name, op, version string) {
	for _, o := range []string{">=", "<=", "=", ">", "<"} {
		if i := strings.Index(s, o); i > 0 {
			return s[:i], o, s[i+len(o):]
		}
	}
	return s, "", ""
}
