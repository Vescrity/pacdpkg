package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMoveAndRestoreLists(t *testing.T) {
	src := t.TempDir()
	backup := filepath.Join(t.TempDir(), "backup")

	for _, name := range []string{"a_Packages", "a_Release", "a_InRelease"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(src, "lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(src, "partial"), 0o755); err != nil {
		t.Fatal(err)
	}

	moved, err := moveListsFrom(src, backup)
	if err != nil || !moved {
		t.Fatalf("moveListsFrom = %v, %v", moved, err)
	}
	if _, err := os.Stat(filepath.Join(src, "lock")); err != nil {
		t.Errorf("lock should stay in place: %v", err)
	}
	if _, err := os.Stat(filepath.Join(src, "partial")); err != nil {
		t.Errorf("partial should stay in place: %v", err)
	}
	if _, err := os.Stat(filepath.Join(src, "a_Packages")); err == nil {
		t.Errorf("a_Packages should have been moved")
	}
	if _, err := os.Stat(filepath.Join(backup, "a_Packages")); err != nil {
		t.Errorf("backup missing a_Packages: %v", err)
	}

	// A failed refresh leaves a partial file behind; restore must replace it.
	if err := os.WriteFile(filepath.Join(src, "a_Packages"), []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	restoreListsFrom(src, backup)
	data, err := os.ReadFile(filepath.Join(src, "a_Packages"))
	if err != nil || string(data) != "a_Packages" {
		t.Fatalf("restore = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(src, "a_Release")); err != nil {
		t.Errorf("a_Release not restored: %v", err)
	}
	if _, err := os.Stat(backup); err == nil {
		t.Errorf("backup directory should be gone")
	}
}
