# Benchmarking M.U.T.A.T.E.

Compare identical real packages on the same host using separate builds:

```sh
# Compile previously checked-in commits (from an existing repo checkout)
git worktree add /tmp/mutate-before ef57627
git worktree add /tmp/mutate-after HEAD
(cd /tmp/mutate-before && go build -o /tmp/mutate-before-bin ./cmd/mutate)
(cd /tmp/mutate-after && go build -o /tmp/mutate-after-bin ./cmd/mutate)

python3 benchmarks/compare.py \
  --before /tmp/mutate-before-bin --after /tmp/mutate-after-bin \
  --target deb --runs 15 --warmup 3 \
  --operations inspect build dry-run -- \
  /path/to/real-package-1.deb /path/to/real-package-2.deb
```

The script alternates execution order between versions, uses unique output files, reports **p50 and p95**, and aborts on conversion failures instead of silently skipping them. Choose packages that each binary can convert on the test host; cross-format dependencies may need `--build-arg=--dependency-map=...`. No package manager installation is performed.

To see where time goes, set `MUTATE_TIMINGS=1` on the **optimized** executable. This prints `detect`, `unpack`, `elf`, `dependencies`, `fhs`, `scripts` and `package` durations to stderr. Keep profiling disabled for the timing comparison so logging does not distort measurements.

Run benchmarks on the same filesystem, after building both binaries with the same Go version and compiler flags. Record machine, distribution, package corpus, CPU power policy, and repeats. A microbenchmark or CI pass does **not** establish end-to-end speed improvement.

## Investigating mixed-package latency

The comparator prints p50 and p95 for every package separately and the median percentage change across packages. The historical pooled median can shift when the workload contains a mix of cheap native inspections and expensive packages with large ELF payloads or provider lookups. Examine per-package changes before interpreting the pooled p50 as a regression.

Use `MUTATE_TIMINGS=1 mutate inspect package.deb` for the breakdown including cleanup. Then compare `mutate inspect package.deb --no-resolve` against the default to isolate the **host dependency resolver** from the archive extraction and ELF scan. Both paths still validate the package content; only the external provider lookup is skipped. No real-world speedup is claimed without rerunning the same ten-package corpus.

**CLI note:** `--operations` accepts several values. Add `--` before positional package paths to stop option parsing and prevent package paths being interpreted as operations.
