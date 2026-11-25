package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"pacdpkg/internal/cli"
	"pacdpkg/internal/conf"
	"pacdpkg/internal/deb"
	"pacdpkg/internal/render"
)

const version = "0.1.0"

type App struct {
	prog    string
	cfg     *cli.Config
	conf    *conf.Config
	targets []string
}

func Errorf(format string, a ...any) {
	fmt.Fprint(os.Stderr, render.ErrorPrefix())
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}

func Warnf(format string, a ...any) {
	fmt.Fprint(os.Stderr, render.WarningPrefix())
	fmt.Fprintf(os.Stderr, format+"\n", a...)
}

func Run(argv []string) int {
	prog := filepath.Base(argv[0])
	tokens, targets, err := cli.Parse(prog, argv[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	cfg := &cli.Config{}
	for _, t := range tokens {
		switch t.Primary().Key {
		case "help":
			cfg.Help = true
			continue
		case "version":
			cfg.Version = true
			continue
		}
		op, ok := opFromKey(t.Primary().Key)
		if !ok {
			continue
		}
		if cfg.OpSet {
			Errorf("only one operation may be used at a time")
			return 1
		}
		cfg.Op, cfg.OpSet = op, true
	}

	if cfg.Help {
		fmt.Fprint(os.Stdout, cli.Usage(os.Stdout, prog, cfg.Op))
		return 0
	}
	if cfg.Version {
		backend := "dpkg"
		if v := deb.DpkgVersion(); v != "" {
			backend = "dpkg v" + v
		}
		cli.Version(os.Stdout, prog, version, backend)
		return 0
	}

	for _, t := range tokens {
		switch t.Primary().Key {
		case "help", "version":
			continue
		}
		if _, ok := opFromKey(t.Primary().Key); ok {
			continue
		}
		def := t.For(cfg.Op)
		if def == nil {
			raw := t.Raw
			if strings.HasPrefix(raw, "--") {
				raw = "--" + t.Primary().Long
			}
			Errorf("invalid option '%s'", raw)
			return 1
		}
		if err := cfg.Apply(def, t.Arg); err != nil {
			Errorf("%s", err)
			return 1
		}
	}

	if err := cfg.CheckArgs(); err != nil {
		Errorf("%s", err)
		return 1
	}

	for _, e := range []struct {
		set  bool
		name string
	}{
		{cfg.RootSet, "--root"},
		{cfg.DBPathSet, "--dbpath"},
		{cfg.SysRootSet, "--sysroot"},
		{cfg.DBOnly, "--dbonly"},
		{cfg.NoScriptlet, "--noscriptlet"},
		{len(cfg.AssumeInstalled) > 0, "--assume-installed"},
		{len(cfg.Overwrite) > 0, "--overwrite"},
	} {
		if e.set {
			Errorf("option '%s' is not supported with the deb backend", e.name)
			return 1
		}
	}

	if os.Geteuid() != 0 && cfg.NeedsRoot() {
		Errorf("you cannot perform this operation unless you are root.")
		return 1
	}

	confPath := cfg.ConfigFile
	var cfgConf *conf.Config
	if cfg.ConfigSet {
		cfgConf, err = conf.Load(confPath, true)
		if err != nil {
			Errorf("config file %s could not be read: %s", confPath, errText(err))
			return 1
		}
	} else {
		confPath = "/etc/pacdpkg.conf"
		cfgConf, err = conf.Load(confPath, false)
		if err != nil {
			Errorf("could not read configuration: %s", errText(err))
			return 1
		}
	}
	if cfgConf.RootDir != "" && cfgConf.RootDir != "/" {
		Errorf("RootDir in the config file is not supported with the deb backend")
		return 1
	}
	if cfgConf.DBPath != "" {
		Errorf("DBPath in the config file is not supported with the deb backend")
		return 1
	}
	if cfg.CacheDir == "" {
		cfg.CacheDir = cfgConf.CacheDir
	}
	if cfg.Arch == "" && cfgConf.Architecture != "" && cfgConf.Architecture != "auto" {
		cfg.Arch = cfgConf.Architecture
	}

	if cfg.CacheDirSet {
		if cfg.CacheDir == "" {
			Errorf("failed to resolve path '' passed to '--cachedir': No such file or directory")
			return 1
		}
		abs, err := filepath.Abs(cfg.CacheDir)
		if err != nil {
			Errorf("failed to resolve path '%s' passed to '--cachedir': %v", cfg.CacheDir, errText(err))
			return 1
		}
		if st, err := os.Stat(abs); err != nil || !st.IsDir() {
			Errorf("failed to resolve path '%s' passed to '--cachedir': No such file or directory", cfg.CacheDir)
			return 1
		}
		cfg.CacheDir = abs
	}
	if cfg.CacheDir != "" {
		deb.ExtraArgs = append(deb.ExtraArgs, "-o", "Dir::Cache::archives="+cfg.CacheDir)
	}
	if cfg.Arch != "" {
		deb.ExtraArgs = append(deb.ExtraArgs, "-o", "APT::Architecture="+cfg.Arch)
	}

	targets, err = readStdinTargets(targets)
	if err != nil {
		Errorf("%s", err)
		return 1
	}

	render.EnableColors(colorEnabled(cfg, cfgConf))
	if cfg.Verbose > 0 {
		verboseInfo(cfgConf, confPath, targets)
	}
	a := &App{prog: prog, cfg: cfg, conf: cfgConf, targets: targets}
	switch cfg.Op {
	case cli.OpQuery:
		return a.query()
	case cli.OpSync:
		return a.sync()
	case cli.OpRemove:
		return a.remove()
	case cli.OpUpgrade:
		return a.upgrade()
	case cli.OpDeptest:
		return a.deptest()
	case cli.OpDatabase:
		return a.database()
	case cli.OpFiles:
		return a.files()
	case cli.OpMain:
		Errorf("no operation specified (use -h for help)")
		return 1
	}
	Errorf("operation '%s' is not implemented by pacdpkg yet", cfg.Op)
	return 1
}

// verboseInfo mirrors pacman -v's configuration banner.
func verboseInfo(c *conf.Config, confPath string, targets []string) {
	root := c.RootDir
	if root == "" {
		root = "/"
	}
	cache := "/var/cache/apt/archives/"
	if c.CacheDir != "" {
		cache = c.CacheDir
	}
	fmt.Fprintf(os.Stdout, "%-10s: %s\n", "Root", root)
	fmt.Fprintf(os.Stdout, "%-10s: %s\n", "Conf File", confPath)
	fmt.Fprintf(os.Stdout, "%-10s: %s\n", "DB Path", "/var/lib/dpkg/")
	fmt.Fprintf(os.Stdout, "%-10s: %s  \n", "Cache Dirs", cache)
	fmt.Fprintf(os.Stdout, "%-10s: %s\n", "Hook Dirs", "")
	fmt.Fprintf(os.Stdout, "%-10s: %s\n", "Lock File", "/var/lib/dpkg/lock-frontend")
	fmt.Fprintf(os.Stdout, "%-10s: %s\n", "Log File", "/var/log/apt/history.log")
	fmt.Fprintf(os.Stdout, "%-10s: %s\n", "GPG Dir", "/etc/apt/trusted.gpg.d/")
	if len(targets) > 0 {
		fmt.Fprintf(os.Stdout, "%-10s: %s\n", "Targets", strings.Join(targets, "  "))
	}
}

func opFromKey(key string) (cli.Op, bool) {
	switch key {
	case "op-D":
		return cli.OpDatabase, true
	case "op-F":
		return cli.OpFiles, true
	case "op-Q":
		return cli.OpQuery, true
	case "op-R":
		return cli.OpRemove, true
	case "op-S":
		return cli.OpSync, true
	case "op-T":
		return cli.OpDeptest, true
	case "op-U":
		return cli.OpUpgrade, true
	}
	return 0, false
}

// readStdinTargets implements pacman's "-" target: read one target per line.
func readStdinTargets(targets []string) ([]string, error) {
	idx := -1
	for i, t := range targets {
		if t == "-" {
			idx = i
			break
		}
	}
	if idx < 0 {
		return targets, nil
	}
	if isTerminal(os.Stdin) {
		return nil, fmt.Errorf("argument '-' specified without input on stdin")
	}
	data, err := os.ReadFile("/dev/stdin")
	if err != nil {
		return nil, fmt.Errorf("failed to read arguments from stdin: (%s)", errText(err))
	}
	out := append([]string{}, targets[:idx]...)
	out = append(out, targets[idx+1:]...)
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		out = append(out, line)
		found = true
	}
	if !found {
		return nil, fmt.Errorf("argument '-' specified with empty stdin")
	}
	return out, nil
}

// errText unwraps path/syscall wrappers so messages read like strerror.
func errText(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		err = pe.Err
	}
	var se *os.SyscallError
	if errors.As(err, &se) {
		err = se.Err
	}
	return capitalize(err.Error())
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func colorEnabled(cfg *cli.Config, c *conf.Config) bool {
	auto := func() bool {
		st, err := os.Stdout.Stat()
		return err == nil && st.Mode()&os.ModeCharDevice != 0
	}
	switch cfg.Color {
	case "always":
		return true
	case "never":
		return false
	case "auto":
		return auto()
	}
	return auto() && c.Color != nil && *c.Color
}

func isTerminal(f *os.File) bool {
	var termios syscall.Termios
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TCGETS),
		uintptr(unsafe.Pointer(&termios)), 0, 0, 0)
	return errno == 0
}
