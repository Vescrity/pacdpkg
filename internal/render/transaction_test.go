package render

import (
	"bytes"
	"testing"
)

// The expected outputs below are captured from pacman 7.1.0.

func TestTransactionClassicInstall(t *testing.T) {
	var b bytes.Buffer
	Transaction(&b, []TxPkg{{
		Name: "fakepkg", NewVersion: "1.0-1", InstalledSize: 6, Explicit: true,
	}}, false, false)
	want := "" +
		"Packages (1) fakepkg-1.0-1\n" +
		"\n" +
		"Total Installed Size:  0.00 MiB\n" +
		"\n"
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestTransactionVerboseRemoval(t *testing.T) {
	var b bytes.Buffer
	Transaction(&b, []TxPkg{{
		Name: "menu-cache", OldVersion: "1.1.1-2", InstalledSize: 262144,
	}}, true, false)
	want := "" +
		"Package (1)  Old Version  Net Change\n" +
		"\n" +
		"menu-cache   1.1.1-2       -0.25 MiB\n" +
		"\n" +
		"Total Removed Size:  0.25 MiB\n" +
		"\n"
	if b.String() != want {
		t.Errorf("got:\n%q\nwant:\n%q", b.String(), want)
	}
}

func TestTransactionClassicRemoval(t *testing.T) {
	var b bytes.Buffer
	Transaction(&b, []TxPkg{{
		Name: "arch-checkfw", OldVersion: "1.1.0-4", InstalledSize: 41943,
	}}, false, false)
	want := "" +
		"Packages (1) arch-checkfw-1.1.0-4\n" +
		"\n" +
		"Total Removed Size:  0.04 MiB\n" +
		"\n"
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

// pacman -U aalib-9.0-1 when aalib-1.4rc5-19 is installed.
func TestTransactionClassicUpgrade(t *testing.T) {
	var b bytes.Buffer
	Transaction(&b, []TxPkg{{
		Name: "aalib", OldVersion: "1.4rc5-19", NewVersion: "9.0-1",
		InstalledSize:    1_048_576,
		OldInstalledSize: 291_000,
	}}, false, false)
	want := "" +
		"Packages (1) aalib-9.0-1\n" +
		"\n" +
		"Total Installed Size:  1.00 MiB\n" +
		"Net Upgrade Size:      0.72 MiB\n" +
		"\n"
	if b.String() != want {
		t.Errorf("got:\n%q\nwant:\n%q", b.String(), want)
	}
}

func TestTransactionVerboseUpgrade(t *testing.T) {
	var b bytes.Buffer
	Transaction(&b, []TxPkg{{
		Name: "aalib", OldVersion: "1.4rc5-19", NewVersion: "9.0-1",
		InstalledSize:    1_048_576,
		OldInstalledSize: 291_000,
	}}, true, false)
	want := "" +
		"Package (1)  Old Version  New Version  Net Change\n" +
		"\n" +
		"aalib        1.4rc5-19    9.0-1          0.72 MiB\n" +
		"\n" +
		"Total Installed Size:  1.00 MiB\n" +
		"Net Upgrade Size:      0.72 MiB\n" +
		"\n"
	if b.String() != want {
		t.Errorf("got:\n%q\nwant:\n%q", b.String(), want)
	}
}
