package cli

import (
	"fmt"
	"strconv"
)

type Config struct {
	Op    Op
	OpSet bool

	Help    bool
	Version bool

	DBPath     string
	Root       string
	SysRoot    string
	Arch       string
	CacheDir   string
	ConfigFile string
	HookDir    string
	LogFile    string
	GPGDir     string
	Color      string
	Ask        string

	NoConfirm     bool
	Debug         bool
	NoProgressBar bool
	NoScriptlet   bool
	DBOnly        bool
	Verbose       int
	Print         bool
	PrintFormat   string

	AsDeps          bool
	AsExplicit      bool
	Needed          bool
	DownloadOnly    bool
	IgnorePkg       []string
	IgnoreGroup     []string
	Overwrite       []string
	AssumeInstalled []string
	Nodeps          int

	Quiet      bool
	Group      int
	Search     bool
	Info       int
	Check      int
	Changelog  bool
	List       bool
	Deps       bool
	Explicit   bool
	Foreign    bool
	Native     bool
	Owns       bool
	IsFile     bool
	Unrequired int
	Upgrades   bool

	Recursive int
	Cascade   bool
	NoSave    bool
	Unneeded  bool

	Clean      int
	Refresh    int
	SysUpgrade int

	FileRegex       bool
	MachineReadable bool
}

// Apply interprets one option token for the selected operation.
func (c *Config) Apply(d *OptDef, arg string) error {
	switch d.Key {
	case "help":
		c.Help = true
	case "version":
		c.Version = true
	case "noop":
	case "dbpath":
		c.DBPath = arg
	case "root":
		c.Root = arg
	case "sysroot":
		c.SysRoot = arg
	case "arch":
		c.Arch = arg
	case "cachedir":
		c.CacheDir = arg
	case "config":
		c.ConfigFile = arg
	case "hookdir":
		c.HookDir = arg
	case "logfile":
		c.LogFile = arg
	case "gpgdir":
		c.GPGDir = arg
	case "color":
		switch arg {
		case "always", "never", "auto":
			c.Color = arg
		default:
			return fmt.Errorf("invalid argument '%s' for --color", arg)
		}
	case "ask":
		if _, err := strconv.Atoi(arg); err != nil {
			return fmt.Errorf("'%s' is not a valid ask level", arg)
		}
		c.Ask = arg
	case "confirm":
		c.NoConfirm = false
	case "noconfirm":
		c.NoConfirm = true
	case "debug":
		if arg != "" {
			level, err := strconv.Atoi(arg)
			if err != nil || (level != 1 && level != 2) {
				return fmt.Errorf("'%s' is not a valid debug level", arg)
			}
		}
		c.Debug = true
		c.NoProgressBar = true
	case "verbose":
		c.Verbose++
	case "noprogressbar":
		c.NoProgressBar = true
	case "noscriptlet":
		c.NoScriptlet = true
	case "dbonly":
		c.DBOnly = true
		c.NoScriptlet = true
	case "quiet":
		c.Quiet = true
	case "groups":
		c.Group++
	case "search":
		c.Search = true
	case "info":
		c.Info++
	case "check":
		c.Check++
	case "changelog":
		c.Changelog = true
	case "list":
		c.List = true
	case "deps":
		c.Deps = true
	case "explicit":
		c.Explicit = true
	case "foreign":
		c.Foreign = true
	case "native":
		c.Native = true
	case "owns":
		c.Owns = true
	case "file":
		c.IsFile = true
	case "unrequired":
		c.Unrequired++
	case "upgrades":
		c.Upgrades = true
	case "print":
		c.Print = true
	case "print-format":
		c.Print = true
		c.PrintFormat = arg
	case "asdeps":
		c.AsDeps = true
	case "asexplicit":
		c.AsExplicit = true
	case "needed":
		c.Needed = true
	case "downloadonly":
		c.DownloadOnly = true
	case "ignore":
		c.IgnorePkg = append(c.IgnorePkg, splitList(arg)...)
	case "ignoregroup":
		c.IgnoreGroup = append(c.IgnoreGroup, splitList(arg)...)
	case "overwrite":
		c.Overwrite = append(c.Overwrite, splitList(arg)...)
	case "assume-installed":
		c.AssumeInstalled = append(c.AssumeInstalled, splitList(arg)...)
	case "nodeps":
		c.Nodeps++
	case "cascade":
		c.Cascade = true
	case "nosave":
		c.NoSave = true
	case "recursive":
		c.Recursive++
	case "unneeded":
		c.Unneeded = true
	case "clean":
		c.Clean++
	case "refresh":
		c.Refresh++
	case "sysupgrade":
		c.SysUpgrade++
	case "regex":
		c.FileRegex = true
	case "machinereadable":
		c.MachineReadable = true
	}
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, part := range splitComma(s) {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func splitComma(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}

func invalidOpt(used bool, a, b string) error {
	if !used {
		return nil
	}
	return fmt.Errorf("invalid option: '%s' and '%s' may not be used together", a, b)
}

// CheckArgs reproduces pacman's cross-option validation.
func (c *Config) CheckArgs() error {
	switch c.Op {
	case OpQuery:
		return c.checkQueryArgs()
	case OpFiles:
		if c.FileRegex && c.List {
			return invalidOpt(true, "--regex", "--list")
		}
	case OpDatabase:
		if err := invalidOpt(c.AsDeps && c.AsExplicit, "--asdeps", "--asexplicit"); err != nil {
			return err
		}
		if c.Check > 0 {
			if err := invalidOpt(c.AsDeps, "--asdeps", "--check"); err != nil {
				return err
			}
			if err := invalidOpt(c.AsExplicit, "--asexplicit", "--check"); err != nil {
				return err
			}
		}
	case OpRemove:
		if err := c.checkTransArgs(); err != nil {
			return err
		}
		if c.NoSave {
			if err := invalidOpt(c.Print, "--nosave", "--print"); err != nil {
				return err
			}
			return invalidOpt(c.DBOnly, "--nosave", "--dbonly")
		}
	case OpUpgrade:
		if err := c.checkTransArgs(); err != nil {
			return err
		}
		return invalidOpt(c.AsDeps && c.AsExplicit, "--asdeps", "--asexplicit")
	case OpSync:
		return c.checkSyncArgs()
	}
	return nil
}

func (c *Config) checkTransArgs() error {
	if !c.Print {
		return nil
	}
	if err := invalidOpt(c.DBOnly, "--print", "--dbonly"); err != nil {
		return err
	}
	return invalidOpt(c.NoScriptlet, "--print", "--noscriptlet")
}

func (c *Config) checkSyncArgs() error {
	if err := invalidOpt(c.AsDeps && c.AsExplicit, "--asdeps", "--asexplicit"); err != nil {
		return err
	}
	switch {
	case c.Clean > 0:
		if err := invalidOpt(c.Group > 0, "--clean", "--groups"); err != nil {
			return err
		}
		if err := invalidOpt(c.Info > 0, "--clean", "--info"); err != nil {
			return err
		}
		if err := invalidOpt(c.List, "--clean", "--list"); err != nil {
			return err
		}
		if err := invalidOpt(c.Refresh > 0, "--clean", "--refresh"); err != nil {
			return err
		}
		if err := invalidOpt(c.Search, "--clean", "--search"); err != nil {
			return err
		}
		if err := invalidOpt(c.SysUpgrade > 0, "--clean", "--sysupgrade"); err != nil {
			return err
		}
		return invalidOpt(c.DownloadOnly, "--clean", "--downloadonly")
	case c.Info > 0:
		if err := invalidOpt(c.Group > 0, "--info", "--groups"); err != nil {
			return err
		}
		if err := invalidOpt(c.List, "--info", "--list"); err != nil {
			return err
		}
		if err := invalidOpt(c.Search, "--info", "--search"); err != nil {
			return err
		}
		if err := invalidOpt(c.SysUpgrade > 0, "--info", "--sysupgrade"); err != nil {
			return err
		}
		return invalidOpt(c.DownloadOnly, "--info", "--downloadonly")
	case c.Search:
		if err := invalidOpt(c.Group > 0, "--search", "--groups"); err != nil {
			return err
		}
		if err := invalidOpt(c.List, "--search", "--list"); err != nil {
			return err
		}
		if err := invalidOpt(c.SysUpgrade > 0, "--search", "--sysupgrade"); err != nil {
			return err
		}
		return invalidOpt(c.DownloadOnly, "--search", "--downloadonly")
	case c.List:
		if err := invalidOpt(c.Group > 0, "--list", "--groups"); err != nil {
			return err
		}
		if err := invalidOpt(c.SysUpgrade > 0, "--list", "--sysupgrade"); err != nil {
			return err
		}
		return invalidOpt(c.DownloadOnly, "--list", "--downloadonly")
	case c.Group > 0:
		if err := invalidOpt(c.SysUpgrade > 0, "--groups", "--sysupgrade"); err != nil {
			return err
		}
		return invalidOpt(c.DownloadOnly, "--groups", "--downloadonly")
	}
	return nil
}

// NeedsRoot reports whether the selected operation requires privileges.
func (c *Config) NeedsRoot() bool {
	switch c.Op {
	case OpDatabase:
		return c.Check == 0
	case OpUpgrade, OpRemove:
		return !c.Print
	case OpSync:
		return c.Clean > 0 || c.Refresh > 0 ||
			(c.Group == 0 && c.Info == 0 && !c.List && !c.Search && !c.Print)
	case OpFiles:
		return c.Refresh > 0
	}
	return false
}

func (c *Config) checkQueryArgs() error {
	display := func(opname string) error {
		for _, e := range []struct {
			used bool
			name string
		}{
			{c.Changelog, "--changelog"},
			{c.Check > 0, "--check"},
			{c.Info > 0, "--info"},
			{c.List, "--list"},
		} {
			if err := invalidOpt(e.used, opname, e.name); err != nil {
				return err
			}
		}
		return nil
	}
	filters := func(opname string) error {
		for _, e := range []struct {
			used bool
			name string
		}{
			{c.Deps, "--deps"},
			{c.Explicit, "--explicit"},
			{c.Upgrades, "--upgrade"},
			{c.Unrequired > 0, "--unrequired"},
			{c.Native, "--native"},
			{c.Foreign, "--foreign"},
		} {
			if err := invalidOpt(e.used, opname, e.name); err != nil {
				return err
			}
		}
		return nil
	}

	switch {
	case c.IsFile:
		for _, e := range []struct {
			used bool
			name string
		}{
			{c.Group > 0, "--groups"},
			{c.Search, "--search"},
			{c.Owns, "--owns"},
		} {
			if err := invalidOpt(e.used, "--file", e.name); err != nil {
				return err
			}
		}
	case c.Search:
		if err := invalidOpt(c.Group > 0, "--search", "--groups"); err != nil {
			return err
		}
		if err := invalidOpt(c.Owns, "--search", "--owns"); err != nil {
			return err
		}
		if err := display("--search"); err != nil {
			return err
		}
		if err := filters("--search"); err != nil {
			return err
		}
	case c.Owns:
		if err := invalidOpt(c.Group > 0, "--owns", "--groups"); err != nil {
			return err
		}
		if err := display("--owns"); err != nil {
			return err
		}
		if err := filters("--owns"); err != nil {
			return err
		}
	case c.Group > 0:
		if err := display("--groups"); err != nil {
			return err
		}
	}

	if err := invalidOpt(c.Deps && c.Explicit, "--deps", "--explicit"); err != nil {
		return err
	}
	return invalidOpt(c.Native && c.Foreign, "--native", "--foreign")
}
