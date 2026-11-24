#!/bin/bash
# Compare parser diagnostics, help and version output of pacman and pacdpkg.
# Requires a real pacman (Arch development machine); exits non-zero on drift.
set -u
export LC_ALL=C

PACMAN=${PACMAN:-pacman}
PACDPKG=${PACDPKG:-./bin/pacdpkg}
fail=0

compare() {
	local desc="$1"
	shift
	"$PACMAN" "$@" >/tmp/oracle.out 2>/tmp/oracle.err
	local pacman_rc=$?
	"$PACDPKG" "$@" >/tmp/pacdpkg.out 2>/tmp/pacdpkg.err
	local pacdpkg_rc=$?

	if [ "$pacman_rc" -ne "$pacdpkg_rc" ]; then
		echo "FAIL $desc: rc $pacman_rc != $pacdpkg_rc"
		fail=1
	fi
	if ! diff -q <(sed "s/$PACMAN/pacdpkg/g" /tmp/oracle.out) /tmp/pacdpkg.out >/dev/null; then
		echo "FAIL $desc: stdout differs"
		diff <(sed "s/$PACMAN/pacdpkg/g" /tmp/oracle.out) /tmp/pacdpkg.out | head -5
		fail=1
	fi
	if ! diff -q <(sed "s/$PACMAN/pacdpkg/g" /tmp/oracle.err) /tmp/pacdpkg.err >/dev/null; then
		echo "FAIL $desc: stderr differs"
		diff <(sed "s/$PACMAN/pacdpkg/g" /tmp/oracle.err) /tmp/pacdpkg.err | head -5
		fail=1
	fi
}

compare "help main" -h
compare "help query" -Q -h
compare "help sync" -S -h
compare "help files" -F -h
compare "help upgrade" -U -h
compare "unknown short" -Z
compare "unknown long" --bogus
compare "ambiguous prefix" --q
compare "prefix abbreviation" --quer -h
compare "two operations" -Q -S
compare "repeated operation" -Q -Q
compare "invalid option for op" -Q --asdeps
compare "missing short arg" -b
compare "missing long arg" -Q --dbpath
compare "no operation" 

if [ "$fail" -eq 0 ]; then
	echo "all parser checks match"
fi
exit "$fail"
