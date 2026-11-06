# Instruction selection: measurements of 2026-11-06

D-029, on the nine workloads of `BENCHMARKS.md`, under WSL2 (noise is labeled by
the ranges below).

**Status: accepted by the owner as the default, with one unresolved regression
(b08_ackermann).** Block alignment is out of scope and was not shipped.

## Method

The method is that of `BENCHMARKS.md`, unchanged, and its commands reproduce
these results. CPU time comes from 15 timed runs per variant, round-robin, pinned
to one CPU, with a discarded warm-up. The two sets of times are:

| Set | Tree | Environment | Summary |
|---|---|---|---|
| First | Uncommitted tree on 85178eb | `data/environment.txt` | `data/times_summary.tsv` |
| Confirmation | Clean `ca5dbe2`, the accepted commit | `data/environment-ca5dbe2.txt` | `data/times_summary-ca5dbe2.tsv` |

Raw times were not kept in the repository. The deterministic counts in
`data/summary.tsv` are byte-identical in both.

`ra` is registers without selection, and `isel` is what `aether build` produces.
The `slot`, `ssa`, and `ra` rows of `summary.tsv`, `functions.tsv`, and `ops.tsv`
were byte-identical to the RA-0 data, so the baseline is unchanged.

Sections below separate what was **measured** from what is only a **hypothesis**.
No hardware counters are available here, so nothing below measures branch
mispredictions, cache misses, or instructions per cycle.

## Counts, isel against ra

| Workload | Dyn. instrs | Dyn. mem ops | Static instrs | Text bytes | Frame bytes |
|---|---|---|---|---|---|
| b01_fib | -27.9% | 0 | -20.0% | -19.6% | 0 |
| b02_primes | -55.2% | 0 | -44.7% | -43.3% | 0 |
| b03_collatz | -73.3% | 0 | -55.8% | -55.0% | 0 |
| b04_matpow | -28.8% | -61.8% | -32.0% | -47.9% | -77.8% |
| b05_hash | -21.0% | 0 | -31.9% | -26.9% | 0 |
| b06_vm | -49.1% | 0 | -42.3% | -37.8% | 0 |
| b07_gcd | -46.2% | 0 | -39.9% | -37.6% | 0 |
| b08_ackermann | -35.6% | 0 | -31.1% | -28.9% | 0 |
| b09_crc | -62.4% | 0 | -46.2% | -46.0% | 0 |

Text bytes count the `main.*` functions. No count got worse except register
moves on b04 (+48.5%): with constants out of the way, values that were spilled
now live in registers, so memory operations became moves. On the S2 suite and
the workloads together, static instructions fell 32.0% (175,453 to 119,263) and
frame bytes 30.7%, and no function grew (`TestSelectNoLarger`).

## CPU time

Medians in ms. "Overlap" is of the min-max ranges of isel and ra.

| Workload | ra | isel | isel/ra | Overlap | gc | ra/gc | isel/gc |
|---|---|---|---|---|---|---|---|
| b01_fib | 176.3 | 177.1 | 1.005 | yes | 94.1 | 1.87 | 1.88 |
| b02_primes | 144.0 | 99.6 | 0.691 | no | 98.5 | 1.46 | 1.01 |
| b03_collatz | 177.0 | 112.5 | 0.636 | no | 86.4 | 2.05 | 1.30 |
| b04_matpow | 367.7 | 313.6 | 0.853 | no | 175.8 | 2.09 | 1.78 |
| b05_hash | 126.6 | 126.7 | 1.001 | yes | 40.4 | 3.14 | 3.14 |
| b06_vm | 236.9 | 210.9 | 0.890 | no | 120.1 | 1.97 | 1.76 |
| b07_gcd | 393.0 | 345.4 | 0.879 | no | 327.7 | 1.20 | 1.05 |
| **b08_ackermann** | 222.3 | 267.3 | **1.202** | no | 129.3 | 1.72 | 2.07 |
| b09_crc | 130.3 | 92.0 | 0.706 | no | 75.6 | 1.72 | 1.22 |

The comparison with gc follows `BENCHMARKS.md`: gc default against Aether's
default, and gc binaries carry a full runtime that Aether's do not.

### Confirmation at the accepted commit

The same commands, run again on clean `ca5dbe2`, with isel/ra ratios within
0.005 of the first set and the same overlap result on every workload:

| Workload | ra | isel | isel/ra | Overlap | isel/gc |
|---|---|---|---|---|---|
| b01_fib | 174.8 | 176.0 | 1.007 | yes | 1.89 |
| b02_primes | 143.9 | 100.2 | 0.696 | no | 1.01 |
| b03_collatz | 177.7 | 113.3 | 0.638 | no | 1.30 |
| b04_matpow | 370.5 | 315.6 | 0.852 | no | 1.78 |
| b05_hash | 127.4 | 127.9 | 1.003 | yes | 3.16 |
| b06_vm | 237.7 | 211.3 | 0.889 | no | 1.75 |
| b07_gcd | 394.5 | 347.8 | 0.882 | no | 1.06 |
| **b08_ackermann** | 222.0 | 267.6 | **1.205** | no | 2.07 |
| b09_crc | 130.7 | 92.3 | 0.706 | no | 1.22 |

## Unresolved regression: b08_ackermann

**Status: open. The cause is not established.**

Measured:
- b08 runs 20% slower with `isel` than with `ra`, in both sets: 222.3 to 267.3 ms
  and 222.0 to 267.6 ms. The ranges do not overlap (204.6-226.9 against
  263.3-272.4 in the confirmation set).
- `isel` executes 35.6% fewer instructions, with the same memory operations.
- With `.p2align 6` before every block, the same assembly runs in 221.7 ms instead
  of 267.6 ms (7 timed runs, separate session), so b08's time is sensitive to
  code placement.
- `ra` aligned the same way runs in 195.6 ms, so `isel` stays about 13% behind
  `ra` when both are aligned. Alignment does not remove the difference.

Hypotheses, not tested:
- Denser code puts more branches in each cache line, and the branch predictor
  reacts to that.
- Something other than placement accounts for the part of the gap that alignment
  leaves.

Sensitivity to alignment does not show that placement caused the regression. Any
of these would need hardware counters or a controlled experiment to settle.

## Alignment trade-offs (not shipped)

Block alignment is out of scope by owner decision. This data is kept for whoever
takes it up. The experiment post-processed the emitted assembly outside the
repository. `nfN` puts `.p2align N` before block labels that follow `ret`, `jmp`,
or a call to a panic helper, so no padding runs. `bN` puts it before every block
label.

CPU median in ms, 7 timed runs, one session, no ranges kept, so small differences
here do not mean much:

| Workload | isel | nf4 | nf5 | nf6 | b4 | b6 |
|---|---|---|---|---|---|---|
| b01_fib | 176.2 | 177.0 | 176.3 | 176.4 | 176.6 | 174.8 |
| b02_primes | 100.0 | 101.6 | 101.3 | 101.1 | 100.7 | 100.9 |
| b03_collatz | 112.8 | 98.4 | 98.3 | 98.3 | 119.7 | 119.8 |
| b04_matpow | 313.0 | 312.9 | 313.0 | 313.0 | 313.2 | 312.7 |
| b05_hash | 125.8 | 127.5 | 125.7 | 125.2 | 148.1 | 165.1 |
| b06_vm | 211.0 | 206.9 | 185.9 | 185.6 | 188.8 | 190.6 |
| b07_gcd | 345.6 | 350.3 | 365.1 | 346.0 | 350.6 | 353.6 |
| b08_ackermann | 269.1 | 242.0 | 243.5 | 242.7 | 237.8 | 220.5 |
| b09_crc | 91.4 | 96.2 | 92.0 | 93.6 | 88.5 | 90.2 |

Added bytes in the executable's text segment (`size`), and as a percentage of the
workload's `main.*` text. The `isel` assembly in these builds is byte-identical to
that of `ca5dbe2`.

| Workload | `main.*` bytes | nf4 | nf5 | nf6 | b4 | b6 |
|---|---|---|---|---|---|---|
| b01_fib | 205 | +8 (4%) | +8 (4%) | +8 (4%) | +13 (6%) | +157 (77%) |
| b02_primes | 360 | +71 (20%) | +167 (46%) | +359 (100%) | +135 (38%) | +807 (224%) |
| b03_collatz | 209 | +56 (27%) | +120 (57%) | +312 (149%) | +88 (42%) | +568 (272%) |
| b04_matpow | 889 | +22 (2%) | +54 (6%) | +86 (10%) | +38 (4%) | +214 (24%) |
| b05_hash | 910 | 0 | 0 | +32 (4%) | +32 (4%) | +224 (25%) |
| b06_vm | 704 | +114 (16%) | +242 (34%) | +402 (57%) | +162 (23%) | +1106 (157%) |
| b07_gcd | 349 | +62 (18%) | +174 (50%) | +302 (87%) | +126 (36%) | +686 (197%) |
| b08_ackermann | 318 | +15 (5%) | +15 (5%) | +15 (5%) | +34 (11%) | +226 (71%) |
| b09_crc | 479 | +31 (6%) | +95 (20%) | +255 (53%) | +95 (20%) | +511 (107%) |

Measured trade-offs, against the `isel` column:
- `nf6` gains on b03 (-13%), b06 (-12%), and b08 (-10%), loses on b09 (+2.4%), and
  grows `main.*` text by 4% to 149%. b08 stays about 9% above its `ra` time.
- `b6` is the only variant that brings b08 near its `ra` time (-18%), and it costs
  b05 31% and b03 6%, with 24% to 272% more `main.*` text.
- No variant improves every workload.

Against the whole text segment, runtime included, `nf6` adds 0.9% to 35.9% (mean
18.1%). These workloads are small, which magnifies percentage growth, and the
effects may differ on other CPUs.

## Other results

b01 and b05 show no time change despite 28% and 21% fewer instructions (measured).
Most of what they lost was register moves. That those moves cost little on this
CPU is a hypothesis.

## Limits

Synthetic S2 kernels, one CPU, under WSL2. Layout effects this size may differ
on other CPUs. Register-register operations still go through `%rax`, and values
live across calls stay in cells, so neither is addressed here.

Fuzzing: `data/fuzz-generated-60s.log` is the complete 60 s `FuzzGenerated` run
from before the commit, and `data/fuzz-generated-60s-ca5dbe2.log` the complete run
on the accepted code. Each records its exit status.

## Reviewing `ca5dbe2`

The selector and the benchmark harness landed in one commit. They are separate by
file. Nothing outside `_test.go` files belongs to the harness.

| Group | Files | Lines |
|---|---|---|
| Selector | `internal/amd64/{select,amd64,regalloc}.go`, `cmd/aether/build.go` | +725 -32 |
| Selector tests | `internal/amd64/select_test.go`, `cmd/aether/{select,controls,ra_native,m1,stack}_test.go` | +781 -14 |
| Benchmark harness | `cmd/aether/bench_{test,profile_test,profile_check_test,stats_test}.go` | +45 -13 |
| Docs and data | `README.md`, `docs/` | +325 -11 |
