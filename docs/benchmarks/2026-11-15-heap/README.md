# Heap memory growth (2026-11-15)

Peak resident set of executables using the Phase 7 heap: a bump allocator
that never frees (D-010, D-035), on four workloads in
`cmd/aether/testdata/bench_heap`, built by `aether build`, `aether build -O0`,
and gc with `GOAMD64=v1`, by `TestBenchHeap` (`cmd/aether/bench_heap_test.go`).
This answers `ROADMAP.md` Phase 7's "memory growth is documented if the
allocator does not collect". Every run gave its gc output. Measurements and
hypotheses are kept apart.

**Environment.** WSL2 (kernel 6.18.58.1-microsoft-standard-WSL2) on an AMD
Ryzen 7 7435HS, every run pinned to CPU 6; go1.26.1; GNU as and ld 2.42; GNU
time; commit 3a54751 plus this change (`data/environment.txt`). One discarded
warm-up run per executable.

```
AETHER_BENCH_OUT=$OUT AETHER_BENCH_RUNS=7 taskset -c 6 \
  go test -count=1 -timeout 9m ./cmd/aether -run '^TestBenchHeap$' -v
```

Peak RSS is GNU time's `%M` (the kernel's `ru_maxrss`), in KiB, median of 7
runs, with min–max (`data/heap_summary.tsv`, raw runs in `data/heap_rss.tsv`).

## Workloads

| Workload | What it allocates | `aether build` | `-O0` | gc | aether/gc |
|---|---|---|---|---|---|
| h01_list_churn | 64 rounds of a 100,000-node list (16-byte nodes), each dropped after use | 99968 (99968–99968) | 99968 | 11772 (11296–12156) | 8.49 |
| h02_tree_live | a binary tree of 2²¹−1 24-byte nodes, live to the end | 49152 (49152–49152) | 49152 | 54524 (54268–54652) | 0.90 |
| h03_escaping_locals | 2 × 4,000,000 address-taken `int` locals, half escaping the iteration | 62464 (62464–62464) | 62464 | 6396 (6268–6524) | 9.77 |
| h04_small_objects | 2,000,000 `new(int8)` and 2,000,000 `new(E)` of a zero-size `E` | 15616 (15616–15616) | 15616 | 3580 (3580–3580) | 4.36 |

## Growth with garbage

h01_list_churn with its `rounds` constant set to each value, everything else
unchanged (`data/heap_growth.tsv`). Each round allocates 100,000 nodes and
drops them.

| Rounds | `aether build` KiB | gc KiB |
|---|---|---|
| 1 | 1536 | 3068 |
| 2 | 3072 | 4732 |
| 4 | 6272 | 6652 (6396–6780) |
| 8 | 12416 | 8444 (8060–9596) |
| 16 | 24960 | 9468 (8828–9852) |
| 32 | 49920 | 11260 (11108–11644) |
| 64 | 99968 | 11612 (11172–12260) |
| 128 | 199936 | 11748 (11180–12744) |

Aether's runs did not vary at all; gc's varied by up to about 1.5 MiB.

## Findings

From these measurements:
- **Aether's peak grows linearly with everything a program allocates,
  garbage included.** From 64 to 128 rounds it grows by 99968 KiB, 1562.0 KiB
  per round, which is 100,000 nodes × 16 bytes (1562.5 KiB) to within 0.1%.
  gc's peak levels off at 11.0–11.5 MiB (medians) from 32 rounds on.
- **The peak is close to the bytes allocated.** Computed from the workloads
  (16 bytes per list node, 24 per tree node, 8 per `int`, and 8 per `int8`
  after rounding), the allocations total 100,000.0, 49,152.0, 62,500.0, and
  15,625.0 KiB; the measured peaks equal them (h02) or are up to 0.06% below
  them. `new(E)` of a
  zero-size type allocates nothing: h04's peak matches its `int8`s alone.
- **Without garbage, Aether's heap is the smaller.** On h02, where every node
  stays live, Aether's peak is 0.90 of gc's.
- **`-O0` makes no difference**, as expected: the heap does not depend on
  where values live.
- **Run time was not measured by the harness.** Single unpinned runs in a
  check before the measurement took 0.01–0.07 s for Aether and 0.01–0.14 s
  for gc; that is no basis for a comparison.

Hypotheses, not measured:
- That a measured peak is slightly *below* the bytes a program wrote is
  possible because the kernel's resident-set counters are approximate:
  `proc(5)` says `VmHWM`, the peak resident set, "is inaccurate". That
  `ru_maxrss` comes from the same counter was not checked in the kernel
  source.
- gc's extra on h02 (about 5 MiB) is its runtime and collector state; on the
  churning workloads its peak reflects the live set plus the collector's
  default `GOGC=100` headroom.

## Consequences

Programs that allocate garbage in a loop run out of the 64 GiB reservation
eventually, after allocating 64 GiB in total, and exit with `fatal error:
runtime: out of memory`; under gc they would run in constant memory. Long
before that, their resident set grows by every allocated byte, so the machine's
memory, not the reservation, is the practical limit. This is divergence V-5
(`SEMANTICS.md`) until a collector arrives in Phase 9.
