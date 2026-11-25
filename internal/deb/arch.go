package deb

import (
	"os"
	"runtime"
	"strings"
)

const dpkgArchFile = "/var/lib/dpkg/arch"

// NativeArch returns dpkg's native architecture. The arch file is
// authoritative; runtime.GOARCH is the fallback when dpkg has not written it
// (for example in a minimal chroot).
func NativeArch() string {
	if arch := archFromDpkgArch(dpkgArchFile); arch != "" {
		return arch
	}
	return DebArch(runtime.GOARCH)
}

func archFromDpkgArch(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		arch := strings.TrimSpace(line)
		if arch == "" || arch == "all" {
			continue
		}
		return arch
	}
	return ""
}

// DebArch maps pacman and Go architecture names to Debian's names.
func DebArch(name string) string {
	switch name {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	case "i686", "i586", "i486", "i386", "386":
		return "i386"
	case "armv7l", "armv6l", "arm":
		return "armhf"
	case "mips64le":
		return "mips64el"
	case "mips64":
		return "mips64"
	case "mipsle":
		return "mipsel"
	case "ppc64le":
		return "ppc64el"
	}
	return name
}
