package conf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMerged(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.conf")
	b := filepath.Join(dir, "b.conf")
	if err := os.WriteFile(a, []byte("[options]\nVerbosePkgLists\nIgnorePkg = foo\nColor\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("[options]\nIgnorePkg = bar\nCacheDir = /tmp/cache\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadMerged(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.VerbosePkgLists {
		t.Error("VerbosePkgLists lost")
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
