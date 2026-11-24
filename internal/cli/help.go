package cli

import (
	_ "embed"
	"fmt"
	"io"
	"strings"
)

//go:embed helpdata/main.txt
var helpMain string

//go:embed helpdata/D.txt
var helpDatabase string

//go:embed helpdata/F.txt
var helpFiles string

//go:embed helpdata/Q.txt
var helpQuery string

//go:embed helpdata/R.txt
var helpRemove string

//go:embed helpdata/S.txt
var helpSync string

//go:embed helpdata/T.txt
var helpDeptest string

//go:embed helpdata/U.txt
var helpUpgrade string

func Usage(w io.Writer, prog string, op Op) string {
	var text string
	switch op {
	case OpDatabase:
		text = helpDatabase
	case OpFiles:
		text = helpFiles
	case OpQuery:
		text = helpQuery
	case OpRemove:
		text = helpRemove
	case OpSync:
		text = helpSync
	case OpDeptest:
		text = helpDeptest
	case OpUpgrade:
		text = helpUpgrade
	default:
		text = helpMain
	}
	return strings.ReplaceAll(text, "pacdpkg", prog)
}

func Version(w io.Writer, prog, version, backend string) {
	fmt.Fprintf(w, "%s v%s - %s\n", prog, version, backend)
	fmt.Fprintf(w, "Debian-family backend (dpkg/apt), not libalpm. GPL-2.0-or-later.\n")
}
