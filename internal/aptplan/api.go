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
	Needed        bool
	Purge         bool
	Recursive     int
	Cascade       bool
	ForceDeps     bool
	Downgrade     bool
	Lock          bool
	UpgradeOnly   bool
	AsDeps        bool
	AsExplicit    bool
	OverrideHeld  bool
	IgnorePkgs    []string
	IgnoreGroups  []string
	Arch          string
	Cachedir      string
	Architectures []string
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
	if o.AsDeps {
		f |= C.APT_FLAG_ASDEPS
	}
	if o.AsExplicit {
		f |= C.APT_FLAG_ASEXPLICIT
	}
	if o.OverrideHeld {
		f |= C.APT_FLAG_OVERRIDE_HELD
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
	ignp := newCStringArray(len(o.IgnorePkgs))
	igng := newCStringArray(len(o.IgnoreGroups))
	archs := newCStringArray(len(o.Architectures))
	defer func() {
		freeCStringArray(names, n)
		freeCStringArray(versions, n)
		freeCStringArray(repos, n)
		freeCStringArray(debs, n)
		freeCStringArray(ignp, len(o.IgnorePkgs))
		freeCStringArray(igng, len(o.IgnoreGroups))
		freeCStringArray(archs, len(o.Architectures))
	}()
	for i, t := range targets {
		setCString(names, i, t.Name)
		setCString(versions, i, t.Version)
		setCString(repos, i, t.Repo)
		setCString(debs, i, t.Deb)
	}

	for i, pat := range o.IgnorePkgs {
		setCString(ignp, i, pat)
	}
	for i, pat := range o.IgnoreGroups {
		setCString(igng, i, pat)
	}
	for i, arch := range o.Architectures {
		setCString(archs, i, arch)
	}
	arch := C.CString(o.Arch)
	cachedir := C.CString(o.Cachedir)
	defer func() {
		C.free(unsafe.Pointer(arch))
		C.free(unsafe.Pointer(cachedir))
	}()
	req := C.AptRequest{
		op:             C.int(op),
		flags:          flags(o),
		ntargets:       C.int(n),
		nignorepkgs:    C.int(len(o.IgnorePkgs)),
		nignoregroups:  C.int(len(o.IgnoreGroups)),
		narchitectures: C.int(len(o.Architectures)),
		arch:           arch,
		cachedir:       cachedir,
	}
	if n > 0 {
		req.names = names
		req.versions = versions
		req.repos = repos
		req.debs = debs
	}
	if len(o.IgnorePkgs) > 0 {
		req.ignorepkgs = ignp
	}
	if len(o.IgnoreGroups) > 0 {
		req.ignoregroups = igng
	}
	if len(o.Architectures) > 0 {
		req.architectures = archs
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

// DepSpec is one pacman-style dependency test target.
type DepSpec struct {
	Name    string
	Op      string
	Version string
}

type wireList struct {
	Error    string   `json:"error"`
	Missing  []string `json:"missing"`
	Problems []string `json:"problems"`
	Orphans  []string `json:"orphans"`
}

// CheckDeps reports which dependency targets are not satisfied by installed
// packages.
func CheckDeps(specs []DepSpec) ([]string, error) {
	n := len(specs)
	names := newCStringArray(n)
	ops := newCStringArray(n)
	versions := newCStringArray(n)
	defer func() {
		freeCStringArray(names, n)
		freeCStringArray(ops, n)
		freeCStringArray(versions, n)
	}()
	for i, spec := range specs {
		setCString(names, i, spec.Name)
		setCString(ops, i, spec.Op)
		setCString(versions, i, spec.Version)
	}
	q := C.AptDepQuery{ntargets: C.int(n)}
	if n > 0 {
		q.names = names
		q.ops = ops
		q.versions = versions
	}
	return callList(C.aptplan_checkdeps(&q))
}

// CheckDB reports installed packages with unsatisfied dependencies.
func CheckDB() ([]string, error) {
	return callList(C.aptplan_checkdb())
}

// Orphans returns packages installed as dependencies that are no longer
// required. With ignoreOptdeps, Recommends and Suggests are not considered.
func Orphans(ignoreOptdeps bool) (map[string]bool, error) {
	list, err := callList(C.aptplan_orphans(C.int(boolInt(ignoreOptdeps))))
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(list))
	for _, name := range list {
		out[name] = true
	}
	return out, nil
}

func callList(res *C.char) ([]string, error) {
	if res == nil {
		return nil, fmt.Errorf("libapt returned no response")
	}
	defer C.aptplan_free(res)
	var wire wireList
	if err := json.Unmarshal([]byte(C.GoString(res)), &wire); err != nil {
		return nil, fmt.Errorf("could not parse the libapt response: %v", err)
	}
	if wire.Error != "" {
		return nil, fmt.Errorf("%s", wire.Error)
	}
	if wire.Missing != nil {
		return wire.Missing, nil
	}
	if wire.Problems != nil {
		return wire.Problems, nil
	}
	return wire.Orphans, nil
}

type wireArch struct {
	Error string `json:"error"`
	Arch  string `json:"arch"`
}

// NativeArch returns libapt's configured native architecture.
func NativeArch() (string, error) {
	res := C.aptplan_nativearch()
	if res == nil {
		return "", fmt.Errorf("libapt returned no response")
	}
	defer C.aptplan_free(res)
	var wire wireArch
	if err := json.Unmarshal([]byte(C.GoString(res)), &wire); err != nil {
		return "", fmt.Errorf("could not parse the libapt response: %v", err)
	}
	if wire.Error != "" {
		return "", fmt.Errorf("%s", wire.Error)
	}
	return wire.Arch, nil
}
