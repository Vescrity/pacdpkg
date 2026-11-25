package render

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// TxPkg is one target of a package transaction.
type TxPkg struct {
	Name             string
	OldVersion       string
	NewVersion       string
	Repo             string
	DownloadSize     int64
	InstalledSize    int64
	OldInstalledSize int64
	Explicit         bool
}

func (t TxPkg) removing() bool { return t.NewVersion == "" }

func (t TxPkg) netChange() int64 {
	if t.removing() {
		return -t.InstalledSize
	}
	return t.InstalledSize - t.OldInstalledSize
}

// sortTargets mirrors pacman's target_cmp: implicit targets first,
// explicit ones after, each group sorted by name.
func sortTargets(pkgs []TxPkg) {
	sort.SliceStable(pkgs, func(i, j int) bool {
		if pkgs[i].Explicit != pkgs[j].Explicit {
			return !pkgs[i].Explicit
		}
		return pkgs[i].Name < pkgs[j].Name
	})
}

// txSize follows pacman's transaction display, which always uses MiB.
func txSize(bytes int64) string {
	return fmt.Sprintf("%.2f MiB", float64(bytes)/(1024*1024))
}

func mixedTransaction(pkgs []TxPkg) bool {
	var add, del bool
	for _, p := range pkgs {
		if p.removing() {
			del = true
		} else {
			add = true
		}
	}
	return add && del
}

// Transaction prints the transaction summary. With verbose (VerbosePkgLists
// in pacman.conf) a table is shown, otherwise the compact list.
func Transaction(w io.Writer, pkgs []TxPkg, verbose, downloadOnly bool, cols int) {
	pkgs = append([]TxPkg(nil), pkgs...)
	sortTargets(pkgs)

	var dlsize, isize, rsize int64
	for _, p := range pkgs {
		if !p.removing() {
			dlsize += p.DownloadSize
			isize += p.InstalledSize
		}
		if p.removing() {
			rsize += p.InstalledSize
		} else if p.OldVersion != "" {
			rsize += p.OldInstalledSize
		}
	}

	fmt.Fprintln(w)
	if verbose {
		verboseTable(w, pkgs)
	} else {
		names := make([]string, 0, len(pkgs))
		for _, p := range pkgs {
			ver := p.NewVersion
			if ver == "" {
				ver = p.OldVersion
			}
			name := p.Name + "-" + Faint(ver)
			if p.removing() && mixedTransaction(pkgs) {
				name += " " + Meta("[removal]")
			}
			names = append(names, name)
		}
		title := fmt.Sprintf("Packages (%d)", len(pkgs))
		io.WriteString(w, Bold(title)+" ")
		writeList(w, names, len(title)+1, cols)
	}
	fmt.Fprintln(w)

	var rows []sizeRow
	if dlsize > 0 || downloadOnly {
		rows = append(rows, sizeRow{"Total Download Size:", txSize(dlsize)})
	}
	if !downloadOnly {
		if isize > 0 {
			rows = append(rows, sizeRow{"Total Installed Size:", txSize(isize)})
		}
		if rsize > 0 && isize == 0 {
			rows = append(rows, sizeRow{"Total Removed Size:", txSize(rsize)})
		}
		if isize > 0 && rsize > 0 {
			rows = append(rows, sizeRow{"Net Upgrade Size:", txSize(isize - rsize)})
		}
	}
	sizesTable(w, rows)
	fmt.Fprintln(w)
}

func verboseTable(w io.Writer, pkgs []TxPkg) {
	hasOld, hasNew, hasNet, hasDl := false, false, false, false
	for _, p := range pkgs {
		hasOld = hasOld || p.OldVersion != ""
		hasNew = hasNew || p.NewVersion != ""
		hasNet = hasNet || p.OldVersion != "" || p.NewVersion != ""
		hasDl = hasDl || p.DownloadSize > 0
	}

	header := []string{fmt.Sprintf("Package (%d)", len(pkgs))}
	right := []bool{false}
	rows := make([][]string, 0, len(pkgs))
	for _, p := range pkgs {
		name := p.Name
		if p.Repo != "" && p.Repo != "local" {
			name = p.Repo + "/" + p.Name
		}
		row := []string{name}
		if hasOld {
			row = append(row, p.OldVersion)
		}
		if hasNew {
			row = append(row, p.NewVersion)
		}
		if hasNet {
			row = append(row, txSize(p.netChange()))
		}
		if hasDl {
			if p.DownloadSize > 0 {
				row = append(row, txSize(p.DownloadSize))
			} else {
				row = append(row, "")
			}
		}
		rows = append(rows, row)
	}
	if hasOld {
		header = append(header, "Old Version")
		right = append(right, false)
	}
	if hasNew {
		header = append(header, "New Version")
		right = append(right, false)
	}
	if hasNet {
		header = append(header, "Net Change")
		right = append(right, true)
	}
	if hasDl {
		header = append(header, "Download Size")
		right = append(right, true)
	}
	table(w, header, rows, right)
}

func table(w io.Writer, header []string, rows [][]string, right []bool) {
	widths := make([]int, len(header))
	for i, h := range header {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, c := range row {
			if i < len(widths) && len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}
	line := func(cells []string, headerRow bool) {
		var b strings.Builder
		for i, c := range cells {
			if i > 0 {
				b.WriteString("  ")
			}
			pad := widths[i] - len(c)
			if pad < 0 {
				pad = 0
			}
			if headerRow {
				c = Bold(c)
			}
			if right[i] {
				b.WriteString(strings.Repeat(" ", pad))
				b.WriteString(c)
			} else {
				b.WriteString(c)
				b.WriteString(strings.Repeat(" ", pad))
			}
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
	line(header, true)
	line(make([]string, len(header)), false)
	for _, row := range rows {
		line(row, false)
	}
}

type sizeRow struct{ label, value string }

func sizesTable(w io.Writer, rows []sizeRow) {
	if len(rows) == 0 {
		return
	}
	labelWidth, valueWidth := 0, 0
	for _, r := range rows {
		if len(r.label) > labelWidth {
			labelWidth = len(r.label)
		}
		if len(r.value) > valueWidth {
			valueWidth = len(r.value)
		}
	}
	for _, r := range rows {
		label := Bold(r.label)
		fmt.Fprintf(w, "%s%s  %*s\n", label, strings.Repeat(" ", labelWidth-len(r.label)), valueWidth, r.value)
	}
}

var stdinReader = bufio.NewReader(os.Stdin)

// Question prints a pacman-style confirmation. Without noconfirm the prompt
// goes to stderr so piped stdout stays clean; with noconfirm the prompt is
// still printed (to stdout) and the preset answer is used.
func Question(text string, presetYes, noconfirm bool) bool {
	stream := io.Writer(os.Stderr)
	if noconfirm {
		stream = os.Stdout
	}
	suffix := "[y/N]"
	if presetYes {
		suffix = "[Y/n]"
	}
	fmt.Fprintf(stream, "%s%s %s%s ", PromptStart(), text, suffix, Reset())
	if noconfirm {
		fmt.Fprintln(stream)
		return presetYes
	}

	line, err := stdinReader.ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	if line != "" {
		fmt.Fprintln(stream)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return presetYes
	}
	switch strings.ToLower(line) {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	}
	return false
}
