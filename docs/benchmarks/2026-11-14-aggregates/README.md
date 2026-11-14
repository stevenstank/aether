# Aggregate benchmarks (2026-11-14)

Arrays and structs (Phase 6 increment 6.3, D-034) on five workloads in
`cmd/aether/testdata/bench_agg`, built by `aether build`, `aether build -O0`,
and gc with `GOAMD64=v1`, by `TestBenchAgg` (`cmd/aether/bench_agg_test.go`).
Every executable gave its recorded gc output on every run. Measurements and
hypotheses are kept apart.

**Environment.** WSL2 (kernel 6.6.87.2-microsoft-standard-WSL2) on an AMD
Ryzen 7 7435HS, every run pinned to CPU 6; go1.26.1; GNU as and ld 2.42;
commit 6fabe75 plus this change (`data/environment.txt`). Caches warm, one
discarded warm-up run and build per variant. No hardware counters.

```
AETHER_BENCH_OUT=$OUT AETHER_BENCH_RUNS=15 AETHER_BENCH_COMPILE_RUNS=7 taskset -c 6 \
  go test -count=1 -timeout 60m ./cmd/aether -run '^TestBenchAgg$' -v
```

## Execution

CPU ms (user + system), median of 15 runs, with the interquartile range
(`data/agg_summary.tsv`).

| Workload | What it does | `aether build` | `-O0` | gc | aether/gc | -O0/aether |
|---|---|---|---|---|---|---|
| a01_small_copy | 2- and 4-word structs copied, passed, returned | 715.1 (713.9–718.4) | 5072.4 (5069.6–5085.2) | 19.9 (19.7–19.9) | 36.0 | 7.1 |
| a02_nested_copy | a 21-scalar nested struct copied, stored in an array, passed | 572.1 (571.3–573.1) | 4672.3 (4649.6–4681.7) | 185.8 (185.4–186.5) | 3.1 | 8.2 |
| a03_large_array_copy | a 1024-element array copied and passed | 567.1 (563.5–570.5) | 4978.9 (4957.3–4987.5) | 82.9 (82.8–83.5) | 6.8 | 8.8 |
| a04_compare | `==`/`!=` on a 256-element array and a struct | 727.9 (723.0–730.3) | 4882.4 (4874.7–4902.0) | 23.2 (23.1–23.3) | 31.4 | 6.7 |
| a05_field_access | matrix product and insertion sort, by field and index | 131.7 (131.3–132.1) | 1347.5 (1343.9–1352.8) | 49.5 (49.4–49.7) | 2.7 | 10.2 |

## Compilation

Wall ms of the whole build process, median of 7 (`data/agg_compile.tsv`):
`aether build` 23.9–29.1, `-O0` 23.5–30.8 (both including `as` and `ld`), gc
65.0–66.2 (`go build`, including the go command and its cache lookup). Kept
apart from execution; not a like-for-like comparison of compilers.

## Where `aether build`'s time goes

Executed instructions of the instrumented `aether build` executable, by the
IR operation that emitted them (`data/agg_profile.tsv`, exact counts).

| Workload | Executed instructions | from `addr`, `ldm`, `stm` | Largest shares |
|---|---|---|---|
| a01_small_copy | 6.72 G | 74.4% | ldm 34.5%, stm 29.2%, addr 10.7%, prologue 8.3% |
| a02_nested_copy | 6.22 G | 72.8% | ldm 39.1%, stm 32.0%, add 8.8%, mul 7.8% |
| a03_large_array_copy | 11.90 G | 37.9% | branch 20.7%, mul 20.7%, addr 17.2%, jump 10.3% |
| a04_compare | 11.78 G | 36.6% | branch 24.7%, mul 19.2%, addr 16.7%, ldm 12.6% |
| a05_field_access | 2.14 G | 15.5% | branch 33.4%, mul 16.1%, add 14.1%, conv 12.0% |

Findings, from these counts:
- **Small structs (a01, 36×).** 168 instructions and 89 memory-object accesses
  per iteration. Copying every word of the source's assignments, arguments,
  and results once, with its field updates, needs about 69; so the extra
  temporaries of D-034 (a value read is copied into a temporary first) account
  for about a fifth of the memory traffic, and keeping the structs in memory at
  all for the rest. *Hypothesis:* gc keeps these structs in registers and
  inlines the two small functions; not measured here.
- **Large copies and comparisons (a03 6.8×, a04 31×).** About 40% of the
  instructions are loop control and index arithmetic of the element loops
  (branch, jump, mul, add): each 8-byte element costs a multiply, a branch,
  and two `addr`s besides its load and store. *Hypothesis:* gc uses a block
  copy and a vectorized compare.
- **Nested copies (a02, 3.1×) and field and element access (a05, 2.7×)** are
  close to Aether's ratio on scalar code: memory traffic dominates a02 in
  both compilers, and a05's cost is the bounds checks, index conversions, and
  scaling that scalar code has too.
- **`-O0`** is 6.7–10.2× slower than `aether build`, as on the scalar
  workloads, since every value lives in a stack cell.

**Not done.** No optimization follows from this report by itself
(`BENCHMARKS.md` §What justifies optimization work). If one is pursued, the
counts point at three separate causes, in order of measured share: memory
residence of small structs, element loops for large copies and comparisons,
and the extra temporary copy.
