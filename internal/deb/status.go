package deb

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"pacdpkg/internal/alpm"
)

const (
	statusFile = "/var/lib/dpkg/status"
	infoDir    = "/var/lib/dpkg/info"
	extStates  = "/var/lib/apt/extended_states"
)

type stanza struct {
	fields    map[string]string
	conffiles []alpm.Conffile
}

// readStanzas parses an RFC822-style dpkg/apt database file.
func readStanzas(path string) ([]*stanza, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []*stanza
	cur := &stanza{fields: map[string]string{}}
	var last string

	flush := func() {
		if len(cur.fields) > 0 {
			out = append(out, cur)
		}
		cur = &stanza{fields: map[string]string{}}
		last = ""
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if last == "Conffiles" && fieldPath(line) {
				if cf, ok := parseConffile(line); ok {
					cur.conffiles = append(cur.conffiles, cf)
				}
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		last = key
		cur.fields[key] = strings.TrimSpace(value)
	}
	flush()
	return out, sc.Err()
}

func fieldPath(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "/")
}

func parseConffile(line string) (alpm.Conffile, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "/") {
		return alpm.Conffile{}, false
	}
	cf := alpm.Conffile{Path: fields[0]}
	sum := fields[1]
	if len(sum) == 32 && strings.Trim(sum, "0123456789abcdef") == "" {
		cf.Sum = sum
	}
	return cf, true
}

// loadLocal builds the installed package set from dpkg's status file and
// its info directory, without invoking dpkg itself.
func loadLocal() ([]*alpm.Pkg, error) {
	stanzas, err := readStanzas(statusFile)
	if err != nil {
		return nil, err
	}
	var pkgs []*alpm.Pkg
	for _, st := range stanzas {
		state := strings.Fields(st.fields["Status"])
		if len(state) != 3 || state[2] != "installed" {
			continue
		}
		p := &alpm.Pkg{
			Name:        st.fields["Package"],
			Version:     st.fields["Version"],
			Arch:        st.fields["Architecture"],
			Groups:      nonEmpty(st.fields["Section"]),
			Packager:    st.fields["Maintainer"],
			URL:         st.fields["Homepage"],
			Description: st.fields["Description"],
			Depends:     alpm.ParseDepList(st.fields["Depends"]),
			OptDepends:  alpm.ParseDepList(joinNonEmpty(st.fields["Recommends"], st.fields["Suggests"])),
			Provides:    alpm.ParseDepList(st.fields["Provides"]),
			Conflicts:   alpm.ParseDepList(st.fields["Conflicts"]),
			Breaks:      alpm.ParseDepList(st.fields["Breaks"]),
			Replaces:    alpm.ParseDepList(st.fields["Replaces"]),
			Conffiles:   st.conffiles,
			StateWant:   state[0],
			StateFlag:   state[1],
			StateStatus: state[2],
			Validated:   "Unknown",
		}
		p.Depends = append(p.Depends, alpm.ParseDepList(st.fields["Pre-Depends"])...)
		if n, err := strconv.ParseInt(st.fields["Installed-Size"], 10, 64); err == nil {
			p.InstalledSize = n * 1024
		}
		if p.Name == "" {
			continue
		}
		qualified := st.fields["Multi-Arch"] == "same"
		p.Binary = binaryName(p.Name, p.Arch, qualified)
		if info, err := os.Stat(infoPath(p.Binary, p.Name, ".list")); err == nil {
			p.InstallDate = info.ModTime().Unix()
		}
		p.HasScriptlet = hasMaintainerScript(p.Binary, p.Name)
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

func joinNonEmpty(vals ...string) string {
	var out []string
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return strings.Join(out, ",")
}

func binaryName(name, arch string, qualified bool) string {
	if qualified {
		return name + ":" + arch
	}
	return name
}

func infoPath(binary, name, suffix string) string {
	if binary != "" {
		path := infoDir + "/" + binary + suffix
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return infoDir + "/" + name + suffix
}

// loadAutoInstalled reads apt's extended_states, which records which
// packages were pulled in as dependencies.
func loadAutoInstalled() map[string]bool {
	auto := map[string]bool{}
	stanzas, err := readStanzas(extStates)
	if err != nil {
		return auto
	}
	for _, st := range stanzas {
		if st.fields["Auto-Installed"] != "1" {
			continue
		}
		auto[st.fields["Package"]] = true
		auto[binaryName(st.fields["Package"], st.fields["Architecture"], true)] = true
	}
	return auto
}

// dpkgVersion returns the installed dpkg version from the status file.
func dpkgVersion() string {
	stanzas, err := readStanzas(statusFile)
	if err != nil {
		return ""
	}
	for _, st := range stanzas {
		if st.fields["Package"] == "dpkg" {
			return st.fields["Version"]
		}
	}
	return ""
}

// rawFileList returns the recorded file list of an installed package,
// without touching the filesystem or sorting.
func rawFileList(p *alpm.Pkg) ([]string, error) {
	if p.RawFiles != nil {
		return p.RawFiles, nil
	}
	data, err := os.ReadFile(infoPath(p.Binary, p.Name, ".list"))
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "/." {
			continue
		}
		if !strings.HasPrefix(line, "/") {
			line = "/" + line
		}
		files = append(files, line)
	}
	p.RawFiles = files
	return files, nil
}

// listFiles returns the package indices in dpkg's info directory.
func listFiles() []string {
	entries, err := os.ReadDir(infoDir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".list") {
			out = append(out, e.Name())
		}
	}
	return out
}

// md5Sums parses the recorded md5sums of an installed package.
func md5Sums(p *alpm.Pkg) map[string]string {
	data, err := os.ReadFile(infoPath(p.Binary, p.Name, ".md5sums"))
	if err != nil {
		return nil
	}
	return parseMd5Sums(data)
}

func parseMd5Sums(data []byte) map[string]string {
	sums := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if len(line) < 34 {
			continue
		}
		sum := line[:32]
		if strings.Trim(sum, "0123456789abcdef") != "" {
			continue
		}
		path := strings.TrimLeft(line[32:], " ")
		if path == "" {
			continue
		}
		sums["/"+strings.TrimPrefix(path, "./")] = sum
	}
	return sums
}

func nonEmpty(vals ...string) []string {
	var out []string
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func hasMaintainerScript(binary, name string) bool {
	for _, suf := range []string{".preinst", ".postinst", ".prerm", ".postrm", ".config"} {
		if _, err := os.Stat(infoPath(binary, name, suf)); err == nil {
			return true
		}
	}
	return false
}

// AuditProblems reports stanzas whose dpkg state is not a clean install.
func AuditProblems() ([]string, error) {
	stanzas, err := readStanzas(statusFile)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, st := range stanzas {
		state := strings.Fields(st.fields["Status"])
		if len(state) != 3 {
			continue
		}
		if state[1] == "ok" && state[2] == "installed" {
			continue
		}
		if state[0] == "deinstall" && (state[2] == "config-files" || state[2] == "not-installed") {
			continue
		}
		out = append(out, "'"+st.fields["Package"]+"': "+strings.Join(state, " "))
	}
	return out, nil
}

// loadDiversions parses dpkg's diversions database: triples of
// original path, diverted path and owning package.
func loadDiversions() map[string]string {
	out := map[string]string{}
	f, err := os.Open("/var/lib/dpkg/diversions")
	if err != nil {
		return out
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	for i := 0; i+2 < len(lines); i += 3 {
		out[lines[i]] = lines[i+1]
	}
	return out
}
