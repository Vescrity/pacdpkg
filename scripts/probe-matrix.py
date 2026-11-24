#!/usr/bin/env python3
"""Probe the option x operation validity matrix against a real pacman.

Runs every option token of pacman 7.1 for every operation (560 combinations)
against both pacman and pacdpkg and reports any classification difference.
A probe counts as INVALID when stderr contains "invalid option", which is
what pacman prints for options not accepted by the selected operation.

Usage: probe-matrix.py [pacman] [pacdpkg]
"""

import os
import shutil
import subprocess
import sys

OPS = "DFQRSTU"
ARGFOR = {
    "-b": "/tmp/x", "-r": "/tmp/x", "--dbpath": "/tmp/x", "--root": "/tmp/x",
    "--sysroot": "/tmp/x", "--config": "/tmp/x", "--cachedir": "/tmp/x",
    "--hookdir": "/tmp/x", "--logfile": "/tmp/x", "--gpgdir": "/tmp/x",
    "--ignore": "foo", "--ignoregroup": "foo", "--overwrite": "foo",
    "--assume-installed": "foo=1", "--print-format": "%n", "--ask": "4",
    "--color": "never", "--arch": "x86_64",
}
SHORTS = "b c d e f g i k l m n o p q r s t u v w x y".split()
LONGS = """dbpath cascade changelog clean nodeps deps explicit groups info check
list foreign native nosave owns file print quiet root sysroot recursive search
regex machinereadable unrequired upgrades sysupgrade unneeded verbose downloadonly
refresh noconfirm confirm config ignore assume-installed debug force overwrite
noprogressbar noscriptlet ask cachedir hookdir asdeps logfile ignoregroup needed
asexplicit arch print-format gpgdir dbonly color disable-download-timeout
disable-sandbox disable-sandbox-filesystem disable-sandbox-syscalls""".split()
TOKENS = ["-" + s for s in SHORTS] + ["--" + l for l in LONGS]
ENV = dict(os.environ, LC_ALL="C", LANG="C")


def classify(binary, token, op):
    args = [binary, "-" + op, token]
    if token in ARGFOR:
        args.append(ARGFOR[token])
    proc = subprocess.run(args, capture_output=True, text=True, env=ENV)
    return "INVALID" if "invalid option" in proc.stderr else "ok"


def main():
    pacman = sys.argv[1] if len(sys.argv) > 1 else "pacman"
    pacdpkg = sys.argv[2] if len(sys.argv) > 2 else shutil.which("pacdpkg") or "./bin/pacdpkg"
    diffs = 0
    for token in TOKENS:
        for op in OPS:
            want = classify(pacman, token, op)
            got = classify(pacdpkg, token, op)
            if want != got:
                diffs += 1
                print(f"DIFF {token} -{op}: pacman={want} pacdpkg={got}")
    total = len(TOKENS) * len(OPS)
    print(f"{total - diffs}/{total} agree")
    return 1 if diffs else 0


if __name__ == "__main__":
    sys.exit(main())
