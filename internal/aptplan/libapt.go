//go:build libapt

package aptplan

/*
#cgo CXXFLAGS: -DLIBAPT
#cgo LDFLAGS: -lapt-pkg
*/
import "C"

const available = true

// Available reports whether this build can use libapt.
func Available() bool { return available }
