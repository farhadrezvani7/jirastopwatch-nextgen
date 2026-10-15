#!/usr/bin/env python3
"""Fails if a macOS binary lacks the LC_UUID load command (current macOS refuses to run such binaries)."""
import struct, sys

bad = 0
for f in sys.argv[1:]:
    b = open(f, "rb").read()
    if struct.unpack("<I", b[:4])[0] != 0xFEEDFACF:
        print(f, "is not a 64-bit Mach-O file"); bad += 1; continue
    n = struct.unpack("<I", b[16:20])[0]; off = 32; cmds = []
    for _ in range(n):
        cmd, size = struct.unpack("<II", b[off:off + 8]); cmds.append(cmd); off += size
    ok = 0x1B in cmds
    print(f, "LC_UUID:", "ok" if ok else "MISSING")
    bad += not ok
sys.exit(1 if bad else 0)
