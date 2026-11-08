# Compiler memory and scalability (2026-11-08)

The two metric categories of `BENCHMARKS.md` that the Phase 5 report left
uncovered: the compiler's memory, per stage and as a process, and compile
time and memory against program size. Every figure was measured; hypotheses
are marked.

**Environment.** WSL2 (kernel 6.6.87.2-microsoft-standard-WSL2) on an AMD
Ryzen 7 7435HS, pinned to CPU 6; go1.26.1; GNU as and ld 2.42; GNU time
(Ubuntu's, version string "UNKNOWN") for peak RSS; commit 44202d4 plus the
harness of this change, which touches no compiler code
(`data/environment.txt`). Caches warm, with a discarded warm-up build.

**Method.** `TestBenchMemory` and `TestBenchScale` (`bench_scale_test.go`) run
the stages of `aether build` in process with the compiler's own functions,
after a collection before each stage, and check for every program that they
build exactly the assembly `emitProgram` builds. Per stage they record wall
time, bytes and objects allocated, and the heap live after the stage (after a
collection; it includes the test process's own heap). Peak RSS is that of an
`aether build` process, measured by GNU time, without as and ld. The ru_maxrss
of a process the test starts itself is not usable: Go starts children sharing
its address space until exec, and Linux keeps that high-water mark across it,
which made the first trial report the test's RSS (8.8–12.7 MiB instead of
4.6–8.4).

## Memory on the workloads and the largest corpus programs

`data/memory_summary.tsv` (peak RSS: median of 11 processes, with min–max),
`data/memory.tsv` (by stage).

| Program | Source bytes | Allocated MiB | of which emit | Live heap max MiB | Peak RSS MiB |
|---|---|---|---|---|---|
| b01_fib … b09_crc | 181–1,134 | 0.11–0.63 | 0.03–0.33 | 0.18–0.26 | 4.6–5.2 |
| s2_a02_state_machines | 2,709 | 1.07 | 0.61 | 0.35 | 5.8 |
| s2_i01_integer_tables | 4,618 | 1.91 | 0.85 | 0.59 | 6.6 |
| s2_d01_division_shifts | 6,563 | 3.30 | 1.90 | 0.75 | 8.4 |

- Emission allocates the most: 27–58% of each program's bytes, 51% over all
  of them. The frontend (parse, typecheck, gate) allocates 12–28%.
- Peak RSS is 4.6–8.4 MiB; run-time overhead dominates it on the workloads.

## Scalability

Synthetic programs at sizes n = 16 to 4,096, doubling (`scaleAxes`), each
also run natively against the interpreter. Times are medians of 7 builds;
`data/scale.tsv` has every stage, and `data/scale_slopes.tsv` the growth
exponent between consecutive sizes (log2 of the ratio at 2n to n: 1 is linear,
2 quadratic).

- `funcs`: n small functions, each calling the previous one.
- `stmts`: one function of n assignments over four variables.
- `nest`: n nested `if`s, each declaring a variable (blocks and slots grow
  together).

| Axis | n | Compiler ms (no assemble) | Assemble ms | Allocated MiB | Peak RSS MiB | `.text` bytes |
|---|---|---|---|---|---|---|
| funcs | 256 / 4,096 | 9.7 / 200.4 | 17.3 / 76.2 | 4.8 / 77.3 | 9.2 / 44.1 | 13,201 / 209,041 |
| stmts | 256 / 4,096 | 3.9 / 89.4 | 14.2 / 62.9 | 1.9 / 33.7 | 6.6 / 18.9 | 1,969 / 252,655 |
| nest | 256 / 512 | 59.3 / 351.9 | 19.8 / 24.7 | 8.0 / 18.3 | 10.2 / 14.9 | 27,209 / 54,601 |
| nest | 4,096 | 1,852.7 | 121.7 | 322.6 | 141.8 | 573,320 |

**Measured:**
- `funcs` and `stmts` are linear in size, apart from the limits below:
  compiler time exponents 0.5–1.2, allocation 0.6–1.1. Typechecking and
  emission are the largest compiler stages there.
- `nest` is superlinear in four stages, by profile:
  - promotion grows about as n³, exponents 2.5–2.9 from n = 64 to 512,
    reaching 299 ms at n = 512. 650 of 830 profiled ms are its live-slot
    fixpoint (`internal/ir/promote.go:57`), which repeats a blocks × slots
    sweep until nothing changes.
  - the IR verifier's definite-assignment check
    (`ir.(*verifier).definitelyStored`), run after lowering and again in
    `EmitWith`, gives "lower" exponents of 1.7–1.9, and "emit" ones of
    1.4–1.6 once the function is slot-based.
  - go/types' scope lookup through nested scopes gives "typecheck"
    exponents of about 1.7–1.9.
  - At n = 4,096 (221 KB of source), a build takes 2.0 s, allocates
    323 MiB, and peaks at 142 MiB RSS.
- The backend limits make cliffs rather than curves:
  - `maxPromoteBlockSlots` stops promotion of `nest` at n = 1,024, which is
    why its total time falls from n = 512 to 1,024.
  - `maxAllocValues` leaves `stmts` main (2,058 values after the passes,
    1,034 at n = 512) without registers at n = 1,024:
    `.text` grows 9.2× from n = 512 for twice the statements.
  - `maxPromoteInstrs` leaves it slot-based at n = 2,048: `.text` grows
    another 4.3×.
- `as` and `ld` take most of the build of small programs in every
  axis, as the Phase 5 report found on the workloads; the compiler overtakes
  them at about n = 512 for `funcs`, 2,048 for `stmts`, and 128 for `nest`.

**Hypotheses, not measured:** the promotion fixpoint needs about as many
sweeps as the nesting depth because its block order is not a postorder of the
nested structure; a worklist or a reverse-postorder sweep would make it near
linear. The verifier's definite-assignment check is likely quadratic for the
same reason. Neither is changed here.
