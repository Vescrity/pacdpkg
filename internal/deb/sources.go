package deb

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SourceRepo is one binary repository derived from apt's sources.
type SourceRepo struct {
	URI       string
	Suite     string
	Component string
	Label     string
	Order     int
}

func (r SourceRepo) flat() bool {
	return r.Suite == "./" || r.Suite == "." || r.Suite == "/" || r.Suite == ""
}

// PackageURL builds the download URL of a pool file.
func (r SourceRepo) PackageURL(filename string) string {
	base := strings.TrimSuffix(r.URI, "/")
	path := filename
	if r.flat() {
		path = strings.TrimPrefix(path, "./")
	}
	path = strings.TrimPrefix(path, "/")
	return base + "/" + escapePath(path)
}

// escapePath percent-encodes like apt does for pool filenames.
func escapePath(path string) string {
	var b strings.Builder
	for _, c := range []byte(path) {
		switch {
		case c == '/' || c == '-' || c == '.' || c == '_' || c == '~':
			b.WriteByte(c)
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02x", c)
		}
	}
	return b.String()
}

// ContentsURLs lists the candidate Contents index URLs of this repository.
func (r SourceRepo) ContentsURLs(arch string) []string {
	base := strings.TrimSuffix(r.URI, "/")
	name := "Contents-" + arch + ".gz"
	if r.flat() {
		return []string{base + "/" + name}
	}
	return []string{
		base + "/dists/" + r.Suite + "/" + r.Component + "/" + name,
		base + "/dists/" + r.Suite + "/" + name,
	}
}

// listPrefix is how apt spells the binary index filename for this repo,
// without the trailing "_Packages".
func (r SourceRepo) listPrefix(arch string) string {
	stripped := strings.TrimPrefix(strings.TrimPrefix(r.URI, "https://"), "http://")
	stripped = strings.TrimSuffix(stripped, "/")
	stripped = strings.ReplaceAll(stripped, "/", "_")
	if r.flat() {
		return stripped + "_."
	}
	return stripped + "_dists_" + strings.ReplaceAll(r.Suite, "/", "_") + "_" +
		r.Component + "_binary-" + arch
}

// loadSourceRepos parses apt's sources and returns them keyed by the binary
// index filename they describe.
func loadSourceRepos(arch string) map[string]*SourceRepo {
	out := map[string]*SourceRepo{}
	order := 0
	for _, src := range loadSources() {
		for _, comp := range src.components() {
			repo := SourceRepo{URI: src.uri, Suite: src.suite, Component: comp, Order: order}
			order++
			switch {
			case repo.flat():
				repo.Label = hostOf(src.uri)
			case comp == "":
				repo.Label = src.suite
			default:
				repo.Label = src.suite + "/" + comp
			}
			out[repo.listPrefix(arch)+"_Packages"] = &repo
		}
		if src.flat {
			repo := SourceRepo{URI: src.uri, Suite: src.suite, Order: order}
			order++
			repo.Label = hostOf(src.uri)
			out[repo.listPrefix(arch)+"_Packages"] = &repo
		}
	}
	return out
}

type sourceEntry struct {
	uri      string
	suite    string
	flat     bool
	comps    []string
	archList []string
}

func (s sourceEntry) components() []string {
	if s.flat {
		return nil
	}
	if len(s.comps) == 0 {
		return []string{"main"}
	}
	return s.comps
}

func loadSources() []sourceEntry {
	var out []sourceEntry
	files := []string{"/etc/apt/sources.list"}
	entries, err := os.ReadDir("/etc/apt/sources.list.d")
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".list") && !strings.HasSuffix(name, ".sources") {
				continue
			}
			files = append(files, filepath.Join("/etc/apt/sources.list.d", name))
		}
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.HasSuffix(path, ".sources") {
			out = append(out, parseDeb822(string(data))...)
		} else {
			out = append(out, parseOneLine(string(data))...)
		}
	}
	return out
}

func parseOneLine(text string) []sourceEntry {
	var out []sourceEntry
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "deb" {
			continue
		}
		i := 1
		for i < len(fields) && strings.HasPrefix(fields[i], "[") {
			for i < len(fields) && !strings.HasSuffix(fields[i], "]") {
				i++
			}
			i++
		}
		if i+1 >= len(fields) {
			continue
		}
		entry := sourceEntry{uri: fields[i], suite: fields[i+1]}
		entry.flat = entry.suite == "./" || entry.suite == "." || entry.suite == "/"
		if !entry.flat {
			for _, comp := range fields[i+2:] {
				if strings.HasPrefix(comp, "#") {
					break
				}
				entry.comps = append(entry.comps, comp)
			}
		}
		out = append(out, entry)
	}
	return out
}

func parseDeb822(text string) []sourceEntry {
	var out []sourceEntry
	var cur map[string]string
	flush := func() {
		if cur == nil {
			return
		}
		types := strings.Fields(cur["Types"])
		enabled := cur["Enabled"] != "no"
		isDeb := false
		for _, t := range types {
			if t == "deb" {
				isDeb = true
			}
		}
		if !isDeb || !enabled {
			cur = nil
			return
		}
		uris := strings.Fields(cur["URIs"])
		suites := strings.Fields(cur["Suites"])
		comps := strings.Fields(cur["Components"])
		for _, uri := range uris {
			for _, suite := range suites {
				e := sourceEntry{uri: uri, suite: suite, comps: comps}
				e.flat = suite == "./" || suite == "." || suite == "/"
				out = append(out, e)
			}
		}
		cur = nil
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if cur == nil {
			cur = map[string]string{}
		}
		cur[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	flush()
	return out
}

func hostOf(uri string) string {
	uri = strings.TrimPrefix(strings.TrimPrefix(uri, "https://"), "http://")
	return strings.TrimSuffix(uri, "/")
}

// ExpectedListFiles returns the binary index filenames apt should have after
// a successful update.
func ExpectedListFiles(arch string) []string {
	repos := loadSourceRepos(arch)
	out := make([]string, 0, len(repos))
	for name := range repos {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ExpectedListGroups groups the expected index filenames by repository
// (URI + suite). A repository only has to publish one of its components.
func ExpectedListGroups(arch string) [][]string {
	groups := map[string][]string{}
	for _, src := range loadSources() {
		key := src.uri + "|" + src.suite
		for _, comp := range src.components() {
			repo := SourceRepo{URI: src.uri, Suite: src.suite, Component: comp}
			groups[key] = append(groups[key], repo.listPrefix(arch)+"_Packages")
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([][]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, groups[key])
	}
	return out
}
