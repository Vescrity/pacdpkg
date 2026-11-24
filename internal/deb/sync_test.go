package deb

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/pierrec/lz4/v4"
)

func TestOpenList(t *testing.T) {
	body := "Package: foo\nVersion: 1\n\n"
	cases := []struct {
		name  string
		write func(io.Writer) (io.WriteCloser, error)
	}{
		{"x_Packages", nil},
		{"x_Packages.gz", func(w io.Writer) (io.WriteCloser, error) { return gzip.NewWriter(w), nil }},
		{"x_Packages.lz4", func(w io.Writer) (io.WriteCloser, error) { return lz4.NewWriter(w), nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.name)
			if tc.write == nil {
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			} else {
				f, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				zw, err := tc.write(f)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := io.WriteString(zw, body); err != nil {
					t.Fatal(err)
				}
				if err := zw.Close(); err != nil {
					t.Fatal(err)
				}
				f.Close()
			}
			rc, err := openList(path)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(rc)
			if err != nil {
				t.Fatal(err)
			}
			if err := rc.Close(); err != nil {
				t.Fatal(err)
			}
			if string(data) != body {
				t.Errorf("got %q, want %q", data, body)
			}
		})
	}
}
