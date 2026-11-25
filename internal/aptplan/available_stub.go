//go:build !libapt

package aptplan

const available = false

// Available reports whether this build can use libapt for resolution and
// execution. Builds with -tags libapt link the real library.
func Available() bool { return available }
