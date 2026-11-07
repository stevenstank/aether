# Phase 5 benchmark report (2026-11-08)

The Phase 5 acceptance report (`ROADMAP.md`): the measured effect of every
Phase 5 step on the nine workloads against the Phase 4 baseline, and compile
latency checked against D-008's revisit triggers. Every figure here was
measured; hypotheses are marked as such.

**Environment.** WSL2 (kernel 6.6.87.2-microsoft-standard-WSL2) on an AMD
Ryzen 7 7435HS, every run pinned to CPU 6; go1.26.1; GNU as and ld 2.42. The
compiler was commit ed607ca plus the harness changes committed with this
report, which touch no compiler code (`data/environment.txt`). File-system
caches warm, with a discarded warm-up run. WSL2 has no hardware counters, so
"executed instructions" are the harness's instrumented counts
(`BENCHMARKS.md`), and causes of timing differences cannot be established.

## Variants

Each variant is a step of Phase 5, cumulative:

| Variant | What it is |
|---|---|
| `slot` | **Phase 4 baseline**: every value in a stack cell; what `aether build -O0` builds |
| `ssa` | promoted to SSA (D-006), values still in cells |
| `ra` | with register allocation (D-026, D-027) |
| `isel` | with instruction selection (D-029) |
| `abir1` | with the ABI-R1 calling convention (D-030, D-031) |
| `two` | with the register forms of arithmetic (`internal/amd64/arith.go`) |
| `fold` | with constant folding (`opt.Fold`) |
| `dce` | with dead code elimination (`opt.DCE`): **what `aether build` builds now** |
| `gc`, `gcN` | `go build`, and with `-gcflags=all='-N -l'`: reference points only |

## Phase 4 baseline against the current build

CPU time is the median of 15 runs at the shipped code layout
(`data/times_summary.tsv`, which also has the interquartile range and min–max
of each). Counts are exact for the instrumented executables
(`data/summary.tsv`).

| Workload | CPU ms `slot` | CPU ms `dce` | `dce`/`slot` | `dce`/gc | Executed instrs `dce`/`slot` | Memory ops `dce`/`slot` | Text bytes `slot` → `dce` |
|---|---|---|---|---|---|---|---|
| b01_fib | 300.7 | 158.0 | 0.525 | 1.69 | 0.476 | 0.311 | 318 → 161 |
| b02_primes | 304.6 | 99.5 | 0.327 | 1.01 | 0.289 | 0.007 | 1046 → 325 |
| b03_collatz | 600.6 | 103.0 | 0.171 | 1.20 | 0.202 | 0.001 | 898 → 206 |
| b04_matpow | 1048.6 | 313.2 | 0.299 | 1.79 | 0.331 | 0.116 | 3357 → 754 |
| b05_hash | 664.6 | 104.5 | 0.157 | 2.59 | 0.426 | 0.124 | 2594 → 679 |
| b06_vm | 590.3 | 187.4 | 0.317 | 1.57 | 0.399 | 0.151 | 1814 → 660 |
| b07_gcd | 707.3 | 346.6 | 0.490 | 1.06 | 0.327 | 0.042 | 943 → 317 |
| b08_ackermann | 384.1 | 177.2 | 0.461 | 1.39 | 0.376 | 0.145 | 637 → 227 |
| b09_crc | 451.4 | 96.9 | 0.215 | 1.28 | 0.224 | 0.048 | 1797 → 377 |

On every workload the current build takes 16–53% of the Phase 4 baseline's
CPU time, and 1.01–2.59 times gc's. Every executable gave exactly gc's output.

## Each step

CPU ms, median of 15 runs at the shipped layout:

| Workload | `slot` | `ssa` | `ra` | `isel` | `abir1` | `two` | `fold` | `dce` | `gc` | `gcN` |
|---|---|---|---|---|---|---|---|---|---|---|
| b01_fib | 300.7 | 258.0 | 175.0 | 175.8 | 154.4 | 158.1 | 158.9 | 158.0 | 93.5 | 102.4 |
| b02_primes | 304.6 | 175.2 | 143.3 | 99.5 | 99.4 | 99.4 | 99.4 | 99.5 | 98.6 | 102.0 |
| b03_collatz | 600.6 | 329.7 | 174.8 | 112.0 | 112.1 | 102.9 | 102.9 | 103.0 | 85.8 | 145.6 |
| b04_matpow | 1048.6 | 542.0 | 366.5 | 313.2 | 313.0 | 313.2 | 313.0 | 313.2 | 175.1 | 220.3 |
| b05_hash | 664.6 | 424.5 | 126.4 | 126.1 | 129.5 | 103.7 | 104.3 | 104.5 | 40.4 | 88.8 |
| b06_vm | 590.3 | 453.2 | 235.8 | 210.8 | 188.8 | 187.4 | 187.6 | 187.4 | 119.5 | 169.8 |
| b07_gcd | 707.3 | 477.9 | 393.3 | 346.3 | 346.3 | 346.3 | 346.4 | 346.6 | 327.2 | 384.6 |
| b08_ackermann | 384.1 | 296.4 | 221.2 | 267.3 | 183.2 | 175.1 | 175.7 | 177.2 | 127.8 | 142.2 |
| b09_crc | 451.4 | 319.2 | 130.4 | 92.1 | 91.0 | 97.1 | 97.0 | 96.9 | 75.9 | 104.4 |

Executed instructions, millions:

| Workload | `slot` | `ssa` | `ra` | `isel` | `abir1` | `two` | `fold` | `dce` |
|---|---|---|---|---|---|---|---|---|
| b01_fib | 3205 | 2658 | 2658 | 1915 | 1603 | 1524 | 1524 | 1524 |
| b02_primes | 4111 | 2914 | 2793 | 1253 | 1247 | 1188 | 1188 | 1188 |
| b03_collatz | 3796 | 3337 | 2882 | 768 | 768 | 768 | 768 | 768 |
| b04_matpow | 8074 | 5280 | 5017 | 3571 | 3571 | 2671 | 2671 | 2671 |
| b05_hash | 4340 | 3140 | 3100 | 2450 | 2210 | 1850 | 1850 | 1850 |
| b06_vm | 2283 | 1903 | 1887 | 960 | 928 | 912 | 912 | 912 |
| b07_gcd | 7389 | 4836 | 4737 | 2547 | 2435 | 2419 | 2419 | 2419 |
| b08_ackermann | 3418 | 2728 | 2728 | 1756 | 1286 | 1286 | 1286 | 1286 |
| b09_crc | 2046 | 1722 | 1557 | 585 | 564 | 459 | 459 | 459 |

- **Folding and DCE** leave the machine code of the workloads unchanged, except
  two instructions of b05's that run once; their timings equal `two`'s within
  the spread. They remove code elsewhere: on the S2 suite and the workloads,
  static machine instructions fall 51.0% with folding and a further 1.1% with
  DCE, mostly in the generated programs (`TestPassesNoLarger`).
- **One layout is not enough** for the last steps. At the shipped layout
  `two`/`abir1` is 0.92 on b03 and 1.07 on b09; across eight code offsets it
  is 1.006 and 1.023 (below). Placement moves these workloads by more
  than the steps do.

## Across code offsets

Each variant at eight offsets modulo 64, 11 runs each; the figure is the median
over offsets of each offset's median CPU ms (`data/abi_layout_summary.tsv`, with
every offset's median).

| Workload | `isel` | `abir1` | `two` | `dce` | `two`/`abir1` | `dce`/`isel` |
|---|---|---|---|---|---|---|
| b01_fib | 176.5 | 154.8 | 158.1 | 158.4 | 1.021 | 0.897 |
| b02_primes | 101.5 | 100.3 | 100.4 | 100.3 | 1.001 | 0.988 |
| b03_collatz | 101.0 | 100.7 | 101.3 | 101.2 | 1.006 | 1.002 |
| b04_matpow | 313.3 | 313.2 | 313.5 | 313.3 | 1.001 | 1.000 |
| b05_hash | 129.5 | 107.2 | 111.3 | 113.8 | 1.038 | 0.879 |
| b06_vm | 206.6 | 188.4 | 187.1 | 186.9 | 0.993 | 0.905 |
| b07_gcd | 366.0 | 349.6 | 348.8 | 348.8 | 0.998 | 0.953 |
| b08_ackermann | 232.6 | 165.0 | 164.7 | 166.3 | 0.998 | 0.715 |
| b09_crc | 90.8 | 90.9 | 93.0 | 92.8 | 1.023 | 1.022 |

## Regressions

Recorded as `BENCHMARKS.md` requires; none has an established cause.

- **b01_fib, `two` against `abir1`: +2.1%,** slower at all eight offsets.
  Its only code change is `movq -16(%rbp), %rax; addq %rbx, %rax; movq %rax,
  %rbx` becoming `addq -16(%rbp), %rbx`. *Hypothesis:* register-to-register
  moves cost almost nothing on this core, so instruction count does not
  predict time here.
- **b09_crc, `two` against `abir1`: +2.3%** across offsets, +6.7% at the shipped
  layout. Executed instructions fell 18.6%.
- **b05_hash, `two` against `abir1`: +3.8%** across offsets, but `abir1`'s
  offsets range from 97 to 129 ms, so this is within placement noise.
- **b09_crc, `dce` against `isel`: +2.2%** across offsets; it is the step above.
- **b08_ackermann's `isel` regression** (D-029, +20% against `ra`) is no longer
  visible in the current build, which takes 177 ms against `ra`'s 221 ms at the
  shipped layout. Its cause was never established.

## Compile latency and D-008

`TestBenchLatency` times the stages of `aether build` in process with the
compiler's own functions, 21 runs after a warm-up
(`data/latency_summary.tsv`, medians). `assemble` is everything after
emission: it finds and identifies `as` and `ld` (`as --version`, `ld -V`),
writes the files, and runs `as` three times (the program and two runtime
files) and `ld` once; `identify` is its first part, timed alone.

| Program | compile ms | optimize+emit ms | emit `-O0` ms | assemble ms | of which identify ms | assemble share |
|---|---|---|---|---|---|---|
| b01_fib | 0.144 | 0.141 | 0.065 | 13.209 | 3.210 | 0.98 |
| b02_primes | 0.200 | 0.365 | 0.146 | 13.256 | 3.248 | 0.96 |
| b03_collatz | 0.180 | 0.306 | 0.122 | 13.303 | 3.149 | 0.96 |
| b04_matpow | 0.401 | 0.935 | 0.357 | 13.667 | 3.302 | 0.91 |
| b05_hash | 0.281 | 0.483 | 0.253 | 13.020 | 3.039 | 0.94 |
| b06_vm | 0.236 | 0.456 | 0.198 | 12.980 | 3.042 | 0.95 |
| b07_gcd | 0.169 | 0.301 | 0.111 | 12.892 | 3.221 | 0.96 |
| b08_ackermann | 0.134 | 0.148 | 0.071 | 12.623 | 3.107 | 0.98 |
| b09_crc | 0.231 | 0.417 | 0.182 | 12.825 | 2.988 | 0.95 |
| s2_d01_division_shifts | 1.695 | 5.269 | 3.177 | 15.322 | 3.023 | 0.69 |
| s2_i01_integer_tables | 1.330 | 3.114 | 1.792 | 14.875 | 3.095 | 0.77 |
| s2_a02_state_machines | 0.637 | 1.471 | 0.649 | 13.652 | 3.008 | 0.87 |

Measured end to end, `aether build` of each workload took a median of 18–19 ms
wall and 29 ms for s2_d01, with no difference between the default build and
`-O0` at that resolution, and a peak RSS of 4.5–8.3 MiB as GNU `time`
reports it. These two figures come from a shell script outside the harness and
are not reproducible from the repository; the in-process figures above are the
ones to rely on.

**D-008's first revisit trigger is met:** "`as`/`ld` time measurably dominates
compile latency on recorded workloads". The tools take 91–98% of the build
on every workload and 69–87% on the largest corpus programs. About 3 ms of
each build is identifying the tools, and each build reassembles the two
runtime files. *Hypothesis, not measured:* most of the rest is process
creation rather than assembling, since the inputs are a few hundred lines.
Meeting the trigger calls for revisiting D-008, which is the owner's decision;
nothing here changes it. At about 13 ms per build the latency is not a
problem in practice.

**Not covered:** scalability with source size, and compiler memory per stage.

## What this does not cover

- D-010 (memory management and GC) is not revisited: S2 has no heap, and no
  measurement here bears on it.
- Runtime results come from nine small workloads on one machine under WSL2.

## Reproducing

From the repository root at the commit of this report, with `OUT` an empty
directory:

```
AETHER_BENCH_OUT=$OUT go test -count=1 ./cmd/aether -run 'TestBenchOracle$' -v
AETHER_BENCH_OUT=$OUT go test -count=1 -timeout 60m ./cmd/aether -run 'TestBenchMeasure$' -v
AETHER_BENCH_OUT=$OUT AETHER_BENCH_RUNS=15 taskset -c 6 \
  go test -count=1 -timeout 90m ./cmd/aether -run 'TestBenchTime$' -v
AETHER_ABI_LAYOUT=1 AETHER_ABI_VARIANTS=isel,abir1,two,dce AETHER_BENCH_OUT=$OUT \
  AETHER_BENCH_RUNS=11 taskset -c 6 \
  go test -count=1 -timeout 150m ./cmd/aether -run '^TestABITime$' -v
AETHER_BENCH_OUT=$OUT AETHER_BENCH_RUNS=21 taskset -c 6 \
  go test -count=1 ./cmd/aether -run '^TestBenchLatency$' -v
```

The raw runs (`times.tsv`, `abi_layout.tsv`, `latency.tsv`, about 5,800 lines)
are not committed, to keep the change reviewable; these commands regenerate
them.
