# RA-0 results (2026-11-06)

Experiment RA-0 of D-026: values of SSA functions in registers, with the
instruction sequences unchanged. Measured at Aether commit `2606705` plus the
harness's `ra` variant (uncommitted at the time, "modified" in
`data/environment.txt`), under WSL2, with the harness of `BENCHMARKS.md`.
RA-0 is test-only: `aether build` does not allocate registers, and its
executables are unchanged.

**How to read this report.** As in the baseline report
(`../2026-11-05-regalloc/REPORT.md`):
- **Measured:** read from the files in `data/`.
- **Inferred:** an explanation of measured data that was not itself measured.
  No hardware counters were available.

## Summary

- *Measured:* both pre-registered thresholds of D-026 are met.
  - Dynamic memory operations fall by 50.2–99.8% against SSA on b02, b03,
    b04, b07, and b09 (threshold: at least 50% on each). b04 passes only
    narrowly.
  - CPU time improves by 18–70% against SSA on all 9 workloads, with
    non-overlapping min–max ranges (threshold: at least 10% on at least 5).
- *Measured:* the other D-026 criteria hold. The checks are below
  (§Criteria).
- *Measured:* dynamic instructions change by 0–14% only, since selection is
  unchanged, while CPU time falls by 18–70%.
- *Inferred:* this supports the baseline's inference that memory dependency
  chains through stack cells, not instruction count, dominated the cost.
  It remains an inference, without counters.

## Environment and method

The environment is that of the baseline report: an AMD Ryzen 7 7435HS under
WSL2 (kernel 6.6.87.2-microsoft-standard-WSL2), go1.26.1, GNU as and ld
2.42, no `perf`. Its limitations apply unchanged: the host is not controlled,
CPU frequency is unknown, and pinning fixes only the guest CPU.

- `TestBenchOracle` and `TestBenchMeasure` built all five variants and
  checked every executable, plain and instrumented, against the recorded gc
  output.
- `TestBenchTime` made 15 runs per executable, after a warm-up run, pinned
  with `taskset -c 6`. The load average was 0.50 at the start.
- **Control:** the `slot` and `ssa` rows of `summary.tsv`, `functions.tsv`,
  and `ops.tsv`, and all of `pressure.tsv` and `model.tsv`, are
  byte-identical to the baseline's. Neither variant's code changed.
- **Session agreement:** the slot, SSA, and gc medians of this session are
  within 3.3% of the baseline's session 1 (24 of 36 within 1%).

## Results (measured)

### CPU time (ms, median of 15)

| Workload | slot | ssa | ra | ra/ssa | ra/gcN |
|---|---|---|---|---|---|
| b01_fib | 301.8 | 258.7 | 175.6 | 0.679 | 1.72 |
| b02_primes | 306.2 | 175.3 | 143.5 | 0.819 | 1.40 |
| b03_collatz | 601.9 | 331.6 | 175.1 | 0.528 | 1.20 |
| b04_matpow | 1046.9 | 541.6 | 364.9 | 0.674 | 1.66 |
| b05_hash | 662.7 | 423.1 | 126.4 | 0.299 | 1.43 |
| b06_vm | 590.4 | 453.1 | 236.1 | 0.521 | 1.39 |
| b07_gcd | 705.6 | 477.1 | 392.8 | 0.823 | 1.02 |
| b08_ackermann | 382.7 | 296.5 | 220.8 | 0.745 | 1.56 |
| b09_crc | 450.4 | 317.9 | 130.1 | 0.409 | 1.25 |

- The RA-0 and SSA min–max ranges overlap for no workload
  (`data/times_summary.tsv`).
- RA-0 is 1.2–3.1× slower than gc and 1.02–1.72× slower than gc
  `-N -l`. gc binaries include a full runtime, so this is not
  like-for-like (`BENCHMARKS.md`).

### Dynamic instructions and memory operations

| Workload | ra/ssa instrs | ra/ssa mem ops | mem ops removed | ra cell ld+st |
|---|---|---|---|---|
| b01_fib | 1.000 | 0.574 | 42.6% | 3.52e8 |
| b02_primes | 0.959 | 0.016 | **98.4%** | 1.20e7 |
| b03_collatz | 0.864 | 0.002 | **99.8%** | 4.20e6 |
| b04_matpow | 0.950 | 0.498 | **50.2%** | 2.20e9 |
| b05_hash | 0.987 | 0.294 | 70.6% | 2.30e8 |
| b06_vm | 0.992 | 0.224 | 77.6% | 8.70e7 |
| b07_gcd | 0.979 | 0.115 | **88.5%** | 1.44e8 |
| b08_ackermann | 1.000 | 0.393 | 60.7% | 6.27e7 |
| b09_crc | 0.904 | 0.079 | **92.1%** | 5.40e7 |

Bold marks the five workloads the memory threshold names.
- Instructions fall only where φ copies coincide in a register and are
  omitted (b03: 14%). Otherwise selection is unchanged: every operand is
  still moved through `%rax`, now from a register.
- The remaining memory operations are ABI-S0's: arguments, results, return
  addresses, and the saved `%rbp`, which RA-0 does not touch (b01, b08); plus
  values live across calls, which keep their cells.
- **b04_matpow** needs 17 registers in its hot loop (baseline §3). Spilling
  everywhere leaves half of its memory operations, so it passes the
  threshold narrowly. This is the case the baseline report named for live
  range splitting.

### Static size and stack

- **S2 suite** (239 programs, `data/ra-suite.log`), against SSA without
  registers:
  - instructions 176,641 → 173,586 (−1.7%);
  - frame bytes 332,928 → 89,472 (−73.1%);
  - largest frame 10,480 → 2,752 bytes;
  - `aether.mainstack` summed over programs 324,144 → 90,464 (−72.1%).
- **Workloads:** frame bytes fall by 49–90%, and `.text` of the `main.*`
  functions by 7–41%. *Inferred:* the code shrinks because register
  operands encode shorter than `%rbp`-relative ones.
- **Peak run-time stack:** b08_ackermann 1,572,776 → 393,240 bytes (−75%),
  and b01_fib 4,200 → 1,832 bytes.

## Criteria (D-026)

1. **Identical results.** All 239 S2-suite programs built with registers give
   the interpreter's stderr and exit status (`TestNativeRASuite`). The nine
   workloads give gc's recorded output, plain and instrumented
   (`TestBenchMeasure`). **Met.**
2. **The checker on every function, also in `FuzzGenerated`, natively.**
   Emitting with registers runs `checkAllocation` on every function, and
   fails on any error. `FuzzGenerated` emits every accepted program with
   registers and runs it natively when it finishes. 60 s of fuzzing ran
   17,560 executions with no failure (`data/fuzz-generated-60s.log`).
   **Met.**
3. **Deterministic assembly.** Every S2-suite program and every fuzz input
   is emitted twice and compared byte for byte. **Met.**
4. **Planted bugs detected by permanent tests.** This was recorded in D-026,
   part 2: each of the five planted bugs failed at least one permanent
   test. **Met.**
5. **Reported whatever the result.** This report. **Met.**

Thresholds:
- Memory operations at least 50% lower on b02, b03, b04, b07, and b09:
  **met** (b04 at 50.2%).
- CPU time at least 10% better than SSA, with non-overlapping ranges, on at
  least 5 of 9 workloads: **met on 9 of 9**.

## What it does not show

- **Under the call threshold:** values live across calls stay in cells.
  In b01 and b08 most remaining memory operations are ABI-S0 call
  sequences, which a register convention (the D-011 successor) would
  address, not RA-0.
- **Selection:** instructions are within 14% of SSA's. The baseline's
  modeled full allocator is at 0.49–0.72× SSA's instructions (modeled, not
  measured). The gap is instruction selection: `cmp; jcc`, immediates, and
  direct register operands.
- **Representativeness:** these are the synthetic S2 kernels of the
  baseline. There is no memory, no arrays, and no heap.
- **Built executables:** nothing here changes them. Whether any allocator
  reaches `aether build` is for the owner to decide in a new entry
  (D-026).

## Data files

All in `data/`:

| File | Contents |
|---|---|
| `summary.tsv`, `functions.tsv`, `ops.tsv` | as in the baseline, with `ra` rows; `slot` and `ssa` rows identical to the baseline |
| `times.tsv`, `times_summary.tsv` | raw times (15 runs, five variants) and their summary, with the ra/ssa table |
| `environment.txt` | the timing session's environment |
| `ra-suite.log` | `TestNativeRASuite`'s static figures |
| `fuzz-generated-60s.log` | the complete 60 s `FuzzGenerated` run, with exit status |

`pressure.tsv` and `model.tsv` are unchanged and are not repeated: see the
baseline's `data/`.
