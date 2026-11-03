#!/usr/bin/env python3
"""Compare two mutate binaries using the same real package corpus.

Runs are interleaved to reduce order/cache bias. No packages are installed.
Requires only Python 3's standard library.
"""
import argparse
import math
import os
from pathlib import Path
import statistics
import subprocess
import sys
import tempfile
import time


def percentile(samples, pct):
    ordered = sorted(samples)
    return ordered[max(0, math.ceil(pct * len(ordered)) - 1)]


def command_for(binary, operation, package, target, output, extra):
    if operation == "inspect":
        return [binary, "inspect", package]
    if operation == "fast":
        return [binary, "inspect", package, "--fast"]
    if operation == "dry-run":
        return [binary, "install", package, "--dry-run"]
    return [binary, "build", package, "--to", target, "-o", output, *extra]


def run(binary, operation, package, target, extra):
    # Each invocation gets a fresh output, preventing a previous run from
    # benefiting from an already-created package.
    with tempfile.TemporaryDirectory(prefix="mutate-bench-") as work:
        out = os.path.join(work, "output." + ("pkg.tar.zst" if target == "arch" else target))
        argv = command_for(binary, operation, package, target, out, extra)
        started = time.perf_counter_ns()
        process = subprocess.run(
            argv,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.PIPE,
            check=False,
        )
        duration_ms = (time.perf_counter_ns() - started) / 1_000_000
        if process.returncode:
            message = process.stderr.decode(errors="replace")[-1500:]
            raise RuntimeError(f"{argv[0]} {operation} {Path(package).name} exited "
                               f"{process.returncode}:\n{message}")
        if operation == "build" and (not os.path.isfile(out) or not os.path.getsize(out)):
            raise RuntimeError(f"build did not produce an output: {package}")
        return duration_ms


def main():
    p = argparse.ArgumentParser(description="Interleaved before/after mutate performance benchmark")
    p.add_argument("--before", required=True, help="Earlier mutate executable")
    p.add_argument("--after", required=True, help="Optimized mutate executable")
    p.add_argument("--target", default="deb", choices=["deb", "rpm", "arch", "apk"])
    p.add_argument("--runs", type=int, default=15)
    p.add_argument("--warmup", type=int, default=2)
    p.add_argument("--operations", nargs="+", choices=["inspect", "fast", "build", "dry-run"],
                   default=["inspect", "build", "dry-run"])
    p.add_argument("--build-arg", action="append", default=[],
                   help="Forward one argument to 'build' (repeat as needed)")
    p.add_argument("packages", nargs="+", help="Existing package files to compare")
    args = p.parse_args()
    if args.runs < 3 or args.warmup < 0:
        p.error("--runs must be >= 3 and --warmup >= 0")
    binaries = [str(Path(args.before).resolve()), str(Path(args.after).resolve())]
    packages = [str(Path(pkg).resolve()) for pkg in args.packages]
    for file in [*binaries, *packages]:
        if not Path(file).is_file():
            p.error(f"missing file: {file}")
    for binary in binaries:
        if not os.access(binary, os.X_OK):
            p.error(f"not executable: {binary}")

    print(f"packages={len(packages)} runs={args.runs} warmup={args.warmup} "
          f"target={args.target}", flush=True)
    print("operation         before p50 / p95    after p50 / p95      p50 change")
    for operation in args.operations:
        previous, optimized = [], []
        for pkg in packages:
            for n in range(args.warmup + args.runs):
                # Alternate A/B order to reduce cold-cache and CPU-frequency bias.
                order = (0, 1) if n % 2 == 0 else (1, 0)
                for index in order:
                    measured = run(binaries[index], operation, pkg, args.target, args.build_arg)
                    if n >= args.warmup:
                        (previous if index == 0 else optimized).append(measured)
        before50, after50 = statistics.median(previous), statistics.median(optimized)
        before95, after95 = percentile(previous, .95), percentile(optimized, .95)
        delta = (after50 / before50 - 1) * 100 if before50 else float("nan")
        print(f"{operation:<16} {before50:8.2f} / {before95:8.2f} ms  "
              f"{after50:8.2f} / {after95:8.2f} ms   {delta:+8.1f}%", flush=True)


if __name__ == "__main__":
    try:
        main()
    except RuntimeError as exc:
        print(f"benchmark failed: {exc}", file=sys.stderr)
        sys.exit(1)
