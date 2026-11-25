package render

import (
	"fmt"
	"io"
	"strings"
	"time"

	"pacdpkg/internal/alpm"
)

// titleWidth is the width of pacman's longest info label ("Compressed Size").
const titleWidth = 15

func title(name string) string {
	return fmt.Sprintf("%-*s :", titleWidth, name)
}

// HumanSize converts bytes to a human readable value, following
// pacman's humanize_size(): with targetUnit 0 the first unit that brings
// the value to 2048 or below is chosen.
func HumanSize(bytes int64, targetUnit byte, precision int) (float64, string) {
	labels := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB", "ZiB", "YiB"}
	val := float64(bytes)
	index := 0
	for index = 0; index < len(labels)-1; index++ {
		if targetUnit != 0 && labels[index][0] == targetUnit {
			break
		}
		if targetUnit == 0 && val <= 2048.0 && val >= -2048.0 {
			break
		}
		val /= 1024.0
	}
	if precision >= 0 && val < 0.0 && val > -0.5/pow10(precision) {
		val = 0.0
	}
	return val, labels[index]
}

func pow10(n int) float64 {
	r := 1.0
	for ; n > 0; n-- {
		r *= 10
	}
	return r
}

func Date(epoch int64) string {
	if epoch <= 0 {
		return ""
	}
	return time.Unix(epoch, 0).Format("Mon Jan _2 15:04:05 2006")
}

func NoneIfEmpty(items []string) []string {
	if len(items) == 0 {
		return []string{"None"}
	}
	return items
}

func optDepStrings(p *alpm.Pkg) []string {
	out := make([]string, 0, len(p.OptDepends))
	for i, d := range p.OptDepends {
		s := d.String()
		if i < len(p.OptDependsInstalled) && p.OptDependsInstalled[i] {
			s += " [installed]"
		}
		out = append(out, s)
	}
	return out
}

func depStrings(deps []alpm.Dep) []string {
	out := make([]string, 0, len(deps))
	for _, d := range deps {
		out = append(out, d.String())
	}
	return out
}

func writeWrapped(w io.Writer, s string, indent, cols int) {
	if cols <= indent || !strings.Contains(s, " ") {
		io.WriteString(w, s)
		return
	}
	width := cols - indent
	words := strings.Fields(s)
	line := 0
	for i, word := range words {
		if i == 0 {
			io.WriteString(w, word)
			line = len(word)
			continue
		}
		if line+1+len(word) > width {
			io.WriteString(w, "\n"+strings.Repeat(" ", indent)+word)
			line = len(word)
			continue
		}
		io.WriteString(w, " "+word)
		line += 1 + len(word)
	}
}

func Field(w io.Writer, name, value string, cols int) {
	io.WriteString(w, Bold(title(name))+" ")
	if value == "" {
		io.WriteString(w, "None")
	} else {
		writeWrapped(w, value, titleWidth+3, cols)
	}
	io.WriteString(w, "\n")
}

func writeList(w io.Writer, items []string, indent, cols int) {
	line := 0
	width := cols - indent
	for i, item := range items {
		if i == 0 {
			io.WriteString(w, item)
			line = len(item)
			continue
		}
		if width > 0 && line+2+len(item) > width {
			io.WriteString(w, "\n"+strings.Repeat(" ", indent)+item)
			line = len(item)
			continue
		}
		io.WriteString(w, "  "+item)
		line += 2 + len(item)
	}
	io.WriteString(w, "\n")
}

func FieldList(w io.Writer, name string, items []string, cols int) {
	io.WriteString(w, Bold(title(name))+" ")
	writeList(w, NoneIfEmpty(items), titleWidth+3, cols)
}

func FieldLinebreak(w io.Writer, name string, items []string, cols int) {
	items = NoneIfEmpty(items)
	indent := titleWidth + 3
	io.WriteString(w, Bold(title(name))+" ")
	for i, item := range items {
		if i > 0 {
			io.WriteString(w, "\n"+strings.Repeat(" ", indent))
		}
		writeWrapped(w, item, indent, cols)
	}
	io.WriteString(w, "\n")
}

type Origin int

const (
	FromLocal Origin = iota
	FromSync
	FromFile
)

type InfoOpts struct {
	Origin Origin
	Extra  bool
	Cols   int
}

func PkgInfo(w io.Writer, p *alpm.Pkg, o InfoOpts) {
	if o.Origin == FromSync {
		Field(w, "Repository", p.Repo, o.Cols)
	}
	Field(w, "Name", p.Name, o.Cols)
	Field(w, "Version", p.Version, o.Cols)
	Field(w, "Description", p.Description, o.Cols)
	Field(w, "Architecture", p.Arch, o.Cols)
	Field(w, "URL", p.URL, o.Cols)
	FieldList(w, "Licenses", p.Licenses, o.Cols)
	FieldList(w, "Groups", p.Groups, o.Cols)
	FieldList(w, "Provides", depStrings(p.Provides), o.Cols)
	FieldList(w, "Depends On", depStrings(p.Depends), o.Cols)
	FieldLinebreak(w, "Optional Deps", optDepStrings(p), o.Cols)

	if o.Extra || o.Origin == FromLocal {
		FieldList(w, "Required By", p.RequiredBy, o.Cols)
		FieldList(w, "Optional For", p.OptionalFor, o.Cols)
	}
	FieldList(w, "Conflicts With", depStrings(p.Conflicts), o.Cols)
	FieldList(w, "Replaces", depStrings(p.Replaces), o.Cols)

	switch o.Origin {
	case FromSync:
		if p.DownloadSize > 0 {
			val, label := HumanSize(p.DownloadSize, 0, 2)
			fmt.Fprintf(w, "%s %.2f %s\n", title("Download Size"), val, label)
			val, label = HumanSize(p.InstalledSize, label[0], 2)
			fmt.Fprintf(w, "%s %.2f %s\n", title("Installed Size"), val, label)
		} else {
			val, label := HumanSize(p.InstalledSize, 0, 2)
			fmt.Fprintf(w, "%s %.2f %s\n", title("Installed Size"), val, label)
		}
	case FromFile:
		if p.DownloadSize > 0 {
			val, label := HumanSize(p.DownloadSize, 0, 2)
			fmt.Fprintf(w, "%s %.2f %s\n", title("Compressed Size"), val, label)
			val, label = HumanSize(p.InstalledSize, label[0], 2)
			fmt.Fprintf(w, "%s %.2f %s\n", title("Installed Size"), val, label)
		}
	default:
		val, label := HumanSize(p.InstalledSize, 0, 2)
		fmt.Fprintf(w, "%s %.2f %s\n", title("Installed Size"), val, label)
	}

	Field(w, "Packager", p.Packager, o.Cols)
	Field(w, "Build Date", Date(p.BuildDate), o.Cols)
	if o.Origin == FromLocal {
		Field(w, "Install Date", Date(p.InstallDate), o.Cols)
		reason := "Unknown"
		if p.ReasonKnown {
			if p.Explicit {
				reason = "Explicitly installed"
			} else {
				reason = "Installed as a dependency for another package"
			}
		}
		Field(w, "Install Reason", reason, o.Cols)
	}
	if o.Origin == FromLocal || o.Origin == FromFile {
		script := "No"
		if p.HasScriptlet {
			script = "Yes"
		}
		Field(w, "Install Script", script, o.Cols)
	}
	if !(o.Origin == FromSync && o.Extra) {
		Field(w, "Validated By", p.Validated, o.Cols)
	}
	switch {
	case o.Extra && o.Origin == FromSync:
		Field(w, "MD5 Sum", p.MD5, o.Cols)
		Field(w, "SHA-256 Sum", p.SHA256, o.Cols)
		FieldList(w, "Signatures", nil, o.Cols)
	case o.Origin == FromFile:
		FieldList(w, "Signatures", nil, o.Cols)
	}

	if o.Extra {
		if o.Origin == FromLocal {
			indent := titleWidth + 3
			if len(p.Conffiles) == 0 {
				io.WriteString(w, Bold(title("Backup Files"))+" (none)\n")
			}
			for i, cf := range p.Conffiles {
				state := "[unmodified]"
				if cf.Modified {
					state = "[modified]"
				}
				if i == 0 {
					io.WriteString(w, Bold(title("Backup Files"))+" ")
				} else {
					io.WriteString(w, strings.Repeat(" ", indent))
				}
				io.WriteString(w, cf.Path+" "+state+"\n")
			}
		}
		Field(w, "Extended Data", "pkgtype=deb", o.Cols)
	}
	io.WriteString(w, "\n")
}

func PkgLine(w io.Writer, p *alpm.Pkg, quiet bool) {
	if quiet {
		fmt.Fprintln(w, p.Name)
		return
	}
	fmt.Fprintf(w, "%s %s\n", Bold(p.Name), Version(p.Version))
}

func UpgradeLine(w io.Writer, p *alpm.Pkg, newver string, ignored, quiet bool) {
	if quiet {
		fmt.Fprintln(w, p.Name)
		return
	}
	fmt.Fprintf(w, "%s %s", Bold(p.Name), Version(p.Version))
	if newver != "" {
		fmt.Fprintf(w, " -> %s", Version(newver))
	}
	if ignored {
		fmt.Fprintf(w, " %s", Meta("[ignored]"))
	}
	fmt.Fprintln(w)
}

func SearchResult(w io.Writer, p *alpm.Pkg, repo, mark string, quiet bool, cols int) {
	if quiet {
		fmt.Fprintln(w, p.Name)
		return
	}
	fmt.Fprintf(w, "%s/%s %s%s\n", Repo(repo), Bold(p.Name), Version(p.Version), Meta(mark))
	io.WriteString(w, "    ")
	writeWrapped(w, p.Description, 4, cols)
	io.WriteString(w, "\n")
}

func FileLine(w io.Writer, pkg, path string, quiet bool) {
	if quiet {
		fmt.Fprintln(w, path)
		return
	}
	fmt.Fprintf(w, "%s %s\n", Bold(pkg), path)
}
