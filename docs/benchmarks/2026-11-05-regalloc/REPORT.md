# Register-allocation baseline (2026-11-05)

Measured at Aether commit `a3b64c7` on 2026-11-05, under WSL2. The
investigation changed no compiler source, test, or document. `gofmt`, `go vet
./...`, and `go test ./...` pass at `a3b64c7` (`data/baseline-validation.txt`).
The workloads and the harness were checked in afterwards, unchanged in what
they measure (§1, "Provenance"). The next experiment it recommends is
**Proposed** as D-026 and is not approved.

*Update 2026-11-06:* the owner accepted D-026, so RA-0 is approved and in
progress. The report is otherwise kept as written; "Proposed" below records
the status when it was written.

**How to read this report.** Every claim is one of:
- **Measured:** read from the data files in `data/`.
- **Inferred:** an explanation of measured data that was not itself measured.
  No hardware counters were available, so every statement about *why* code is
  slow (latency, store forwarding) is inferred.
- **Modeled:** computed from measured counts under stated assumptions
  (§6, "Model estimate"). Not a measurement and not a prediction of time.
- **Proposed:** a recommendation awaiting owner approval (D-026).

**Summary.**
- *Measured:* SSA promotion alone (D-006, still test-only) reduced dynamic
  instructions by 12–35% but CPU time by 14–48%.
- *Inferred:* on these workloads the main cost of the current backend is
  **memory dependency chains through stack cells**, not instruction count,
  so getting values out of stack cells is worth doing.
- *Measured:* register pressure in S2 is low: in 99% of suite functions at
  most 13 values (constants excluded) are live at once.
- *Measured:* about half of all dynamic instructions are outside the reach
  of register allocation: compare/branch selection, constant
  materialization, and ABI-S0 call overhead.
- *Proposed (D-026):* a narrow, test-only **SSA-direct allocator that keeps
  the current per-op instruction selection**. It isolates the memory-traffic
  effect, before any machine IR or ABI change.

---

## 1. Methodology (reproducible)

**Environment.**

| Item | Value |
|---|---|
| CPU | AMD Ryzen 7 7435HS, 8 cores / 16 threads (frequency and governor not controllable under WSL2) |
| OS | Linux 6.6.87.2-microsoft-standard-WSL2 (**virtualized; results labeled WSL2**) |
| Toolchain | go1.26.1 linux/amd64 (builds Aether and the gc oracles); GNU as and ld 2.42 |
| Tools | `perf`, `valgrind`, `gdb`, and `hyperfine` are **not installed**; no hardware counters were used |

**Limitations of this environment.**
- WSL2 is a virtual machine on a Windows host. Host activity outside the VM
  is neither visible nor controlled, and adds noise.
- CPU frequency, boost, and governor cannot be set or read from inside the
  VM. They are unknown and were not held fixed.
- Pinning (`taskset -c 6`) fixes the guest CPU only. It does not isolate the
  core from its SMT sibling or from host scheduling.
- No PMU access was attempted (`perf` is not installed), so there are no
  cycle, cache, or store-forwarding counts.
- One machine, two timing sessions. The measured spread (§2.1) bounds the
  noise only for these sessions.

**Workloads** (`cmd/aether/testdata/bench/*.go`, S2 only). All nine are
accepted by `aether check`.
Each output is identical to a `go build` binary (`*.gcout`) for the slot,
SSA, and instrumented builds.

| Workload | Shape |
|---|---|
| b01_fib | naive recursive fib(37): call-dominated |
| b02_primes | trial division to 1.5M: `%` and compare-heavy, leaf call per n |
| b03_collatz | nested loops, shifts, no calls |
| b04_matpow | 3×3 matrix product mod p: high pressure, `*`, `%` |
| b05_hash | uint64 mixing with a small `rotl` function |
| b06_vm | switch dispatch, globals, call per step |
| b07_gcd | leaf `gcd` called from a 4000×4000 loop |
| b08_ackermann | deep recursion (about 8,190 frames) |
| b09_crc | bitwise CRC-32, uint8/uint16/int32 canonicalization |

**Variants.**
- `slot`: what `aether build` produces today (verified byte-identical for
  three workloads).
- `ssa`: every function promoted with `ir.Promote`, then the same backend.
- `gc` and `gcN`: `go build` and `go build -gcflags=all='-N -l'`.

gc is used only as a reference point. Its binaries include the full runtime,
so the comparison is not like-for-like (BENCHMARKS.md).

**Harness** (`cmd/aether/bench_*_test.go`; `BENCHMARKS.md`, "The
harness"). It is test code that uses `compile`, `promoteProgram`,
`amd64.Emit`, and `assemble`.
- **Dynamic counts without perf.** A profiling copy of each executable gets a
  64-bit counter at the start of every straight-line region of the emitted
  assembly (after each label and each conditional jump), plus a min-`%rsp`
  probe in each entry block.
  - Count × static region contents gives exact dynamic machine instructions
    and memory operands, classified as: slot or value cell, own args, outgoing
    area, global, stack-limit load, and push/pop/call/ret.
  - Each instruction is also attributed to the IR op that emitted it.
  - Runtime print helpers are excluded.
  - The probes change only dead flags. Instrumented output matches gc.
  - **Check:** the measured calls in b01 are 78,176,337 = 2·fib(38) − 1.
    The harness's tests check call, block, fall-through, and peak-stack
    counts of small programs the same way.
- **Register pressure.** Liveness of SSA values (φ operands live out of the
  edge's source), computed statically and weighted by measured block
  frequencies. Constants are excluded where noted, since they are
  rematerializable or immediates. The pressure at an instruction counts what
  is live before it plus its result, so it is an upper bound, by at most
  one, on the registers the instruction needs.
- **Timing.** CPU time (user+sys, `rusage`) and wall time.
  - Round-robin over the variants, 1 discarded warm-up plus 15 runs each,
    pinned with `taskset -c 6`, warm caches, idle machine.
  - A second session (7 runs) moved no median by more than 3.3%, and every
    SSA/slot ratio was within 0.004.
- **Static measures.** The D-006 method (`TestNativeSSASuite`), re-run at
  HEAD with identical results.

**Reproduce.** From the repository root at `a3b64c7` or later with the
harness, `OUT` an empty directory (the commands are also in
`BENCHMARKS.md`):
```
AETHER_BENCH_OUT=$OUT go test -count=1 ./cmd/aether -run 'TestBenchOracle$' -v
AETHER_BENCH_OUT=$OUT go test -count=1 -timeout 30m ./cmd/aether \
  -run 'TestBenchMeasure$|TestBenchSuitePressure$' -v
AETHER_BENCH_OUT=$OUT AETHER_BENCH_RUNS=15 taskset -c 6 \
  go test -count=1 -timeout 30m ./cmd/aether -run 'TestBenchTime$' -v
```
`summary`, `functions`, `pressure`, `ops`, `model`, and `suite_pressure`
`.tsv` are deterministic and must be byte-identical to `data/` while the
compiler's output is unchanged. Times vary; compare medians and ranges.

**Provenance.** The data in `data/`, except session 3, were produced on
2026-11-05 by the same measurement code as a test file overlaid with `go test
-overlay`. The gc variants were built by hand with `GO111MODULE=off go build`
(and `-gcflags=all='-N -l'`), and the times were summarized by a Python
script. When the
harness was checked in (2026-11-06):
- the checked-in harness, run at `a3b64c7`, reproduced all six deterministic
  files byte-identically;
- the Go summary (`times_summary.tsv`) reproduced the Python script's output
  byte-identically on both recorded sessions (`data/times_session*_summary.tsv`);
- the gc variants are now built by `TestBenchOracle`, in module mode, and
  checked against the recorded outputs;
- a third timing session (2026-11-06, `data/times_session3*`) used the
  checked-in harness end to end: `a3b64c7` plus the uncommitted harness
  ("modified" in its environment file), 15 runs, `taskset -c 6`, load
  average 0.99 at its start. Every CPU median was within 2.2% of session 1
  (23 of 36 within 1%), and every SSA/slot ratio within 0.004. The environment
  file's `cpus` line was relabeled `usable cpus` afterwards, matching the
  harness's final output; no value changed.

The tables below use session 1.

## 2. Baselines

All of §2 is **measured**.

### 2.1 Run time (CPU ms, median of 15; IQR ≤ 1.5% everywhere)

Ratios are of the unrounded medians (`data/times_session1_summary.tsv`).

| Workload | slot | ssa | ssa/slot | gc | gcN | ssa/gcN |
|---|---|---|---|---|---|---|
| b01_fib | 306.3 | 262.5 | 0.857 | 94.6 | 103.6 | 2.53 |
| b02_primes | 314.1 | 178.6 | 0.569 | 99.6 | 104.0 | 1.72 |
| b03_collatz | 611.6 | 336.7 | 0.551 | 86.8 | 147.3 | 2.29 |
| b04_matpow | 1043.9 | 543.5 | 0.521 | 175.8 | 219.6 | 2.48 |
| b05_hash | 663.9 | 424.3 | 0.639 | 40.6 | 88.8 | 4.78 |
| b06_vm | 590.1 | 453.6 | 0.769 | 119.4 | 169.6 | 2.68 |
| b07_gcd | 707.9 | 478.8 | 0.676 | 328.2 | 384.7 | 1.24 |
| b08_ackermann | 384.3 | 296.9 | 0.773 | 129.1 | 141.2 | 2.10 |
| b09_crc | 451.0 | 318.7 | 0.707 | 76.0 | 104.3 | 3.05 |

- The slot and SSA min–max ranges do not overlap for any workload.
- Today's `aether build` (slot) is 2.2–16× slower than gc.
- SSA-without-registers is 1.2–4.8× slower than gc `-N -l`.

### 2.2 Dynamic instructions and memory traffic

| Workload | slot instrs | ssa instrs | ssa/slot instrs | ssa/slot **time** | ssa mem ops per instr | cell share of ssa mem ops | ABI share of ssa instrs |
|---|---|---|---|---|---|---|---|
| b01_fib | 3.21e9 | 2.66e9 | 0.829 | 0.857 | 0.69 | 62% | 55.9% |
| b02_primes | 4.11e9 | 2.91e9 | 0.709 | 0.569 | 0.56 | 99% | 1.0% |
| b03_collatz | 3.80e9 | 3.34e9 | 0.879 | **0.551** | 0.70 | 100% | 0.0% |
| b04_matpow | 8.07e9 | 5.28e9 | 0.654 | 0.521 | 0.84 | 100% | 0.0% |
| b05_hash | 4.34e9 | 3.14e9 | 0.724 | 0.639 | 0.73 | 81% | 29.3% |
| b06_vm | 2.28e9 | 1.90e9 | 0.834 | 0.769 | 0.64 | 85% | 12.6% |
| b07_gcd | 7.39e9 | 4.84e9 | 0.654 | 0.676 | 0.57 | 94% | 7.6% |
| b08_ackermann | 3.42e9 | 2.73e9 | 0.798 | 0.773 | 0.70 | 64% | 52.9% |
| b09_crc | 2.05e9 | 1.72e9 | 0.842 | 0.707 | 0.64 | 97% | 4.0% |

The "ABI" column counts the instructions of the prologue (stack check, push,
frame), `call`, `param`, and `ret`.

### 2.3 Static size and stack

- **S2 suite** (239 programs, 547 functions; unchanged from D-006): slot →
  SSA gives instructions 234,359 → 176,641, code bytes 1,419,429 → 1,034,373,
  and frame bytes 563,520 → 332,928.
- **Workloads:** SSA reduces `.text` of the `main.*` functions by 13–33% and
  frame bytes by 19–46%.
- **Peak run-time stack**, measured by the probe:
  - b08_ackermann: 2,097,000 → 1,572,776 bytes (−25%), about 8,190 frames.
  - b01_fib: 5,976 → 4,200 bytes.
  - All others: under 1.6 KB.

  Run-time stack is now measured, which D-006 left open.

## 3. Bottlenecks

Each item gives its **measured** evidence, then an **inferred**
interpretation. The interpretations were not tested with counters.

1. **Store→load chains on loop-carried values.**
   - *Evidence (measured):* in b03_collatz, SSA cuts instructions by 12% but time by
     45%. Throughput rises from 6.2 to 9.9 instructions/ns.
   - *Interpretation (inferred):* in every loop, each φ and temporary goes through memory,
     and the loop-carried path pays store-forwarding latency on each hop.
   - *Who addresses it:* the register allocator.
2. **Compare-and-branch and constants.**
   - *Evidence (measured):* compare ops (`eq`/`ne`/`lt`/`le`/`gt`) plus `branch` are
     29.4% of SSA dynamic instructions, and `const` is 7.6%.
   - *Interpretation (inferred):* each condition is `cmp; setcc; movzbl; store; load;
     test; jcc`, about 9 instructions where `cmp; jcc` would do. Constants
     are materialized into a cell at every use, including the divide-by-zero
     check's `0` in hot loops.
   - *Who addresses it:* instruction selection, which register allocation
     helps only partly.
3. **ABI-S0 call cost.**
   - *Evidence (measured):* 56% (fib) and 53% (ackermann) of instructions are call
     sequence: about 19 of 34 instructions per call in fib.
   - *Interpretation (inferred):* this is the stack check, the push/frame, arguments and
     results through memory, and `param` reloads.
   - *Who addresses it:* a register ABI (the D-011 successor), not the
     allocator.
4. **φ copies on every edge.**
   - *Evidence (measured):* jump/branch copy code is 9.6% of SSA dynamic instructions.
     b02's `main` runs **4% more** dynamic instructions under SSA than under
     slots, the first measured case where SSA is worse.
   - *Interpretation (inferred):* a copy runs on every edge even when the value is
     unchanged.
   - *Who addresses it:* the allocator, through coalescing.
5. **Division.** `rem`/`div` are 9.6% of dynamic instructions, and they need
   fixed `%rax`/`%rdx`.

### High-pressure functions (measured)

| | Value |
|---|---|
| Suite: max live values (constants excluded) | median 3, p90 11, p99 13, max 16 |
| Suite: functions fitting in 10 / 12 / 14 registers | 89.5% / 98.6% / 99.5% |
| Suite: corpus programs | max 11; all functions above 12 are generated |
| Workloads | all hot functions have max live ≤ 9, **except b04_matpow `main` (17)**, where 100% of executed instructions have more than 12 live values |
| Values live across a call | median 0, max 5 per call (suite); 1–5 per executed call (workloads) |

## 4. Register availability and ABI

- **Registers.** x86-64 has 16 GPRs. `%rsp` and `%rbp` are reserved: the
  frame-pointer chain is mandatory under D-011. That leaves 14.
- **Fixed constraints:**
  - `idiv`/`div` use `%rax` and `%rdx`;
  - variable shifts use `%cl`;
  - the prologue's stack check needs a free scratch register at entry
    (D-011 "Revisit when");
  - the runtime helpers take their argument in `%rdi`.
- **ABI-S0.** The callee preserves only `%rbp` and `%rsp`, so all 14 are
  caller-saved. Under an allocator, every value live across a call must be
  spilled around it. The measured cost is small: two memory ops times 0.5–5
  values per executed call. This is about 8% of fib's modeled instructions
  and 0.5–18% elsewhere (b05_hash, whose `main` keeps 5 values live across
  each `rotl` call, is the high end).
- **Callee-saved registers** would move this cost to `push`/`pop` in each
  callee that uses them. In fib and ackermann, about one value crosses each
  call, so it is a wash. The leaf functions (`gcd`, `isPrime`, `crc32`,
  `rotl`, `step`) gain nothing.
- **The bigger ABI cost is passing arguments and results through memory.**
  That is a D-011 successor question, separate from allocation.
- **Runtime helpers** preserve `%rbx`, `%rbp`, and `%r12`–`%r15`. Values live
  across `println` could use those for free, but `println` is not hot in any
  workload.

## 5. Options and trade-offs

### 5.1 SSA IR directly vs. a separate machine IR

| | Allocate on SSA IR (assignment table in `internal/amd64`) | Machine IR in `internal/amd64` |
|---|---|---|
| Fit with today | Selection is 1:1 per IR op; values are single words (tuples aside) | New IR, printer, and verifier; not executable by the interpreter |
| Constraints (div, shift, clobbers) | A per-op table; simplest if `%rax`/`%rcx`/`%rdx` stay reserved as scratch | Explicit tied and fixed operands |
| Selection wins (cmp+jcc, immediates, `lea`) | Possible as emitter patterns, which gets awkward as they grow | Natural |
| Checking the allocation | Checker over IR liveness plus the assignment and edge moves | Checker over MIR with physical registers |
| D-016 (no speculative abstractions) | Satisfied | Justified mainly by Phase 6+ needs: SSE register class, multi-word values |

**Assessment.** Measured S2 needs (low pressure, single-word values, one
register class) do not yet justify a machine IR. Reasons for one (SSE in
Phase 6, multi-word values, extensive selection) are later-phase, and
building for them now conflicts with D-016 and the AGENTS scope rule.

Reconsider when one of these happens:
- emitter patterns for selection multiply;
- a second register class arrives;
- the allocation checker cannot be written cleanly against the IR.

### 5.2 Allocation algorithms against Aether's requirements

Aether's requirements, in priority order:
1. verifiable correctness;
2. determinism;
3. simplicity;
4. compatibility with future stack maps (D-010);
5. code quality at low pressure;
6. compile speed, which is not a concern at these sizes (largest function:
   about 1,200 values).

| Approach | Fit |
|---|---|
| Local (per-block) | Too weak: hot loop values are φs, so they would stay in memory |
| Linear scan (Poletto–Sarkar; Wimmer–Franz on SSA) | Good quality at low pressure. Needs block linearization, intervals with holes, splitting, and resolution moves, which means more moving parts to verify. Its speed advantage is irrelevant here. |
| SSA-based coloring (Hack et al.): spill until MaxLive ≤ k, then greedy coloring in dominator-tree order | The SSA interference graph is chordal, so greedy coloring needs exactly MaxLive registers with no iteration. Reuses `ir.Dominators` and the liveness machinery. A value keeps **one location for its whole life**, which keeps the checker simple and later stack maps trivial. φ resolution reuses `edgeCopies`' parallel-copy algorithm. The cost of "spill everywhere" falls only on the roughly 1–5% of functions over k. |
| Chaitin–Briggs / iterated coalescing | Best quality under high pressure, which S2 does not have. Iterative build/simplify/spill, larger code, harder to verify. |

**Choice: SSA-based coloring.** Revisit live-range splitting or linear scan
only if measured spill cost matters (today: b04_matpow and some generated
functions).

## 6. Recommended next experiment (Proposed: D-026)

This section is a **proposal**. It is recorded as D-026 with status
Proposed. Nothing here is approved, and no allocator code exists.

**"RA-0: SSA-direct allocation with reserved scratch registers"** (test-only,
like D-006).
- **Allocatable registers:** 11: `%rbx`, `%rsi`, `%rdi`, `%r8`–`%r15`.
  `%rax`, `%rcx`, and `%rdx` stay reserved, so every existing instruction
  sequence (div, shifts, φ cycle breaking, the prologue check) is unchanged.
  Only operand locations change, from a cell to a register.
- **Calls:**
  - values live across any call, Aether or helper, stay in cells (ABI-S0
    unchanged);
  - tuples stay in cells;
  - functions whose MaxLive exceeds 11 after this get spill-everywhere by a
    simple cost heuristic.
- **Assignment:** greedy coloring in dominator-tree preorder.
- **φ copies:** become parallel copies between locations; φ/operand
  coalescing is a hint only.
- **Isolation:** no ABI change, no new IR contract, and no selection changes.
  The instruction count stays about the same while cell traffic disappears,
  so the experiment measures exactly the bottleneck of §3.1.
- **Allocation checker:** an independent check, a dataflow over the IR and
  the assignment, that:
  - no two simultaneously live values share a register;
  - every use reads its value's location;
  - no register-allocated value is live across a call;
  - every edge's copies implement the φs.
- **Measurement:** the checked-in harness (`BENCHMARKS.md`), against slot
  and SSA.

**Proposed acceptance criteria.**
1. All S2-suite programs (239) and the nine workloads give identical
   stderr and exit status for slot, SSA, RA-0, and the interpreter or gc
   oracle.
2. The checker passes on every function. The run is repeated in
   `FuzzGenerated`, natively.
3. Assembly is deterministic: byte-identical across two compilations.
4. Planted bugs are detected by a permanent test, each one:
   - two interfering values sharing a register;
   - a value in a register across a call;
   - a dropped edge copy;
   - a wrong cycle break;
   - the wrong edge's copies.
5. The result is reported whatever it is: dynamic instructions, memory ops,
   CPU-time median with IQR (15 runs, pinned), and static size and frames.
   Proposed pre-registered thresholds:
   - memory ops fall by at least 50% on b02, b03, b04, b07, and b09;
   - CPU time improves at least 10% over SSA on at least 5 of 9 workloads,
     with non-overlapping ranges.

   Missing these is a recorded negative result. It would redirect the work
   to selection and the ABI, not to more allocator sophistication.
6. Built executables stay those of the slot path until the owner decides
   (a decision entry, as for D-006/D-025).

**Model estimate (modeled), not a prediction of time.** If every non-call-crossing
value were in a register, with two-address and fixed-register copies,
call-crossing spills, and one move per φ copy counted, dynamic instructions
would be 0.49–0.72× SSA (0.33–0.60× slot). RA-0 deliberately keeps the
accumulator style, so its instruction count will be much closer to SSA's. The
remaining gap to that model is the case for later instruction-selection work.

## 7. Risks

- **Miscompilation**, the main risk. It is mitigated by the independent
  checker, the native differential suite, `FuzzGenerated` native, and
  negative controls. "Never generate code you are not sure is correct"
  argues for the single-location design.
- **Measurement validity:**
  - WSL2 adds noise. Spread was under 1.5% and sessions agreed within 3.3%,
    but this is not bare metal.
  - No hardware counters were available, so the latency explanation of §3.1
    is inferred from time vs. instruction ratios, not from counters.
  - Dynamic counts exclude the runtime helpers.
- **Workload representativeness:** these are synthetic S2 kernels with no
  memory or arrays. Gains may not carry over to S3+ code.
- **Harness brittleness:** the instrumentation parses `Emit`'s text output.
  It is now checked in with its own tests, which fail on an operand form it
  does not know, but a change to the emitter's output format can still
  require changes to the harness. If RA-0 changes the operand forms, the
  harness must be extended and re-tested before RA-0 is measured.
- **Scope drift:** the ABI (55% of fib) and selection (about 37% in
  compare/branch/const) are separate decisions. RA-0 must not be credited or
  blamed for them.
- **Spill-everywhere quality** on b04_matpow and large generated functions.
  If it dominates, splitting or linear scan get reconsidered with data.
- **D-010:** the single-location-per-value property and "nothing in
  registers across calls" make future stack maps simple. A later
  callee-saved ABI would break the latter, and has to be designed with stack
  maps in mind.

## 8. Data files

All in `data/` (measured, except `model.tsv`, which is modeled):

| File | Contents |
|---|---|
| `summary.tsv` | dynamic and static totals per workload and variant |
| `functions.tsv` | per-function dynamic instructions, memory, cell, and ABI ops |
| `ops.tsv` | dynamic instructions and cell moves by IR op |
| `pressure.tsv` | per-function pressure, weighted by frequency |
| `suite_pressure.tsv` | static pressure, S2 suite |
| `model.tsv` | the §6 model |
| `times_session1.tsv`, `times_session2.tsv` | raw timings: 15 runs and 7 runs per executable |
| `times_session1_summary.tsv`, `times_session2_summary.tsv` | their summaries: median, IQR, range, ratios |
| `times_session3.tsv`, `times_session3_summary.tsv`, `times_session3_environment.txt` | the reproduction with the checked-in harness, 2026-11-06 |
| `ssasuite.log` | `TestNativeSSASuite` at HEAD |
| `baseline-validation.txt` | gofmt, vet, and test at HEAD |

gc variants were built with `GO111MODULE=off go build` (and
`-gcflags=all='-N -l'`) from a copy of each workload as `main.go`
(§1, "Provenance").
