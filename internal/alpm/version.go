package alpm

import "strings"

// CompareVersion compares two Debian versions ([epoch:]upstream[-revision])
// with dpkg's algorithm and returns -1, 0 or 1.
func CompareVersion(a, b string) int {
	ae, au, ar := splitVersion(a)
	be, bu, br := splitVersion(b)

	switch {
	case ae > be:
		return 1
	case ae < be:
		return -1
	}
	if c := verrevcmp(au, bu); c != 0 {
		return sign(c)
	}
	return sign(verrevcmp(ar, br))
}

func splitVersion(v string) (epoch int64, upstream, revision string) {
	if i := strings.IndexByte(v, ':'); i >= 0 {
		var ok bool
		for _, c := range v[:i] {
			if c < '0' || c > '9' {
				ok = false
				break
			}
			ok = true
		}
		if ok {
			for _, c := range v[:i] {
				epoch = epoch*10 + int64(c-'0')
			}
			v = v[i+1:]
		}
	}
	if i := strings.LastIndexByte(v, '-'); i >= 0 {
		return epoch, v[:i], v[i+1:]
	}
	return epoch, v, ""
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}

func order(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return 0
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		return int(c)
	case c == '~':
		return -1
	case c != 0:
		return int(c) + 256
	}
	return 0
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func verrevcmp(a, b string) int {
	i, j := 0, 0
	at := func(s string, k int) byte {
		if k < len(s) {
			return s[k]
		}
		return 0
	}

	for at(a, i) != 0 || at(b, j) != 0 {
		firstDiff := 0

		for (at(a, i) != 0 && !isDigit(at(a, i))) || (at(b, j) != 0 && !isDigit(at(b, j))) {
			ac, bc := order(at(a, i)), order(at(b, j))
			if ac != bc {
				return ac - bc
			}
			i++
			j++
		}
		for at(a, i) == '0' {
			i++
		}
		for at(b, j) == '0' {
			j++
		}
		for isDigit(at(a, i)) && isDigit(at(b, j)) {
			if firstDiff == 0 {
				firstDiff = int(at(a, i)) - int(at(b, j))
			}
			i++
			j++
		}
		if isDigit(at(a, i)) {
			return 1
		}
		if isDigit(at(b, j)) {
			return -1
		}
		if firstDiff != 0 {
			return firstDiff
		}
	}
	return 0
}
