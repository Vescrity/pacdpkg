package alpm

import "strings"

// Alt is one alternative of a dependency, with its own version constraint.
type Alt struct {
	Name    string
	Op      string
	Version string
}

// Dep is a dependency entry. Debian allows alternatives ("a | b"), so a
// dependency may have more than one Alt.
type Dep struct {
	Alts []Alt
	Desc string
}

func (d Dep) Name() string {
	if len(d.Alts) == 0 {
		return ""
	}
	return d.Alts[0].Name
}

func (d Dep) String() string {
	parts := make([]string, 0, len(d.Alts))
	for _, a := range d.Alts {
		s := a.Name
		if a.Op != "" {
			s += pacmanOp(a.Op) + a.Version
		}
		parts = append(parts, s)
	}
	s := strings.Join(parts, "|")
	if d.Desc != "" {
		s += ": " + d.Desc
	}
	return s
}

func pacmanOp(op string) string {
	switch op {
	case "<<":
		return "<"
	case ">>":
		return ">"
	}
	return op
}

// ParseDepList parses a Debian dependency field such as
// "libc6 (>= 2.38), debianutils (>= 5.6-0.1)".
func ParseDepList(field string) []Dep {
	var deps []Dep
	for _, item := range strings.Split(field, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		var d Dep
		for _, atom := range strings.Split(item, "|") {
			name, op, ver := parseDepAtom(strings.TrimSpace(atom))
			if name != "" {
				d.Alts = append(d.Alts, Alt{Name: name, Op: op, Version: ver})
			}
		}
		if len(d.Alts) > 0 {
			deps = append(deps, d)
		}
	}
	return deps
}

// parseDepAtom splits "name (op version)" and drops multi-arch qualifiers.
func parseDepAtom(s string) (name, op, version string) {
	if i := strings.IndexByte(s, '('); i >= 0 {
		tail := strings.TrimSuffix(strings.TrimSpace(s[i+1:]), ")")
		name = strings.TrimSpace(s[:i])
		fields := strings.Fields(tail)
		if len(fields) == 2 {
			op, version = fields[0], fields[1]
		}
	} else {
		name = strings.TrimSpace(s)
	}
	if i := strings.IndexByte(name, ':'); i >= 0 {
		name = name[:i]
	}
	return name, op, version
}

// Pkg is the pacman-shaped view of a Debian package.
type Pkg struct {
	Name                string
	Version             string
	Arch                string
	Description         string
	URL                 string
	Licenses            []string
	Groups              []string
	Provides            []Dep
	Depends             []Dep
	OptDepends          []Dep
	OptDependsInstalled []bool
	Conflicts           []Dep
	Breaks              []Dep
	Replaces            []Dep

	Repo          string
	Binary        string
	SHA256        string
	MD5           string
	DownloadSize  int64
	InstalledSize int64
	Packager      string
	BuildDate     int64
	InstallDate   int64
	Explicit      bool
	ReasonKnown   bool
	StateWant     string
	StateFlag     string
	StateStatus   string
	HasScriptlet  bool
	Validated     string

	RequiredBy  []string
	OptionalFor []string

	Conffiles []Conffile
	Files     []string
	RawFiles  []string
}

type Conffile struct {
	Path     string
	Sum      string
	Modified bool
	Missing  bool
}
