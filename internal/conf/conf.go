package conf

import (
	"bufio"
	"os"
	"strings"
)

// Config holds the configuration options that affect pacdpkg.
type Config struct {
	RootDir         string
	DBPath          string
	CacheDir        string
	Architecture    string
	IgnorePkg       []string
	IgnoreGroup     []string
	HoldPkg         []string
	VerbosePkgLists bool
	NoProgressBar   bool
	CheckSpace      bool
	Color           *bool
}

// Load reads a pacman-style configuration file. With required set, an unreadable
// file is an error (an explicit --config); otherwise defaults are used.
func Load(path string, required bool) (*Config, error) {
	cfg := &Config{}
	f, err := os.Open(path)
	if err != nil {
		if required {
			return nil, err
		}
		return cfg, nil
	}
	defer f.Close()

	inOptions := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inOptions = strings.EqualFold(line, "[options]")
			continue
		}
		if !inOptions {
			continue
		}
		key, value, _ := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "RootDir":
			cfg.RootDir = value
		case "DBPath":
			cfg.DBPath = value
		case "CacheDir":
			cfg.CacheDir = value
		case "Architecture":
			cfg.Architecture = value
		case "IgnorePkg":
			cfg.IgnorePkg = append(cfg.IgnorePkg, strings.Fields(value)...)
		case "IgnoreGroup":
			cfg.IgnoreGroup = append(cfg.IgnoreGroup, strings.Fields(value)...)
		case "HoldPkg":
			cfg.HoldPkg = append(cfg.HoldPkg, strings.Fields(value)...)
		case "VerbosePkgLists":
			cfg.VerbosePkgLists = true
		case "NoProgressBar":
			cfg.NoProgressBar = true
		case "CheckSpace":
			cfg.CheckSpace = true
		case "Color":
			on := true
			cfg.Color = &on
		}
	}
	return cfg, nil
}
