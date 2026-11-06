# Calling convention investigation (2026-11-06)

**Recommendation: run a narrow experiment, ABI-R1.** It passes the first six
arguments and a single result in registers and keeps every register caller-saved,
for Aether-to-Aether calls in the default build only. Nothing here changes the
compiler, and D-011 stays as accepted.

- *Measured:* on an assembly-level model of R1, median CPU time falls 30% on b08,
  20% on b05, 13% on b01, 9% on b06, and 5% on b07. b02, b03, b04, and b09 do not
  change.
- *Measured:* code placement alone moves some workloads by up to 57%. Conclusions
  that hold at one layout can vanish at another, so the experiment's timing
  criteria use a sweep of code offsets.

**How to read this report.** *Measured* means read from `data/`. *Inferred* means
an explanation of measured data that was not itself tested. No hardware counters
are available here.

## Method

The compiler was not changed. A probe outside the repository, in a scratch copy of
the tree, rewrites the assembly of `aether build` into each candidate convention:

| Variant | Convention |
|---|---|
| `isel` | The shipped build, ABI-S0 |
| `nohop` | ABI-S0, without the `%rax` hop between a register and an ABI word |
| `r1` | ABI-S0 except: arguments 0-5 in `%rdi %rsi %r8 %r9 %r10 %r11`, a single result in `%rax`, frames without the outgoing area where nothing uses it. Parallel copies at entry and at each call |
| `r1leaf` | `r1`, and functions with no frame use and no calls except to panic helpers lose the frame and stack check |
| `r1cs` | `r1`, and `%rbx %r12-%r15` become callee-saved: the most-used cells of a function that calls move there, saved on every path |
| `h_r1`, `h_cs` | Hand-written `fib` and `ack`: `r1` with coalescing; callee-saved with saves only on recursive paths |

The dynamic counts come from the harness of `BENCHMARKS.md`. Every timed executable
must exit 0 with the recorded gc output. There are two timing sets, pinned to one
CPU with a discarded warm-up: 15 round-robin runs at the shipped layout, and a
sweep of 5 runs at each of 8 code offsets modulo 64, which shifts the program's
whole text. "Median of offsets" is the median of the eight per-offset medians.
Environment: `data/environment.txt`.

**Validity checks (measured).**
- Each variant gave the interpreter's stderr and exit status on all 239 S2 suite
  programs, including those that overflow the stack, with the runtime unchanged
  (`data/abi_suite.txt`).
- `isel` assembly is byte-identical to `ca5dbe2`'s on all nine workloads.
- `r1` assembly on b03 and b04 is byte-identical to `isel`, which gives a noise
  control: the ratios are 0.998-1.002.

## Where call cost goes today

Shares of dynamic instructions of `isel` (`data/abi_attr.tsv`). Earlier reports
gave 53-56% for fib and Ackermann. Instruction selection shrank everything else, so
the share of call sequences is now higher, but less than half of it is transfer.

| Workload | Calls | Instrs per call | Argument and result transfer | Check, frame, call, return | Cells for values across calls |
|---|---|---|---|---|---|
| b01_fib | 78.2M | 24.5 | 32.7% | 44.9% | 10.2% |
| b08_ackermann | 62.7M | 28.0 | 42.9% | 39.3% | 1.8% |
| b05_hash | 40.0M | 61.2 | 18.0% | 16.3% | 9.4% |
| b06_vm | 16.0M | 60.0 | 6.7% | 16.7% | 9.1% |
| b07_gcd | 16.0M | 159.2 | 7.5% | 6.3% | 4.4% |
| b09_crc | 3.0M | 195.0 | 6.2% | 5.1% | 8.7% |
| b02_primes | 1.5M | 835.1 | 0.9% | 1.2% | 0.8% |

b03 and b04 make no Aether calls. Transfer is a larger share of memory operations:
44.4% on fib, 54.2% on Ackermann, 35.8% on b05, and 40.0% on b07
(`data/abi_attr_mem.tsv`).

## Results

Change against `isel`. Counts are deterministic (`data/abi_summary.tsv`).

| Workload | Dyn. instrs: nohop / r1 / r1leaf / r1cs | Mem ops: r1 | Text: r1 | Peak stack: r1 |
|---|---|---|---|---|
| b01_fib | -8.2 / -16.3 / -16.3 / 0.0% | -29.6% | -17.6% | -33.2% |
| b02_primes | -0.3 / -0.4 / -1.4 / -0.4% | -23.5% | -5.6% | -18.2% |
| b05_hash | -8.2 / -9.8 / -22.9 / -9.8% | -35.8% | -11.9% | -19.0% |
| b06_vm | -3.3 / -3.3 / -16.7 / -5.0% | -11.7% | -2.0% | -15.4% |
| b07_gcd | -2.5 / -4.4 / -9.4 / -5.0% | -30.0% | -8.3% | -26.7% |
| b08_ackermann | -19.6 / -26.8 / -26.8 / -25.0% | -50.0% | -25.5% | -33.3% |
| b09_crc | -2.6 / -3.6 / -7.7 / -3.6% | -20.7% | -6.1% | -26.7% |

CPU time as a ratio to `isel`, median of offsets (`data/abi_layout_summary.tsv`).
The range is the lowest to highest per-offset median, in ms. The last column is the
15-run ratio at the shipped layout (`data/abi_times_summary.tsv`).

| Workload | `isel` range | nohop | r1 | r1leaf | r1cs | `r1` range | r1, shipped layout |
|---|---|---|---|---|---|---|---|
| b01_fib | 171.5-178.6 | 0.968 | **0.874** | 0.874 | 0.848 | 152.5-156.0 | 0.879 |
| b02_primes | 99.6-101.1 | 0.995 | 0.993 | 0.989 | 0.990 | 99.6-100.1 | 1.004 |
| b03_collatz | 98.0-112.1 | 0.998 | 1.000 | 0.997 | 1.001 | 97.6-112.0 | 0.998 |
| b04_matpow | 312.6-313.9 | 1.000 | 1.002 | 1.001 | 1.000 | 312.9-313.9 | 0.999 |
| b05_hash | 125.2-134.6 | 0.945 | **0.801** | 0.684 | 0.742 | 100.2-133.3 | 0.813 |
| b06_vm | 189.2-210.8 | 1.002 | **0.913** | 0.874 | 0.712 | 186.8-192.1 | 0.895 |
| b07_gcd | 345.3-368.2 | 0.990 | **0.952** | 0.941 | 0.942 | 345.2-349.9 | 1.001 |
| b08_ackermann | 212.8-267.8 | 0.979 | **0.696** | 0.706 | 0.717 | 154.6-228.3 | 0.601 |
| b09_crc | 89.0-93.5 | 0.991 | 1.001 | 0.994 | 0.973 | 88.7-94.7 | 0.986 |

- *Measured:* `r1`'s range clears `isel`'s only on b01. Ranges overlap on b05, b06,
  b07, and b08, but offset by offset `r1` is faster at all 8 offsets on b07 and b08
  (b08 by only 0.1 ms at one) and at 7 of 8 on b05 and b06.
- *Measured:* b07's gain appears only across offsets. At the shipped layout its ratio
  is 1.001, so it is not an acceptance criterion below.
- *Measured:* against gc at the shipped layout, b01 goes from 1.88× to 1.66×, b05
  from 3.11× to 2.53×, and b08 from 2.15× to 1.29×.

### Code placement

- *Measured:* b03 at the shipped layout looks 13% faster under `r1cs`. Across
  offsets the ratio is 1.001, so that gain was placement alone.
- *Measured:* `nohop` looks 9% faster on b08 at the shipped layout, and 2% across
  offsets.
- *Measured:* the per-offset spread of b08 is 57% under `ra`, 24% under `isel`, and
  45% under `r1`.
- *Measured, and relevant to D-029:* across offsets b08's `isel/ra` ratio is 1.10
  (233.1 against 212.3 ms), with overlapping ranges, against 1.20 at the shipped
  layout. This narrows the size of the open regression and does not explain it.
- *Measured:* b03's shipped `isel` layout (112.8 ms) sits at the slow end of its
  range (98.0-112.1 in the sweep). Single-layout results in earlier reports carry
  this uncertainty.

## The questions asked

**Argument and result transfer.** Passing values in memory is the part worth
changing, and removing the `%rax` hop alone is not enough: `nohop` removes up to
19.6% of instructions and at most 5.5% of time. `r1` removes the memory traffic as
well, and time falls by more than instructions on b05 (-9.8% instructions, -19.9%
time) and b06 (-3.3%, -8.7%). *Inferred:* the cost is store-to-load dependence
through the stack rather than instruction count, as in the RA-0 report.

**Caller-saved against callee-saved.** The result depends on the callee.
- *Recursion:* about a wash, as the baseline report predicted. `r1cs` against `r1`
  is 0.848 against 0.874 on fib and 0.717 against 0.696 on Ackermann. The hand
  versions with saves only on recursive paths agree on fib (0.842 and 0.839).
- *Leaf callees:* a clear gain, because their callers' values stay in registers and
  the leaf saves nothing. b06 goes from 0.913 to 0.712, and b05 from 0.801 to 0.742.
  **This contradicts the baseline report's statement that leaf functions gain
  nothing.** That statement looked at the callee and not at its caller.
- *Cost:* `r1cs` saves on every path, which adds moves: fib's dynamic instructions
  stay level where `r1` removes 16.3%. Text grows 5.7% on b03 and b04, which have no
  hot calls. Values in callee-saved registers also complicate
  precise stack maps (D-010).

**Values across calls and register pressure.** Per executed call, 0.5 values cross
on Ackermann, 1 on fib, 3 on b02 and b06, 4 on b07 and b09, and 5 on b05. In the
suite, 405 of 418 functions have none. R1 needs no allocator change, because no
register value crosses a call today, so all argument registers are free at each
call. It also doesn't relieve pressure: b04's cost is spilling inside a loop with no
calls, and no variant here helps it. Coalescing does not justify allocator work in
a first step: `h_r1` gains on fib (0.842 against 0.874) but loses on Ackermann
(0.844 against 0.696), despite fewer instructions.

**Recursion, nesting, multiple results, and stack overflow.**
- *Recursion and nesting:* arguments are set up immediately before `call`, and
  results are copied out immediately after, so D-024's order is untouched. The 6
  suite call sites that directly take another call's result ran correctly.
- *Multiple results:* the workloads have none, and 21 of 423 suite call sites do.
  The model leaves them on the stack as in ABI-S0, and they ran correctly.
- *Arity:* suite functions take at most 4 parameters, so six registers cover
  everything seen, and **the path for more than six arguments has not been run**.
- *Stack overflow:* the check still runs first and needs only `%rax`, which stays out
  of the argument registers as D-011 requires. The overflow path does not return, so
  it needs no argument preserved. Frames shrink, so recursion goes deeper before
  overflowing. V-2 already allows a different depth.
- *Future stacks:* a growable goroutine stack (Phase 10, undecided) would have to
  preserve argument registers on its growth path.

**Compatibility.**
- *Runtime:* `rt/*.s` needs no change. Print helpers already take `%rdi` and may
  clobber it, panic helpers take no arguments and do not return, and `_start` calls
  functions with no arguments or results.
- *`-O0`:* stays on ABI-S0, byte-identical. A build uses one convention throughout,
  since there is no separate compilation, so the two never mix.
- *Functions above the size limits:* they stay in the default build and must use R1
  like any other. The rewriter handles their form, but **no such function has run
  under it**.
- *Harness:* `slot`, `ssa`, and `ra` stay on ABI-S0, so earlier results stay
  comparable.

## Designs compared

| Design | Time, call-heavy workloads | Cost and risk | Verdict |
|---|---|---|---|
| A. ABI-S0, no `%rax` hop | 0-5.5% | Small, no ABI change | Not enough on its own, and R1 subsumes it |
| **B. R1: registers for 6 arguments and 1 result, all caller-saved** | 5-30% | Call and entry sequences; no allocator or runtime change | **Recommended** |
| C. B plus frameless leaves | b05 32%, b06 13%, others within about 1 point of B | Amends D-011's mandatory frame chain and skips a stack check; the reserve bound must be restated | Next, as its own step |
| D. B plus callee-saved registers | b06 29%, b05 26%, recursion about equal | Allocator change, saves in callees, harder stack maps | Not now |
| E. System V or Go ABIInternal | Not measured | No foreign boundary exists; gc's fixed registers presuppose its runtime (D-011) | No reason to adopt |

D's gain comes from values staying in registers across leaf calls. Since the whole
program is compiled at once, a caller could get the same benefit from knowing which
registers a leaf clobbers, with no saves and no effect on stack maps at calls that
cannot reach a safepoint. *That option is not measured here*, and should be compared
with D if this gain is pursued.

## Proposed experiment: ABI-R1

**Scope.** An `EmitOptions` flag, off by default and test-only until accepted, like
RA-0. Arguments 0-5 in `%rdi %rsi %r8 %r9 %r10 %r11`, a single result in `%rax`, and
everything else in ABI-S0 positions. `%rbp`, the frame chain, and the prologue check
are unchanged. No value stays in a register across a call. Entry and call-site moves
are parallel copies.

**Out of scope.** Frameless leaves, callee-saved registers, allocator hints, tail
calls, code alignment, and changes to the IR, `rt/*.s`, exit codes, or `-O0`.

**Correctness criteria.** Each must pass with the flag on:
1. All of `go test ./...`, `gofmt`, `go vet`, and 60 s of `FuzzGenerated`, with the
   fuzz check covering the flag.
2. The S2 suite matches the interpreter; `TestSelectDifferential` and the workload
   gc outputs match; `TestO0CrossCheck` passes across the two conventions.
3. New tests run in registers and in cells: 0-8 arguments, including more than six;
   1-3 results; every permutation of arguments among registers that forces a copy
   cycle; arguments as immediates, cells, and large constants; nested calls; mutual
   recursion; D-024 order with panics in arguments; functions above each size limit.
4. Stack boundary tests are restated for the new frame sizes: E = T, E = T − 16, and
   overflow with arguments in registers. Peak stack is no higher on any workload.
5. `checkAllocation` or a new checker independently verifies each call-site and entry
   copy; a failure is an `internal` error.
6. New negative controls each get caught: swapped argument registers, a dropped cycle
   break, a stale `%rax` result, a seventh argument read from a register, and a frame
   without a needed outgoing area.
7. `-O0` output and the `slot`, `ssa`, `ra`, and `isel` rows are byte-identical to
   today; the assembly is deterministic.

**Performance criteria.** These are fixed in advance and compared with `isel` at the
same commit. Timing is the median of 8 code offsets with at least 7 runs each:
1. Dynamic instructions and memory operations are no higher than the `r1` row of
   `data/abi_summary.tsv` on every workload. b03 and b04 assembly stays byte-identical
   to `isel`.
2. CPU time improves by at least 10% on each of b01, b05, and b08, and by at least 5%
   on b06. The model gives 12.6%, 19.9%, 30.4%, and 8.7%.
3. No workload is more than 2% slower.

Missing any of them means R1 does not ship, and the report states the result whatever
it is. Reaching beyond this scope, for example needing allocator changes to meet a
criterion, also stops the experiment and returns to the owner.

**Estimate, not measured:** about 150 lines in the backend and 400 of tests, within a
single reviewable change, with a separate change to ship.

## Limits

- The model is an assembly rewrite, checked by outputs. It shows what the convention
  allows and is not an implementation. It ran on the suite as well as the workloads,
  but its paths for more than six arguments and for functions above the size limits
  did not run.
- The workloads pass at most two arguments and no multiple results.
- One CPU under WSL2, synthetic kernels, no hardware counters. The offset sweep shifts
  the whole program and is not a full exploration of layout, and each offset has 5 runs.
- `r1cs` is deliberately simple: it picks cells by static use and saves on every path.
- Compile latency was not measured.
- The probe (about 1,100 lines) is not in the repository, like earlier out-of-tree
  experiments. Only its outputs are in `data/`.
