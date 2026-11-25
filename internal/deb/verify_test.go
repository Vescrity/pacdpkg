package deb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMD5FileErrRejectsNonRegular(t *testing.T) {
	path := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink("/dev/zero", path); err != nil {
		t.Skipf("symlink: %v", err)
	}
	if _, err := md5FileErr(path); err == nil {
		t.Fatal("md5FileErr followed a non-regular file")
	}
}
