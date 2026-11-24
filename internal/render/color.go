package render

// Color codes match pacman's src/pacman/conf.c enable_colors().
const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[0;1m"
	ansiRed     = "\033[1;31m"
	ansiGreen   = "\033[1;32m"
	ansiYellow  = "\033[1;33m"
	ansiBlue    = "\033[1;34m"
	ansiMagenta = "\033[1;35m"
	ansiCyan    = "\033[1;36m"
	ansiFaint   = "\033[38;5;243m"
)

var colorsEnabled bool

// EnableColors turns pacman-style coloring on or off.
func EnableColors(on bool) { colorsEnabled = on }

func ColorsEnabled() bool { return colorsEnabled }

func paint(code, s string) string {
	if !colorsEnabled || s == "" {
		return s
	}
	return code + s + ansiReset
}

func Bold(s string) string    { return paint(ansiBold, s) }
func Version(s string) string { return paint(ansiGreen, s) }
func Repo(s string) string    { return paint(ansiMagenta, s) }
func Meta(s string) string    { return paint(ansiCyan, s) }
func Group(s string) string   { return paint(ansiBlue, s) }
func Faint(s string) string   { return paint(ansiFaint, s) }
func Err(s string) string     { return paint(ansiRed, s) }
func Warn(s string) string    { return paint(ansiYellow, s) }

// ErrorPrefix and WarningPrefix mirror pacman's colored log levels.
func ErrorPrefix() string {
	if !colorsEnabled {
		return "error: "
	}
	return ansiRed + "error: " + ansiReset
}

func WarningPrefix() string {
	if !colorsEnabled {
		return "warning: "
	}
	return ansiYellow + "warning: " + ansiReset
}

// PromptStart returns pacman's ":: " prompt opener (blue colons, then bold).
func PromptStart() string {
	if !colorsEnabled {
		return ":: "
	}
	return ansiBlue + "::" + ansiBold + " "
}

// Reset terminates a prompt or colored block.
func Reset() string {
	if !colorsEnabled {
		return ""
	}
	return ansiReset
}
