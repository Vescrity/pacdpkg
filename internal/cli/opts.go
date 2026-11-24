package cli

// Op is a pacman operation.
type Op uint8

const (
	OpMain Op = iota
	OpDatabase
	OpFiles
	OpQuery
	OpRemove
	OpSync
	OpDeptest
	OpUpgrade
	opCount
)

func (o Op) String() string {
	switch o {
	case OpDatabase:
		return "database"
	case OpFiles:
		return "files"
	case OpQuery:
		return "query"
	case OpRemove:
		return "remove"
	case OpSync:
		return "sync"
	case OpDeptest:
		return "deptest"
	case OpUpgrade:
		return "upgrade"
	}
	return "main"
}

type OpMask uint16

func mask(ops ...Op) OpMask {
	var m OpMask
	for _, o := range ops {
		m |= 1 << o
	}
	return m
}

const allOps = OpMask(1<<opCount - 1)

func (m OpMask) Has(o Op) bool { return m&(1<<o) != 0 }

type ArgMode uint8

const (
	ArgNone ArgMode = iota
	ArgRequired
	ArgOptional
)

// OptDef binds one token form (short and/or long) to the operations
// that accept it and to a semantic key. Tokens whose meaning depends on
// the operation appear once per operation.
type OptDef struct {
	Short rune
	Long  string
	Mode  ArgMode
	Ops   OpMask
	Key   string
}

var optTable = []OptDef{
	// operations
	{Short: 'D', Long: "database", Ops: allOps, Key: "op-D"},
	{Short: 'F', Long: "files", Ops: allOps, Key: "op-F"},
	{Short: 'Q', Long: "query", Ops: allOps, Key: "op-Q"},
	{Short: 'R', Long: "remove", Ops: allOps, Key: "op-R"},
	{Short: 'S', Long: "sync", Ops: allOps, Key: "op-S"},
	{Short: 'T', Long: "deptest", Ops: allOps, Key: "op-T"},
	{Short: 'U', Long: "upgrade", Ops: allOps, Key: "op-U"},
	{Short: 'V', Long: "version", Ops: allOps, Key: "version"},
	{Short: 'h', Long: "help", Ops: allOps, Key: "help"},

	// global options
	{Short: 'b', Long: "dbpath", Mode: ArgRequired, Ops: allOps, Key: "dbpath"},
	{Short: 'r', Long: "root", Mode: ArgRequired, Ops: allOps, Key: "root"},
	{Long: "sysroot", Mode: ArgRequired, Ops: allOps, Key: "sysroot"},
	{Long: "ask", Mode: ArgRequired, Ops: allOps, Key: "ask"},
	{Long: "arch", Mode: ArgRequired, Ops: allOps, Key: "arch"},
	{Long: "cachedir", Mode: ArgRequired, Ops: allOps, Key: "cachedir"},
	{Long: "color", Mode: ArgRequired, Ops: allOps, Key: "color"},
	{Long: "config", Mode: ArgRequired, Ops: allOps, Key: "config"},
	{Long: "confirm", Ops: allOps, Key: "confirm"},
	{Long: "debug", Mode: ArgOptional, Ops: allOps, Key: "debug"},
	{Long: "disable-download-timeout", Ops: allOps, Key: "disable-download-timeout"},
	{Long: "disable-sandbox", Ops: allOps, Key: "disable-sandbox"},
	{Long: "disable-sandbox-filesystem", Ops: allOps, Key: "disable-sandbox-filesystem"},
	{Long: "disable-sandbox-syscalls", Ops: allOps, Key: "disable-sandbox-syscalls"},
	{Long: "gpgdir", Mode: ArgRequired, Ops: allOps, Key: "gpgdir"},
	{Long: "hookdir", Mode: ArgRequired, Ops: allOps, Key: "hookdir"},
	{Long: "logfile", Mode: ArgRequired, Ops: allOps, Key: "logfile"},
	{Long: "noconfirm", Ops: allOps, Key: "noconfirm"},
	{Short: 'v', Long: "verbose", Ops: allOps, Key: "verbose"},
	{Short: 'f', Ops: 0, Key: "noop"},
	{Long: "force", Mode: ArgNone, Ops: 0, Key: "noop"},

	// query
	{Short: 'c', Long: "changelog", Ops: mask(OpQuery), Key: "changelog"},
	{Short: 'd', Long: "deps", Ops: mask(OpQuery), Key: "deps"},
	{Short: 'e', Long: "explicit", Ops: mask(OpQuery), Key: "explicit"},
	{Short: 'm', Long: "foreign", Ops: mask(OpQuery), Key: "foreign"},
	{Short: 'o', Long: "owns", Ops: mask(OpQuery), Key: "owns"},
	{Short: 't', Long: "unrequired", Ops: mask(OpQuery), Key: "unrequired"},
	{Short: 'u', Long: "upgrades", Ops: mask(OpQuery), Key: "upgrades"},

	// query/sync shared
	{Short: 'g', Long: "groups", Ops: mask(OpQuery, OpSync), Key: "groups"},
	{Short: 'i', Long: "info", Ops: mask(OpQuery, OpSync), Key: "info"},
	{Short: 's', Long: "search", Ops: mask(OpQuery, OpSync), Key: "search"},

	// database/query
	{Short: 'k', Long: "check", Ops: mask(OpDatabase, OpQuery), Key: "check"},

	// files/query/sync
	{Short: 'l', Long: "list", Ops: mask(OpFiles, OpQuery, OpSync), Key: "list"},

	// database/files/query/sync
	{Short: 'q', Long: "quiet", Ops: mask(OpDatabase, OpFiles, OpQuery, OpSync), Key: "quiet"},

	// query/remove
	{Short: 'n', Long: "native", Ops: mask(OpQuery), Key: "native"},
	{Short: 'n', Long: "nosave", Ops: mask(OpRemove), Key: "nosave"},

	// files/query/remove/sync/upgrade
	{Short: 'p', Ops: mask(OpQuery), Key: "file"},
	{Long: "file", Ops: mask(OpQuery), Key: "file"},
	{Short: 'p', Ops: mask(OpFiles, OpRemove, OpSync, OpUpgrade), Key: "print"},
	{Long: "print", Ops: mask(OpFiles, OpRemove, OpSync, OpUpgrade), Key: "print"},

	// remove/sync
	{Short: 's', Long: "recursive", Ops: mask(OpRemove), Key: "recursive"},
	{Short: 'c', Long: "cascade", Ops: mask(OpRemove), Key: "cascade"},
	{Short: 'u', Long: "unneeded", Ops: mask(OpRemove), Key: "unneeded"},

	// sync
	{Short: 'c', Long: "clean", Ops: mask(OpSync), Key: "clean"},
	{Short: 'u', Long: "sysupgrade", Ops: mask(OpSync), Key: "sysupgrade"},
	{Short: 'y', Long: "refresh", Ops: mask(OpFiles, OpSync), Key: "refresh"},
	{Long: "asdeps", Ops: mask(OpDatabase, OpSync, OpUpgrade), Key: "asdeps"},
	{Long: "asexplicit", Ops: mask(OpDatabase, OpSync, OpUpgrade), Key: "asexplicit"},
	{Long: "needed", Ops: mask(OpSync, OpUpgrade), Key: "needed"},
	{Long: "ignore", Mode: ArgRequired, Ops: mask(OpSync, OpUpgrade), Key: "ignore"},
	{Long: "ignoregroup", Mode: ArgRequired, Ops: mask(OpSync, OpUpgrade), Key: "ignoregroup"},
	{Long: "overwrite", Mode: ArgRequired, Ops: mask(OpSync, OpUpgrade), Key: "overwrite"},
	{Short: 'w', Long: "downloadonly", Ops: mask(OpSync, OpUpgrade), Key: "downloadonly"},

	// files
	{Short: 'x', Long: "regex", Ops: mask(OpFiles), Key: "regex"},
	{Long: "machinereadable", Ops: mask(OpFiles), Key: "machinereadable"},

	// transaction options common to -F -R -S -U
	{Short: 'd', Long: "nodeps", Ops: mask(OpFiles, OpRemove, OpSync, OpUpgrade), Key: "nodeps"},
	{Long: "dbonly", Ops: mask(OpFiles, OpRemove, OpSync, OpUpgrade), Key: "dbonly"},
	{Long: "noscriptlet", Ops: mask(OpFiles, OpRemove, OpSync, OpUpgrade), Key: "noscriptlet"},
	{Long: "noprogressbar", Ops: mask(OpFiles, OpRemove, OpSync, OpUpgrade), Key: "noprogressbar"},
	{Long: "print-format", Mode: ArgRequired, Ops: mask(OpFiles, OpRemove, OpSync, OpUpgrade), Key: "print-format"},
	{Long: "assume-installed", Mode: ArgRequired, Ops: mask(OpFiles, OpRemove, OpSync, OpUpgrade), Key: "assume-installed"},
}

func findShort(r rune) []*OptDef {
	var out []*OptDef
	for i := range optTable {
		if optTable[i].Short == r {
			out = append(out, &optTable[i])
		}
	}
	return out
}

// longNameOrder is pacman's option table order; getopt_long reports
// ambiguous prefixes in this order.
var longNameOrder = []string{
	"database", "files", "query", "remove", "sync", "deptest", "upgrade",
	"version", "help", "dbpath", "cascade", "changelog", "clean", "nodeps",
	"deps", "explicit", "groups", "info", "check", "list", "foreign",
	"native", "nosave", "owns", "file", "print", "quiet", "root", "sysroot",
	"recursive", "search", "regex", "machinereadable", "unrequired",
	"upgrades", "sysupgrade", "unneeded", "verbose", "downloadonly",
	"refresh", "noconfirm", "confirm", "config", "ignore", "assume-installed",
	"debug", "force", "overwrite", "noprogressbar", "noscriptlet", "ask",
	"cachedir", "hookdir", "asdeps", "logfile", "ignoregroup", "needed",
	"asexplicit", "arch", "print-format", "gpgdir", "dbonly", "color",
	"disable-download-timeout", "disable-sandbox",
	"disable-sandbox-filesystem", "disable-sandbox-syscalls",
}

func findLong(name string) []*OptDef {
	if name == "" {
		return nil
	}
	var out []*OptDef
	for i := range optTable {
		if optTable[i].Long == name {
			out = append(out, &optTable[i])
		}
	}
	return out
}

func longNames() []string { return longNameOrder }
