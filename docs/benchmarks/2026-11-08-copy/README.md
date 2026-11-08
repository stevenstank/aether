# Coalescing against the DCE baseline (2026-11-08)

The effect of coalescing in register allocation (`EmitOptions.Coalesce`,
`internal/amd64/regalloc.go`) on the nine workloads. `dce` is the committed
build of the Phase 5 report (4ff925f); `copy` is `dce` with coalescing, which
`aether build` now produces. Every figure was measured; hypotheses are marked.

**Environment.** WSL2 (kernel 6.6.87.2-microsoft-standard-WSL2) on an AMD
Ryzen 7 7435HS, every timed run pinned to CPU 6; go1.26.1; GNU as and ld 2.42;
commit 4ff925f plus this change (`data/environment.txt`). Caches warm, with a
discarded warm-up run. No hardware counters.

**Why coalescing, not copy propagation.** After promotion, folding, and DCE,
the IR of the S2 suite and the workloads has no copy to propagate (measured:
no identity conversion, no φ whose values are one value, no two equal φs).
The copies are register moves the backend emits: they were 10–36% of the
`dce` build's executed instructions (`reg-reg mov`, `data/summary.tsv`).

**Control.** The `dce` rows of `data/summary.tsv` are byte-identical to the
Phase 5 report's.

## Static code

On the S2 suite and the workloads (`TestCoalesceNoLarger`): register moves
6,184 → 6,040 (−2.3%), machine instructions 54,065 → 53,924 (−0.3%). No
function is longer or has a larger frame, and no `aether.mainstack` grew. On
the workloads, text bytes fall by 0–12 per program (`data/summary.tsv`).

## Executed instructions

Exact instrumented counts (`data/summary.tsv`). Memory operations, calls,
frames, and peak stack are identical; every removed instruction is a register
move.

| Workload | Executed instrs `copy`/`dce` | Register moves `dce` → `copy` |
|---|---|---|
| b01_fib | 0.949 | 312.7M → 234.5M |
| b02_primes | 0.999 | 309.5M → 308.0M |
| b03_collatz | 1.000 | unchanged |
| b04_matpow | 1.000 | unchanged |
| b05_hash | 0.973 | 630.0M → 550.0M |
| b06_vm | 0.965 | 234.0M → 202.0M |
| b07_gcd | 0.947 | 870.4M → 741.5M |
| b08_ackermann | 0.902 | 376.3M → 250.9M |
| b09_crc | 0.993 | 72.0M → 69.0M |

## CPU time

CPU ms, median of 15 runs at the shipped layout with min–max
(`data/times_summary.tsv`), and the median of the medians at eight code
offsets modulo 64 (7 runs each) with the range of those medians
(`data/layout_summary.tsv`).

| Workload | Shipped `dce` | Shipped `copy` | ratio | Offsets `dce` | Offsets `copy` | ratio |
|---|---|---|---|---|---|---|
| b01_fib | 159.8 (157.3–170.1) | 158.8 (156.3–164.4) | 0.994 | 158.6 (157.2–159.1) | 158.4 (157.8–161.6) | 0.999 |
| b02_primes | 100.8 (99.7–111.7) | 100.9 (99.5–105.1) | 1.001 | 100.6 (100.1–101.0) | 100.4 (100.2–102.0) | 0.998 |
| b03_collatz | 106.2 (103.2–112.6) | 105.9 (103.5–123.9) | 0.997 | 101.8 (98.0–108.7) | 101.6 (98.6–108.9) | 0.998 |
| b04_matpow | 316.0 (313.3–328.5) | 316.4 (312.5–323.6) | 1.001 | 314.3 (313.4–315.7) | 314.4 (313.5–316.0) | 1.000 |
| b05_hash | 106.3 (104.6–109.6) | 109.8 (108.4–112.8) | 1.033 | 112.6 (103.7–123.0) | 108.9 (101.1–120.8) | 0.967 |
| b06_vm | 190.3 (187.3–194.5) | 188.5 (186.0–191.5) | 0.991 | 187.5 (186.3–191.8) | 187.6 (186.6–190.9) | 1.001 |
| b07_gcd | 348.0 (345.6–370.0) | 347.6 (346.0–354.0) | 0.999 | 349.4 (347.5–351.9) | 350.4 (347.9–351.5) | 1.003 |
| b08_ackermann | 176.2 (171.3–184.1) | 220.1 (216.8–222.0) | 1.249 | 166.1 (158.8–229.6) | 168.2 (156.8–238.5) | 1.013 |
| b09_crc | 97.5 (96.7–99.0) | 92.3 (91.5–96.3) | 0.947 | 93.3 (90.6–98.5) | 91.7 (89.4–105.2) | 0.983 |

- **Measured:** across offsets, no workload's `copy`/`dce` ratio is outside
  0.967–1.013, and each variant's median of offsets lies inside the range of the other's
  per-offset medians. Removing up to 9.8% of executed instructions gives no
  CPU time change that this setup can distinguish from code placement.
- **Measured:** at the shipped layout b08 is 25% slower and b05 3.3% slower,
  and b09 5.3% faster, but each lies within the placement range of the
  layout run: `dce` itself takes 229.6 ms on b08 at offset 24 and `copy`
  220.1 ms at offset 0, the shipped one.
- **Hypothesis, not measured:** register-to-register moves are eliminated at
  rename on this core, so they cost issue slots but little time; the Phase 5
  report saw the same for the register forms of arithmetic.

**Regressions.** None in any count. No CPU time regression beyond placement
noise; the shipped-layout b08 result is recorded here as a placement effect,
established by the layout run, not as a change of the code's speed.
