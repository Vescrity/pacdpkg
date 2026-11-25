package aptplan

/*
#cgo CXXFLAGS: -std=c++17
#cgo LDFLAGS: -lstdc++
#include <stdlib.h>
#include "bridge.h"
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"pacdpkg/internal/alpm"
	"pacdpkg/internal/deb"
)

// Op selects the transaction the resolver should compute.
type Op int

const (
	OpInstall Op = iota
	OpRemove
	OpUpgrade
)

// Target is one command-line package target.
type Target struct {
	Name    string
	Version string
	Repo    string
	Deb     string
}

// Options carry the operation flags that change the plan.
type Options struct {
	Needed      bool
	Purge       bool
	Recursive   int
	Cascade     bool
	ForceDeps   bool
	Downgrade   bool
	Lock        bool
	UpgradeOnly bool
}

// Result is a resolved plan plus the libapt session that can execute it.
type Result struct {
	Plan     *deb.Plan
	Problems []string
	Session  uintptr
}

type wireResponse struct {
	Error    string       `json:"error"`
	Problems []string     `json:"problems"`
	Actions  []wireAction `json:"actions"`
}

type wireAction struct {
	Name      string `json:"name"`
	Arch      string `json:"arch"`
	Old       string `json:"old"`
	New       string `json:"new"`
	Kind      string `json:"kind"`
	Repo      string `json:"repo"`
	Site      string `json:"site"`
	Filename  string `json:"filename"`
	Deb       string `json:"deb"`
	Download  uint64 `json:"download"`
	Installed uint64 `json:"installed"`
	Explicit  bool   `json:"explicit"`
	Purge     bool   `json:"purge"`
	Reason    string `json:"reason"`
	Other     string `json:"other"`
	OtherRepo string `json:"otherrepo"`
}

func flags(o Options) C.uint {
	var f C.uint
	if o.Needed {
		f |= C.APT_FLAG_NEEDED
	}
	if o.Purge {
		f |= C.APT_FLAG_PURGE
	}
	if o.Recursive > 0 {
		f |= C.APT_FLAG_RECURSIVE
	}
	if o.Cascade {
		f |= C.APT_FLAG_CASCADE
	}
	if o.ForceDeps {
		f |= C.APT_FLAG_FORCE_DEPS
	}
	if o.Downgrade {
		f |= C.APT_FLAG_DOWNGRADE
	}
	if o.Lock {
		f |= C.APT_FLAG_LOCK
	}
	if o.UpgradeOnly {
		f |= C.APT_FLAG_UPGRADE_ONLY
	}
	return f
}

var (
	sessions    sync.Map
	nextSession atomic.Uint64
)

// Resolve computes a transaction with libapt. The returned session owns the
// apt cache and its locks and must be closed with Close.
func Resolve(op Op, targets []Target, o Options) (*Result, error) {
	n := len(targets)
	names := newCStringArray(n)
	versions := newCStringArray(n)
	repos := newCStringArray(n)
	debs := newCStringArray(n)
	defer func() {
		freeCStringArray(names, n)
		freeCStringArray(versions, n)
		freeCStringArray(repos, n)
		freeCStringArray(debs, n)
	}()
	for i, t := range targets {
		setCString(names, i, t.Name)
		setCString(versions, i, t.Version)
		setCString(repos, i, t.Repo)
		setCString(debs, i, t.Deb)
	}

	req := C.AptRequest{
		op:       C.int(op),
		flags:    flags(o),
		ntargets: C.int(n),
	}
	if n > 0 {
		req.names = names
		req.versions = versions
		req.repos = repos
		req.debs = debs
	}

	var sess *C.AptSession
	res := C.aptplan_resolve(&req, &sess)
	if res == nil {
		return nil, fmt.Errorf("libapt returned no response")
	}
	defer C.aptplan_free(res)

	var wire wireResponse
	if err := json.Unmarshal([]byte(C.GoString(res)), &wire); err != nil {
		if sess != nil {
			C.aptplan_close(sess)
		}
		return nil, fmt.Errorf("could not parse the libapt response: %v", err)
	}
	if len(wire.Problems) > 0 {
		if sess != nil {
			C.aptplan_close(sess)
		}
		return &Result{Problems: wire.Problems}, nil
	}
	if wire.Error != "" {
		if sess != nil {
			C.aptplan_close(sess)
		}
		return nil, fmt.Errorf("%s", wire.Error)
	}

	id := uintptr(nextSession.Add(1))
	sessions.Store(id, sess)
	out := &Result{Session: id}
	plan := &deb.Plan{Session: out.Session, Removals: map[string]deb.RemovalInfo{}}
	for _, a := range wire.Actions {
		switch a.Kind {
		case "remove", "purge":
			plan.Remove = append(plan.Remove, &alpm.Pkg{
				Name:          a.Name,
				Version:       a.Old,
				InstalledSize: int64(a.Installed),
			})
			plan.Removals[a.Name] = deb.RemovalInfo{
				Reason: a.Reason,
				Other:  a.Other,
				Repo:   a.OtherRepo,
			}
		default:
			p := &deb.PlannedPkg{
				Name:       a.Name,
				OldVersion: a.Old,
				NewVersion: a.New,
				Repo:       a.Repo,
				Explicit:   a.Explicit,
				Reinstall:  a.Kind == "reinstall",
				DebPath:    a.Deb,
				URL:        actionURL(a),
			}
			p.Sync = &deb.SyncPkg{
				Name:          a.Name,
				Version:       a.New,
				Arch:          a.Arch,
				Repo:          a.Repo,
				Filename:      a.Filename,
				DownloadSize:  int64(a.Download),
				InstalledSize: int64(a.Installed),
			}
			plan.Install = append(plan.Install, p)
		}
	}
	out.Plan = plan
	return out, nil
}

func actionURL(a wireAction) string {
	if a.Deb != "" {
		return a.Deb
	}
	if a.Site != "" && a.Filename != "" {
		return "https://" + a.Site + "/" + a.Filename
	}
	return a.Filename
}

// Execute downloads and installs the session's plan. With downloadOnly set,
// only the archives are fetched.
func Execute(session uintptr, downloadOnly bool) error {
	v, ok := sessions.Load(session)
	if !ok {
		return fmt.Errorf("no libapt session")
	}
	res := C.aptplan_execute(v.(*C.AptSession), C.int(boolInt(downloadOnly)))
	if res == nil {
		return fmt.Errorf("libapt returned no response")
	}
	defer C.aptplan_free(res)

	var wire wireResponse
	if err := json.Unmarshal([]byte(C.GoString(res)), &wire); err != nil {
		return fmt.Errorf("could not parse the libapt response: %v", err)
	}
	if wire.Error != "" {
		return fmt.Errorf("%s", wire.Error)
	}
	return nil
}

// Close releases a session and any locks it holds.
func Close(session uintptr) {
	if session == 0 {
		return
	}
	if v, ok := sessions.LoadAndDelete(session); ok {
		C.aptplan_close(v.(*C.AptSession))
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func newCStringArray(n int) **C.char {
	if n == 0 {
		return nil
	}
	return (**C.char)(C.malloc(C.size_t(n) * C.size_t(unsafe.Sizeof((*C.char)(nil)))))
}

func setCString(arr **C.char, i int, s string) {
	slice := unsafe.Slice(arr, i+1)
	slice[i] = C.CString(s)
}

func freeCStringArray(arr **C.char, n int) {
	if arr == nil {
		return
	}
	slice := unsafe.Slice(arr, n)
	for _, p := range slice {
		C.free(unsafe.Pointer(p))
	}
	C.free(unsafe.Pointer(arr))
}
