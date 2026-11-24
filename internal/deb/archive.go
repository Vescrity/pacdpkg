package deb

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
	"github.com/ulikunitz/xz"

	"pacdpkg/internal/alpm"
)

// debArchive is a .deb file opened without dpkg-deb.
type debArchive struct {
	control map[string][]byte
	files   []string
}

func openDeb(path string) (*debArchive, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("could not find or read package")
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, errors.New("could not find or read package")
	}

	br := bufio.NewReaderSize(f, 1<<16)
	magic := make([]byte, 8)
	if _, err := io.ReadFull(br, magic); err != nil || string(magic) != "!<arch>\n" {
		return nil, errors.New("could not find or read package")
	}

	ar := &debArchive{control: map[string][]byte{}}
	_ = st
	for {
		header := make([]byte, 60)
		if _, err := io.ReadFull(br, header); err != nil {
			break
		}
		if string(header[58:60]) != "`\n" {
			return nil, fmt.Errorf("could not find or read package")
		}
		name := strings.TrimRight(strings.TrimSpace(string(header[0:16])), "/")
		size, err := strconv.ParseInt(strings.TrimSpace(string(header[48:58])), 10, 64)
		if err != nil || size < 0 {
			return nil, fmt.Errorf("could not find or read package")
		}
		body := io.LimitReader(br, size)
		switch {
		case strings.HasPrefix(name, "control.tar"):
			if err := ar.readControl(name, body); err != nil {
				return nil, err
			}
		case strings.HasPrefix(name, "data.tar"):
			if err := ar.readData(name, body); err != nil {
				return nil, err
			}
		}
		io.Copy(io.Discard, body)
		if size%2 == 1 {
			if _, err := br.Discard(1); err != nil {
				break
			}
		}
	}
	if ar.control["control"] == nil {
		return nil, fmt.Errorf("could not find or read package")
	}
	return ar, nil
}

func (ar *debArchive) readControl(name string, body io.Reader) error {
	r, err := decompress(name, body)
	if err != nil {
		return err
	}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		if name != "control" {
			ar.control[name] = []byte{}
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return err
		}
		ar.control[name] = data
	}
}

func (ar *debArchive) readData(name string, body io.Reader) error {
	r, err := decompress(name, body)
	if err != nil {
		return err
	}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		name := hdr.Name
		if strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			return fmt.Errorf("invalid file name in package: %s", name)
		}
		entry := strings.TrimPrefix(name, ".")
		if entry == "/" || entry == "" {
			continue
		}
		ar.files = append(ar.files, entry)
	}
}

func decompress(name string, body io.Reader) (io.Reader, error) {
	switch {
	case strings.HasSuffix(name, ".gz"):
		return gzip.NewReader(body)
	case strings.HasSuffix(name, ".xz"):
		return xz.NewReader(body)
	case strings.HasSuffix(name, ".zst"):
		return zstd.NewReader(body)
	case strings.HasSuffix(name, ".bz2"):
		return bzip2.NewReader(body), nil
	case strings.HasSuffix(name, ".lz4"):
		return lz4.NewReader(body), nil
	case strings.HasSuffix(name, ".tar"):
		return body, nil
	}
	return nil, fmt.Errorf("unsupported compression: %s", name)
}

func parseDebControl(data []byte) (*alpm.Pkg, error) {
	p, err := parseControl(string(data))
	if err != nil {
		return nil, err
	}
	return p, nil
}

// controlField returns a raw control field of the archive.
func (ar *debArchive) controlField(name string) string {
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(ar.control["control"]))
	sc.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(key) == name {
			out.WriteString(strings.TrimSpace(value))
			break
		}
	}
	return out.String()
}
