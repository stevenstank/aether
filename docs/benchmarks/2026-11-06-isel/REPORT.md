# Instruction selection: measurements of 2026-11-06

D-029, on the nine workloads of `BENCHMARKS.md`, under WSL2 (noise is labeled by
the ranges below). Environment: `data/environment.txt`. Uncommitted tree on
85178eb. CPU time from 15 timed runs per variant, round-robin, pinned to one
CPU, with a discarded warm-up. Raw times were not kept in the repository; the
commands in `BENCHMARKS.md` reproduce them, and `data/` has the summaries.

`ra` is registers without selection, and `isel` is what `aether build` produces.
The `slot`, `ssa`, and `ra` rows of `summary.tsv`, `functions.tsv`, and `ops.tsv`
were byte-identical to the RA-0 data, so the baseline is unchanged.

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

## The regression on b08

b08 runs 20% slower with 36% fewer instructions and the same memory operations.
The same assembly with `.p2align 6` before every block runs in 221.7 ms instead
of 267.6 ms (7 timed runs, separate session), so the cost depends on where the code
falls. Denser code puts more branches in each cache line, which may be what the
predictor is reacting to, but there are no hardware counters here to show it. It
is an inference. Aligned the same way, `ra` runs in 195.6 ms, so alignment does
not explain all of the gap, and the rest is not understood.

Alignment was tried on all nine and **not shipped**, since it is a separate
optimization with mixed results. Medians in ms, 7 timed runs, one session.
`nfN` aligns to 2^N only the blocks nothing falls through into, so no padding
runs. `bN` aligns every block.

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

b01 and b05 show no time change despite 28% and 21% fewer instructions. Most of
what they lost was register moves, which may cost little on this CPU; that is
also an inference.

## Limits

Synthetic S2 kernels, one CPU, under WSL2. Layout effects this size may differ
on other CPUs. Register-register operations still go through `%rax`, and values
live across calls stay in cells, so neither is addressed here.

`data/fuzz-generated-60s.log`: the complete 60 s `FuzzGenerated` run, with exit
status.
