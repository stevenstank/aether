# Slice benchmarks (2026-11-15)

Slices (Phase 7 increment 7.2, D-036) on five workloads in
`cmd/aether/testdata/bench_slice`, built by `aether build`, `aether build -O0`,
and gc with `GOAMD64=v1`, by `TestBenchSlice` (`cmd/aether/bench_slice_test.go`,
the harness of `TestBenchAgg`). Every executable gave its recorded gc output on
every run; no workload prints a capacity after growth (V-6). Measurements and
hypotheses are kept apart.

**Environment.** WSL2 (kernel 6.18.58.1-microsoft-standard-WSL2) on an AMD
Ryzen 7 7435HS, every run pinned to CPU 6; go1.26.1; GNU as and ld 2.42;
commit d19f314 plus this change (`data/environment.txt`). Caches warm, one
discarded warm-up run and build per variant. No hardware counters.

```
AETHER_BENCH_OUT=$OUT AETHER_BENCH_RUNS=15 AETHER_BENCH_COMPILE_RUNS=7 taskset -c 6 \
  go test -count=1 -timeout 9m ./cmd/aether -run '^TestBenchSlice$' -v
```

## Execution

CPU ms (user + system), median of 15 runs, with the interquartile range
(`data/slice_summary.tsv`, raw runs in `data/slice_times.tsv`).

| Workload | What it does | `aether build` | `-O0` | gc | aether/gc | -O0/aether |
|---|---|---|---|---|---|---|
| c01_make_small | 300,000 `make([]int, 16)`, written and read | 19.8 (19.5–19.8) | 31.9 (31.8–32.1) | 1.8 (1.8–1.8) | 11.1 | 1.6 |
| c02_append_grow | 8 × 1,000,000 appends to a nil slice | 79.2 (78.7–80.2) | 166.6 (165.9–167.5) | 43.9 (42.8–45.0) | 1.8 | 2.1 |
| c03_append_within | 40 × 1,000,000 appends within the capacity | 61.6 (61.1–61.8) | 555.0 (547.9–556.2) | 23.9 (23.5–24.1) | 2.6 | 9.0 |
| c04_index | 100 passes of reads and writes over 1,000,000 ints | 187.0 (184.5–187.8) | 2364.5 (2360.2–2368.4) | 59.6 (59.4–60.4) | 3.1 | 12.6 |
| c05_copy_overlap | 800 overlapping copies of 100,000 ints, both directions | 82.1 (81.7–82.9) | 82.9 (82.5–83.5) | 9.8 (9.7–9.9) | 8.4 | 1.0 |

## Compilation

Wall ms of the whole build process, median of 7 (`data/slice_compile.tsv`):
`aether build` 20.2–20.4 and `-O0` 20.0–20.4 (both including `as` and `ld`),
gc 55.8–56.9 (`go build`, including the go command and its cache lookup). Kept
apart from execution; not a like-for-like comparison of compilers.

## Where `aether build`'s time goes

Executed instructions of the instrumented `aether build` executable in the
`main.*` functions, by the IR operation that emitted them
(`data/slice_profile.tsv`). The runtime's own instructions (`aether.alloc`,
`aether.memmove`) are not counted.

| Workload | Executed instructions | from `alloc`, `move`, `ldm`, `stm` | Largest shares |
|---|---|---|---|
| c01_make_small | 19.8 M | 13.6% | rem 36.4%, branch 15.2%, add 12.1% |
| c02_append_grow | 216.0 M | 7.4% | jump 25.9%, branch 25.9%, add 25.9% |
| c03_append_within | 1000.0 M | 8.0% | add 40.0%, jump 24.0%, branch 24.0% |
| c04_index | 2711.0 M | 14.8% | branch 33.4%, mul 14.8%, add 11.1%, conv 11.1% |
| c05_copy_overlap | 1.2 M | 16.9% | branch 41.3%, jump 16.6%, stm 16.5% |

Findings, from these measurements:
- **Overlapping copy (c05, 8.4×)** spends almost all its time in
  `aether.memmove`: the compiled code executes only 1.2 M instructions, and
  `-O0` is as fast as `aether build`.
- **Indexing and append within the capacity (c03 2.6×, c04 3.1×)** are close
  to Aether's ratio on scalar code: per element, a bounds check (branch), the
  index scaled by a multiply, and the loop's own control.
- **Growing appends (c02, 1.8×)** are the closest to gc.
- **Small `make` (c01, 11.1×)** costs a call of `aether.alloc` and the
  `makeslice` checks per slice.

Hypotheses, not measured:
- c05: half of the copies move backward, by `rep movsb` with the direction
  flag set, which processors are known to execute much more slowly than the
  forward form; gc's `memmove` uses vector loads and stores.
- c01: gc keeps a 16-element slice that does not escape on the stack, with no
  allocation; Aether allocates on the heap every time (D-036, V-5).

**Not done.** No optimization follows from this report by itself
(`BENCHMARKS.md` §What justifies optimization work).
