package conf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pacdpkg.conf")
	body := "[options]\nVerbosePkgLists\nIgnorePkg = foo bar\nCacheDir = /tmp/cache\nColor\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.VerbosePkgLists {
		t.Error("VerbosePkgLists not enabled")
	}
	if cfg.CacheDir != "/tmp/cache" {
		t.Errorf("CacheDir = %q", cfg.CacheDir)
	}
	if len(cfg.IgnorePkg) != 2 || cfg.IgnorePkg[0] != "foo" || cfg.IgnorePkg[1] != "bar" {
		t.Errorf("IgnorePkg = %v", cfg.IgnorePkg)
	}
	if cfg.Color == nil || !*cfg.Color {
		t.Error("Color not enabled")
	}
}
