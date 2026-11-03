#!/usr/bin/env python3
"""Offline regression of per-package benchmark output with mock executables."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile

COMPARE = Path(__file__).resolve().parent / "compare.py"

with tempfile.TemporaryDirectory(prefix="mutate-bench-test-") as tmp:
    root = Path(tmp)
    packages = [root / "fast.deb", root / "slow.deb"]
    for pkg in packages:
        pkg.write_bytes(b"synthetic benchmark input")
    for version in ("before", "after"):
        cli = root / version
        cli.write_text(
            "#!/bin/sh\n"
            'if [ "$1" = build ]; then\n'
            ' while [ "$#" -gt 0 ]; do\n'
            '  if [ "$1" = -o ]; then printf payload > "$2"; exit 0; fi\n'
            '  shift\n'
            ' done\n'
            ' exit 1\n'
            'fi\n'
            'exit 0\n'
        )
        cli.chmod(0o755)

    cmd = [
        sys.executable, str(COMPARE), "--before", str(root / "before"),
        "--after", str(root / "after"), "--runs", "3", "--warmup", "0",
        "--operations", "inspect", "build", *map(str, packages),
    ]
    completed = subprocess.run(cmd, text=True, capture_output=True, check=True)
    for expected in ("fast.deb", "slow.deb", "median package delta", "p95"):
        if expected not in completed.stdout:
            raise RuntimeError(f"missing benchmark detail {expected}: {completed.stdout}")
print("Benchmark runner regression checks passed")
