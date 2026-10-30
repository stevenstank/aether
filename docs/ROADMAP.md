# Aether Roadmap

Phases are defined by **observable results**, not dates or activity. A phase is
complete only when its completion evidence exists and can be reproduced from
the repository. Decisions are referenced by ID from
[`DECISIONS.md`](DECISIONS.md). Subsets (S1, S2, …) are defined in
[`SEMANTICS.md`](SEMANTICS.md).

Changes from the sequence in the Phase 0 brief:
- **Parsing and semantic analysis are merged into Phase 1.** Both are reused
  (D-001, D-002), so the real work is the driver, diagnostics, and subset gate.
- **SSA and optimization (Phase 5) come before composite types.** Register
  allocation and stack-map design should exist before heap and GC work, which
  depend on them (D-010).
- **Garbage collection is its own phase (9).** It is separated from heap
  allocation (7) and from concurrency (10).

Every phase from Phase 1 onward includes these requirements, which are not
repeated in each phase below:
- `go vet ./...` and `go test ./...` pass.
- New behavior has tests.
- Documentation is updated in the same change (`AGENTS.md`).

---

## Phase 0: Foundation and architecture (complete; owner review 2026-10-24)

- **Objective:** a researched, internally consistent design.
- **Scope:** documentation and decisions only.
- **Exclusions:** any Go code, `go.mod` (D-015).
- **Deliverables:**
  - `AGENTS.md`, `README.md`
  - `docs/VISION.md`, `ARCHITECTURE.md`, `SEMANTICS.md`, `ROADMAP.md`,
    `DECISIONS.md`, `TESTING.md`, `BENCHMARKS.md`
- **Acceptance criteria:**
  - The documents agree with each other.
  - Proposed decisions are not described as accepted anywhere.
  - The first milestone does not depend on gc.
- **Tests:** none (no code).
- **Risks:** the design is unvalidated until Phases 2 and 3.
- **Completion evidence:** owner review, done 2026-10-24. The review:
  - accepted D-012 (S1) with clarifications
  - confirmed the module path (D-015)
  - accepted GNU `as`/`ld` as initial and potentially long-term dependencies
    (D-008)
  - restructured the Milestone 1 provenance checks

  All of this is recorded in the `DECISIONS.md` review log.

## Phase 1: Frontend driver, diagnostics, subset gate (complete 2026-10-24, pending owner review)

- **Objective:** `aether check prog.go` classifies any single-file program as
  `invalid`, `unsupported` (for S1), or acceptable.
- **Scope:**
  - `go.mod`: module `github.com/stevenstank/aether`, `go 1.26` (D-015)
  - frontend wrapper, with `go/parser` and `go/types` configured per D-001/D-002
  - diagnostic type and rendering
  - CLI exit codes (finalize the proposal in `ARCHITECTURE.md`)
  - subset gate for S1, including the pre-type-check import rejection
  - Aether's program-level check (`package main` must declare `func main`)
  - the classification precedence of D-003
- **Exclusions:** IR, code generation.
- **Deliverables:** `cmd/aether` with `check`; `internal/diag`,
  `internal/frontend`, `internal/gate`.
- **Acceptance criteria:**
  - Every S1 construct is accepted.
  - Every construct in S1's "explicitly unsupported" list produces an
    `unsupported` diagnostic with the correct position and exit code 3. This
    includes the valid-Go forms `go/types` accepts (`SEMANTICS.md` §Subset S1,
    unsupported cases).
  - Invalid programs produce exit code 1, including S1-N05, which `go/types`
    alone does not detect.
  - Output is deterministic.
- **Required tests:**
  - golden diagnostic tests: one file per unsupported feature, plus invalid
    cases S1-N01 to S1-N05 (`SEMANTICS.md`)
  - a test that verified cases S1-01 to S1-10 are accepted
  - precedence tests: a program with both an import and a type error is
    `unsupported`; a program with both an unsupported type and a type error
    is `invalid`
  - dependency tests: no Aether package imports `go/importer` or `go/build`.
    `os/exec` may enter the dependency graph only through
    `internal/toolchain`. That package does not exist yet in Phase 1, so for
    now the test asserts that no Aether package depends on `os/exec`, directly
    or transitively.
  - a gate test sweep over `/usr/local/go/test/*.go` files marked `// run`.
    The sweep checks only that every program is classified without an
    `internal` error. Most will be `unsupported`.
  - a fuzz test: arbitrary input never causes an `internal` error or a panic
- **Depends on:** Phase 0.
- **Main risk:** misclassification at the gate/`go/types` boundary (for example,
  imports).
- **Completion evidence:** the test suite passes; the sweep reports zero
  `internal` errors; golden files are reviewed.
- **Evidence recorded 2026-10-24** (go1.26.1, linux/amd64, WSL2):
  - `go vet ./...` is clean, and `gofmt -l` reports nothing for non-testdata
    sources.
  - `go test ./... -count=1` passes: 30 top-level tests and 393 subtests, with
    none failing or skipped. `go test -short ./...` and `-count=3` also pass.
  - **Corpus:** 96 files in `cmd/aether/testdata/check/`, each with a golden
    file:
    - S1-01 to S1-10 and two further accepted-form files
    - S1-N01 to S1-N05 and eight further invalid cases
    - five precedence cases
    - one or more files per unsupported feature category

    All goldens were reviewed by the implementing agent. Owner review is
    pending.
  - **gc cross-check:** every corpus file except the cgo case was built with
    gc, and gc's verdict matched each file's declared validity.
  - **GOROOT sweep:** of the 141 `// run` programs, 0 were accepted, 0
    invalid, and 141 unsupported, with 0 `internal`. The sweep first found 3
    programs with `internal` results, which were fixed (E12).
  - **Fuzzing (`FuzzCheck`):** fuzzing first found one input with an
    `internal` result, which was fixed and kept as a regression seed (E12).
    After the fix, runs of 120 s, 180 s, and 420 s (about 3.3, 17.7, and 33.4
    million executions) passed. One other 180 s run failed without reporting
    a failing input. This is fuzz incident FI-1 (`TESTING.md`): still open and
    unexplained, and assessed as not blocking.
  - **Dependency rules:** checked on the `go list -deps` graph, with a control
    test, and confirmed by temporarily adding a forbidden import (the test
    failed as intended).
- **Closure audit (2026-10-24):**
  - `go test ./... -count=1` passes: 33 top-level tests and 403 subtests, with
    none failing or skipped.
  - The corpus is now 99 files: 12 run, 17 invalid, 70 unsupported.
  - **Added:**
    - D-019 and D-020, recording the two implemented classifications, with an
      `init` variant of each in the corpus
    - `TestFeatureTableMatchesGate`, which compares the gate's feature
      registry with the `SEMANTICS.md` table; negative controls confirmed it
      detects both a missing and an extra name
    - correct syntax-error positions in files with `//line` directives, which
      previously could name a nonexistent file (D-018)
  - FI-1 was investigated: 24 short runs (19.3 M executions) targeting the
    end-of-run shutdown phase passed, without reproducing the failure.
- **Known limitations of the Phase 1 guarantees:**
  - **The S1 membership guarantee rests on the gate's default-deny structure
    and its tests.** There are per-construct tests, the corpus, the gc
    cross-check, the GOROOT sweep, and fuzzing. But no independent oracle
    re-derives S1 membership. Phase 2's interpreter differential tests add
    one for accepted programs.
  - **Classification can be wrong where `go/types` and gc disagree.** Valid Go
    that `go/types` wrongly rejects is reported as `invalid`, as happened with
    `func main() ()` before D-019. That error is in the safe direction:
    nothing is compiled.
  - **FI-1 remains unexplained** (above).
  - **The stage ordering lives in `cmd/aether/check.go`.** Phase 2's
    in-process checks may need it moved into a package.

## Phase 2: IR and reference interpreter for S1 (complete 2026-10-24, pending owner review)

- **Objective:** S1 programs lower to verified IR, and the interpreter's output
  matches gc's.
- **Scope:**
  - `internal/ir` (types, builder, verifier, printer)
  - `internal/lower` for S1
  - `internal/interp`
  - `aether run-ir` (or a test-only equivalent) that interprets a program
- **Exclusions:** native code, SSA, optimization.
- **Deliverables:**
  - the IR text format, documented in `ARCHITECTURE.md` or a new
    `docs/IR.md`
  - S1 rows in the `SEMANTICS.md` support matrix at state *Supported (interp)*
- **Acceptance criteria:**
  - For every S1 test program, the interpreter's stdout, stderr, and exit
    status equal those of the gc-built binary.
  - IR golden output is byte-stable across runs.
  - The verifier rejects deliberately malformed IR in unit tests.
- **Required tests:**
  - IR golden tests
  - verifier negative tests
  - differential tests (interpreter vs. gc), covering at least verified cases
    S1-01 to S1-10 (`SEMANTICS.md`). These include the wraparound, constant,
    tuple-assignment, shadowing, redeclaration, and formatting edge cases.
  - a reviewed IR golden file for the Milestone 1 program (used by check P1)
  - a fuzz test of lowering + verifier on gate-accepted programs
- **Depends on:** Phase 1.
- **Risks:** IR design flaws discovered late; mitigated by keeping the
  instruction set minimal.
- **Completion evidence:** the differential suite passes; the support matrix is
  updated.
- **Evidence recorded 2026-10-24** (go1.26.1, linux/amd64, WSL2):
  - **Delivered:** `internal/ir`, `internal/lower`, and `internal/interp`.
    The IR contract and text format are in `ARCHITECTURE.md` §4, and the
    instruction set is D-021. The test-only equivalent of `run-ir` is
    `interpret` in `cmd/aether/interp_test.go`. No CLI command was added.
  - `go vet ./...` is clean, and `gofmt -l` reports nothing for non-testdata
    sources. `go test ./... -count=1` passes: 48 top-level tests and 550
    subtests, with none failing or skipped.
  - **Differential suite (`TestInterpAgainstGc`, renamed
    `TestDifferentialAgainstGc` in Phase 3):** for the 12 corpus programs
    expected to run (including S1-01 to S1-10) and 200 generated S1 programs,
    the interpreter's stdout, stderr, and exit status equal those of the
    gc-built binary. 51 generated programs with an overflowing constant
    expression were skipped as `invalid`, as designed. Three injected bugs
    (one in the interpreter, two in lowering) were each detected.
  - **IR goldens:** a reviewed `.ir` file for each of the 12 programs,
    including the Milestone 1 program (`s1_03_milestone1.ir`, for check P1).
    `TestLowerCorpus` also checks that lowering is deterministic, and that
    invalid and unsupported programs are not lowered.
  - **Verifier:** 29 negative tests, each breaking one invariant.
  - **Fuzzing:** `FuzzCheck` (which now lowers, verifies, and interprets every
    accepted input) and `FuzzGenerated` each ran 60 s and passed, with about
    3.45 million and 1.03 million executions.
  - **`aether check`:** it now also lowers and verifies every accepted
    program. Its output on every corpus file is unchanged.
- **Known limitations:**
  - The generator produces small programs (one function, a few nested
    blocks), and its coverage was judged by inspection, not measured.
  - The stage ordering stays in `cmd/aether/check.go` (`compile`). Phase 2's
    in-process tests are in the same package, so moving it was not needed.
  - FI-1 (`TESTING.md`) remains open and unexplained.

## Phase 3: First native executable (Milestone 1) (complete; accepted by the owner)

- **Objective:** `aether build` turns an S1 program into a static Linux x86-64
  executable whose program logic comes from Aether's backend. See
  [Milestone 1 acceptance test](#milestone-1-acceptance-test).
- **Scope:**
  - `internal/amd64`, with a naive slot-per-value backend
  - AT&T GNU assembly emission
  - the Phase 3 runtime in `internal/amd64/rt/*.s` (`_start`, print helpers)
  - the runtime-helper calling interface (`ARCHITECTURE.md` §Runtime
    integration boundary)
  - `internal/toolchain` (`as`, `ld`), with the availability, identity, error,
    environment, output-handling, and reproducibility requirements of D-008
    and `ARCHITECTURE.md` §8
  - `-x` to print commands, `-work` to keep the work directory (D-007, D-008,
    D-009)
- **Exclusions:** register allocation, optimization, other functions, any
  run-time panic.
- **Deliverables:** `aether build -o out prog.go`; S1 rows at *Supported
  (native)*.
- **Acceptance criteria:** the Milestone 1 acceptance test passes. Three-way
  agreement (gc / interpreter / native) holds on the whole S1 suite.
- **Required tests:**
  - end-to-end tests
  - provenance tests P1–P3 (Milestone 1)
  - unsupported, invalid, and pre-existing-output tests through `build` (P5)
  - determinism under the conditions of P6
  - tool-check tests: a missing or non-GNU `as`/`ld` gives a `tool`
    diagnostic, exit 5, and no output file
- **Depends on:** Phase 2.
- **Risks:**
  - stack alignment or syscall convention errors
  - an integer-printing edge case (`MinInt64`)
  - binutils version differences
- **Completion evidence:** the suite passes on a clean checkout, and primary
  checks P1–P6 of the Milestone 1 acceptance test pass.
- **Evidence recorded 2026-10-24** (go1.26.1, GNU as and ld 2.42,
  linux/amd64, WSL2):
  - **Delivered:** `internal/amd64` (backend, runtime in `rt/*.s`),
    `internal/toolchain`, and `aether build [-x] [-work] -o OUT FILE.go`
    (D-022). `aether check` is unchanged.
  - `go vet ./...` is clean, and `gofmt -l` reports nothing for non-testdata
    sources. `go test ./... -count=1` passes on a clean clone of the
    repository: 67 top-level tests and 598 subtests, with none failing or
    skipped. `go test -short ./...` also passes.
  - **Milestone 1:** `TestMilestone1`, `TestMilestone1Rejections`, and
    `TestMilestone1Determinism` pass, covering P1–P6 and the supporting
    evidence (`TESTING.md` §Provenance tests).
  - **Three-way agreement:** for the 12 corpus programs expected to run
    (S1-01 to S1-10 and two more) and 200 generated programs, the interpreter
    and the native executable match gc's stdout, stderr, and exit status.
    The 51 generated programs Aether classified `invalid` were all rejected by
    gc for a constant overflow.
  - **Backend and runtime:** every IR operation, run natively at the integer
    boundaries (0, ±1, 2, 7, −10, the 32-bit limits, 3037000500,
    `MaxInt64`, `MinInt64`), matches the interpreter. An ABI harness checks
    that the helpers preserve `%rbx`, `%rbp`, `%r12`–`%r15`, and `%rsp`, print
    the extreme values correctly, and ignore a write error.
  - **Tool and output handling:** missing, non-GNU, wrong-target, failing,
    and silent-failing tools each give exit 5 and leave the output path
    unchanged.
  - **Negative controls**, each detected: a runtime helper clobbering
    `%r12`; a driver assembling text other than the backend's output; an
    extra `ld` option; a backend multiply truncated to 32 bits.
- **Fix-up F1, 2026-10-25** (post-Phase-3 audit): a valid program with a
  frame over the 8 MiB process stack built but crashed with SIGSEGV, while gc
  ran it. `main.main` now runs on a stack the runtime maps to fit its frame
  (D-022). `TestBuildLargeFrame` and `TestRuntimeStack` cover this under
  reduced stack limits. Two negative controls each made both tests fail.
- **Closure hardening, 2026-10-25:** tests for the stack layout and guard
  page, refused output paths (symlinks, FIFO, hard link), cleanup after tool
  failures, and failures of the work directory and output directory
  (`TESTING.md`). Each was checked with a negative control. The limits of the
  output guarantees (races, signals, no `fsync`) are documented in
  `ARCHITECTURE.md` §8.
- **Known limitations:**
  - Native code is unoptimized: every value goes through memory.
  - If the stack mapping is refused, the program exits with status 2 and a
    fatal error (`SEMANTICS.md` V-1). The 1 GiB maximum frame was run on this
    host only, where the mapping was granted.
  - Native correctness is shown on the corpus, generated programs, and
    boundary tables, which are finite. `FuzzGenerated` exercises the
    interpreter only, because running `as` and `ld` per input is too slow.
  - The `-x` log includes the two identification commands (`as --version`,
    `ld -V`) besides the build commands.
  - Reproducibility is shown for one host and one binutils version only.
  - FI-1 (`TESTING.md`) remains open and unexplained.

## Phase 4: Functions, control flow, integer types (S2) (design gate passed 2026-10-26; in progress)

- **Objective:** structured programs with functions run natively.
- **Scope:**
  - `bool` and comparisons, `&&`/`||`
  - `if`, `for` (three-clause, condition-only, infinite), `break`/`continue`,
    `switch` on integers
  - all sized integer types and conversions between them
  - `/ % << >> & | ^ &^`, with:
    - division-by-zero and negative-shift panics
    - the non-panicking `MinInt / -1 == MinInt` and `MinInt % -1 == 0` path
      (`IDIV` faults here, E10)
    - correct results for shift counts at or above the width (x86 masks the
      count, E10)
  - multiple top-level functions, parameters, multiple results, recursion
  - package-level `var` and `const` with `InitOrder`
  - `println` of `bool` (4.1), and `print` of integers and `bool` (4.4)
  - the minimal panic runtime path
- **Exclusions:** closures, methods, strings, the heap.
- **Prerequisites** (design gate, 2026-10-26: D-009 Phase 4 amendment, D-011,
  and D-023 accepted):
  - **4.1** needs D-023 (its 4.1 subset). It does not depend on D-011.
  - **4.2 and later** need D-011 (ABI-S0) and the D-009 Phase 4 stack
    amendment. No calling code is written before them.
- **Increments**, in dependency order, each kept under about 800 changed
  lines (D-023 lists the IR parts of each):
  1. **4.1: control flow and `bool`.** `bool` locals and constants,
     comparisons of `int` and `bool`, `!`, `&&`, `||`, `if`/`else`, the three
     `for` forms, unlabeled `break` and `continue`, `println` of `int` and
     `bool`. Runtime: `aether.printbool`. Still unsupported: functions,
     `switch`, labels, `goto`, `for range`, sized types, `/ % << >>`,
     `print`.
     **Status (2026-10-29):** part 1 done: `bool`, the comparisons, `not`,
     `jump`, and `branch` in the IR, the verifier (invariants 1–5, with
     definite assignment as a greatest fixed point), the printer, the
     interpreter (with the step budget, `interp.RunSteps`), the backend,
     and `aether.printbool`, all tested on hand-built IR. Negative controls
     run by hand and reverted: swapped branch targets, `setb` for signed
     `<`, `printbool` printing `1`, and definite assignment accepting a
     store on any path each fail a test. Part 2 done: the gate and
     lowering (`SEMANTICS.md` §Control flow and bool), corpus programs run
     against gc, the interpreter's step budget and a 2-minute timeout for
     every gc-built and Aether-built program in the differential and build
     tests, and 4.2's last two tests: recursion 50,000 calls deep and
     short-circuit side effects through calls (`s2_c02_bool`,
     `s2_c03_recursion`). Part 3 done: the generator (`TESTING.md`) and
     the last two controls: `||` lowered as `&&` fails the gc comparison,
     and a `continue` that skips the post statement is stopped by the step
     budget.
  2. **4.2: functions.** `int` parameters, one `int` result, recursion,
     ABI-S0 with the prologue bound check, and the runtime stack of at least
     256 MiB (`aether.stacklimit`, `aether.stackoverflow`). The interpreter
     gains an exit status and a call-depth limit that reports a stack
     overflow with exit 2 (D-023). The interpreter's frame stack is a hard
     prerequisite for any IR call, and 4.2 is not accepted until the depth
     limit's value is chosen, recorded in D-023, and tested. Short-circuit
     side effects first become observable here and are tested here.
     **Status (2026-10-29):** started before 4.1, because calls do not
     depend on control flow. Done: the IR, verifier, and printer parts; the
     interpreter's frame stack, exit status, and depth limit (value proposed
     in D-023, pending owner approval); ABI-S0 in the backend; the 256 MiB
     stack, `aether.stacklimit`, and `aether.stackoverflow`; the gate and
     lowering of functions (`SEMANTICS.md` §Functions); the gc overflow
     comparison. Without 4.1 every recursion is unbounded, so tests of
     bounded recursion wait for 4.1.
  3. **4.3: multiple results** (`tuple`, `extract`).
     **Status (2026-10-29):** done in one part: the IR, verifier, printer,
     interpreter, backend (tuple cells filled right after the call), gate,
     and lowering, with `s2_t01_multiple_results` run against gc. Negative
     control run by hand and reverted: an `extract` that reads the
     outgoing area instead of the tuple's cells fails `TestNativeTuples`,
     because a second call overwrites the area first.
  4. **4.4: sized integer types**, conversions, `& | ^ &^`, unary `^`, and
     `print` (no separators, no newline) of every integer type and `bool`.
     Observed in gc: `int` (E1), `bool`, `uint8`, `int8` (E13), and the
     other types in 4.4 (`s2_i02_conversions_print`, D-023).
     **Status (2026-10-29):** part 1 done: the eight integer types in
     canonical form, `and or xor andnot com conv print` in the IR, verifier
     (canonical constants), printer, interpreter, and backend (a
     canonicalizing move after each narrow operation, unsigned condition
     codes), and `aether.printuint`. Tested on hand-built IR against Go's
     own typed arithmetic: every operation on every type's edge values,
     and every conversion. Negative controls run by hand and reverted:
     no canonicalization, signed condition codes for unsigned types,
     `printint` for unsigned values, and a signed comparison in the
     interpreter each fail a test. Part 2 done: the gate and lowering
     (`SEMANTICS.md` §Integer types), and per-type tables of every
     operator and conversion, and `print` of every type, run against gc
     (`s2_i01_integer_tables`, `s2_i02_conversions_print`). Part 3 done:
     the generator (`TESTING.md`), with conversions lowered as no-ops and
     `&^` lowered as `&` as negative controls.
  5. **4.5: `/ % << >>`** with the panic path.
     **Status (2026-10-30):** part 1 done: `div`, `rem`, `shl`, `shr`, and
     `panic K` in the IR, verifier, printer, interpreter, and backend (a
     divisor of -1 handled without `idivq`, shift counts compared with the
     width), and `aether.panicdivide` and `aether.panicshift`. Tested on
     hand-built IR against Go's own arithmetic for every type, and the
     panics against the messages gc prints (observed 2026-10-30:
     `panic: runtime error: integer divide by zero` and `... negative shift
     amount`, exit 2). Negative controls run by hand and reverted:
     `idivq` for every divisor (SIGFPE), no width check on shifts, zero
     instead of the sign fill, and an interpreter dividing unsigned values
     as signed each fail a test. Part 2 done: the gate and lowering with the
     explicit checks (a divisor compared with 0, a signed count with 0,
     each branching to `panic`), the corrected panic comparison rule
     (acceptance criteria above), and division tables for every type and
     five panicking programs run against gc. Not done: the generator.
  6. **4.6: package-level `var` and `const`** with `InitOrder`; `_start`
     calls `main.init` before `main.main` (D-009, D-023).
  7. **4.7: `switch` on integers**, the S2 suite, Phase 4 acceptance.
- **Acceptance criteria:**
  - Differential agreement on the S2 suite.
  - Panicking programs (divergence V-3; corrected in 4.5, since gc writes
    `1panic: …` after `print(1)`):
    For a program that panics with a run-time error, let Q be the exact
    bytes of the gc-built binary's stderr up to and including the first
    newline after the first occurrence of the text `panic: `; the line from
    that occurrence must be `panic: runtime error: ` and a message. The text is
    found anywhere, not at a line start, because a program that last used
    `print` leaves no newline before it. The stderr of the Aether-built
    executable and of the interpreter must each be exactly Q, with nothing
    after it, and the gc-built binary, the Aether-built executable, and the
    interpreter must each write nothing to stdout and exit with status 2.
    gc's traceback after Q is not compared.
  - Stack overflow (planned divergence V-2):
    The rule applies to test programs whose recursion never ends, so that the
    gc-built binary, the Aether-built executable, and the interpreter all
    overflow. The program prints nothing during its recursion. Let D be the
    text `runtime: goroutine stack exceeds`, which begins gc's stack-overflow
    diagnostic. Let P be the exact byte prefix of the gc-built binary's stderr
    before the first occurrence of D. P holds everything the program printed
    before the recursion and is compared exactly; it need not end in a newline,
    because `print` writes none. The gc-built binary's stderr must contain D.
    Its bytes from that occurrence on must begin with the stack-limit line: D,
    one space, one or more ASCII digits, the text `-byte limit`, and a newline.
    After the stack-limit line, the first newline-terminated line that begins
    with `fatal error: ` must exist and must be exactly
    `fatal error: stack overflow`. Beyond P and these checks, gc's stderr is
    not compared: the `runtime: sp=` line, the traceback, and any other lines
    are ignored. The stderr of the Aether-built executable and the stderr of the
    interpreter must each be exactly P followed by `fatal error: stack overflow`
    and one newline, with nothing after it. The gc-built binary, the
    Aether-built executable, and the interpreter must each write nothing to
    stdout and exit with status 2. If any of these checks fails, the test fails.
    A program that overflows in Aether but not under gc shows the V-2 depth
    difference and is not compared with gc by this rule.
- **Required tests.** Run so far (2026-10-29), for 4.2: the overflow
  comparison with gc (`TestStackOverflowAgainstGc`, four programs, serial,
  2-minute timeout, skipped only under `-short`; about 17 s here); on
  hand-built IR, the boundary test, the reserve test for the helpers at `L`,
  unbounded recursion end to end, the interpreter depth limit with its
  low-Go-stack subprocess, argument offsets for 0 to 4 parameters, the
  `NewFrame` bound, and the updated `TestRuntimeStackGuard`; the mapping
  test (`TestStackMapping`), the overflow handler entered at `L` − 8 with
  stderr open and closed (`TestOverflowHandlerReserve`), a frame larger than
  S (`TestFrameLargerThanStack`, its size enlarged in the assembly), and
  `ulimit -v` at 128 MiB and 1 GiB (`TestRuntimeStack`). Negative controls
  run by hand and reverted: without the `jb` or without storing `L`, the
  overflow, boundary, and large-frame tests die with a signal; with flags
  `0x24022`, the mapping test fails on `nr`; with a changed overflow
  message, the gc comparison fails natively and in the interpreter. Of
  4.2, only the tests that need bounded recursion (4.1) are not yet run:
  depth within the limit and short-circuit side effects. Not yet run for
  the other increments: everything else below.
  - **All increments:**
    - every gc-built and Aether-built program in the differential and build
      tests runs **with a timeout** (done in 4.1: `runExe`, 2 minutes; the
      interpreter runs with a step budget), so a bug that loops fails the
      suite instead of hanging it;
    - the Milestone 1 test's exact `nm` symbol set is updated whenever the
      runtime gains a symbol: `aether.printbool` in 4.1 (the whole runtime is
      linked into every program); `aether.stacklimit` and
      `aether.stackoverflow` in 4.2;
    - `FuzzCheck` and `FuzzGenerated` after gate, lowering, IR, or
      interpreter changes, with the complete output saved (FI-1);
    - Milestone 1 P1–P6 and the reproducibility checks, which are rerun
      because the runtime and the generated assembly change.
  - **4.1:**
    - corpus `.go`, `.golden`, and `.ir` files for each new construct, and
      `unsupported` cases for labeled `break`, `goto`, `switch`, `for range`,
      and functions;
    - a verifier negative test for each new invariant, including a slot
      stored on only one path before a load, and a slot stored only inside a
      loop body and loaded after the loop;
    - a verifier positive test of a loop whose header reads a slot stored
      before the loop (the fixed-point rule);
    - interpreter tests on hand-built IR, and the step budget;
    - the generator extended to bounded loops, `if` trees, and comparisons
      including negative operands; gc, interpreter, and native agree on
      corpus and 200 or more generated programs;
    - negative controls, each applied, run, and reverted: swapped branch
      targets; unsigned `setb` for signed `<`; a definite-assignment check
      that accepts a store on any path; `||` lowered as `&&`; `printbool`
      printing `1`; and a `continue` that skips the post statement, which
      must fail by timeout rather than hang.
  - **4.2:**
    - **mapping:** a harness `main.main` writes its `%rsp` and `L`, then
      blocks reading stdin; the test reads `/proc/<pid>/smaps` and checks that
      the guard page is `---p`, the stack is `rw-p` with no `nr` in
      `VmFlags`, and its size is `total`, by containment, because the kernel
      can merge adjacent mappings. Under overcommit mode 2 the `nr` check
      proves nothing (the kernel ignores `MAP_NORESERVE` there), so the test
      logs the mode. Negative control: flags `0x24022` must fail it;
    - **boundary:** a function entered at E = T (D-011) runs; at E = T − 16
      it exits 2 with exactly `fatal error: stack overflow` and no signal;
    - **reserve:** with `%rsp` = `L`, `aether.printint(MinInt64)`,
      `aether.printsp`, and `aether.printnl` print correctly; the overflow
      handler entered at `L` − 8 exits 2, also with stderr closed;
    - **end to end:** unbounded small-frame recursion, and one non-`main`
      frame larger than S, each with exact stderr and exit 2;
    - **depth within the limit:** recursion using no more than S/4 bytes of
      stack matches gc exactly;
    - **interpreter depth limit:** unbounded recursion in the interpreter
      writes `fatal error: stack overflow` after any earlier output and
      returns exit status 2; a recursion within the limit returns exit
      status 0, both at the limit value recorded in D-023. Run it again in
      a subprocess with `debug.SetMaxStack` set far below what Go recursion
      to the limit would need: it must still reach the limit cleanly. This
      subprocess test is supporting evidence for the frame-stack design
      (D-023) and does not by itself prove that Go stack exhaustion is
      impossible;
    - **overflow comparison** (required): a few programs that print before
      an unbounded recursion are compared with gc by the V-2 rule, natively
      and in the interpreter.
      - **Cost:** each gc binary took about 1 GB of memory and 4 s to
        overflow (E14), and each Aether executable can touch up to S bytes
        of stack, at least 256 MiB.
      - **Serial:** these binaries run one at a time, and the test does not
        call `t.Parallel`. That makes only this test serial: under
        `go test ./...`, tests of other packages can run at the same time
        and add to the memory pressure.
      - **Timeout:** each run has a 2-minute timeout, as
        `TestBuildLargeFrame` uses; that is far above the measured 4 s, so
        a run that hits it is treated as a bug.
      - **Never weakened to a skip:** like the other gc-building tests, it
        is skipped only under `-short`. In the full suite, a timeout, a
        signal, or an out-of-memory kill of any of the three runs fails the
        test, and no automatic skip based on free memory or other host
        resources is added.
      - **Memory:** about 1.5 GB of free memory is an estimate, not a
        guarantee: one gc binary at about 1 GB, from a single observed run
        (E14), plus one Aether stack. It does not include the other
        packages' tests, and a host with less free memory may fail the
        test;
    - **argument offsets** for several parameter and result counts, not only
      P = 1;
    - **negative controls:** `_start` never stores `L`, or the `jb` is
      removed; the boundary and end-to-end tests must then fail with
      SIGSEGV;
    - `NewFrame` keeps Size, outgoing area included, at 2³⁰ or less;
    - `TestRuntimeStackGuard` (whose usable size assumes today's formula) and
      the `ulimit -v` test of V-1 are updated to the 256 MiB policy.
  - **4.3–4.7:** per-type wraparound and conversion tables generated against
    gc; `print` output of every integer type and `bool` against gc, including
    mixed `print` and `println` calls; `MinInt / −1` and `MinInt % −1` for
    every signed type; shift counts at and above the width; division-by-zero and negative-shift panics with
    output printed before them; `InitOrder` against gc; `switch` cases.
  - **Not tested:** out-of-memory termination while the stack is in use. It
    cannot be tested safely without cgroups or root, and is a documented
    limitation (D-009).
- **Depends on:** Phase 3.
- **Risks:** calling-convention bugs; `IDIV` edge cases; flags handling.
- **Completion evidence:** the suite passes; `SEMANTICS.md` lists the panic
  and stack-overflow divergences and the D-009 stack limitations.

## Phase 5: SSA, register allocation, first optimizations

- **Objective:** better generated code with **no behavior change**.
- **Scope:**
  - SSA construction (resolve D-006)
  - register allocation
  - register-based internal convention (D-011 follow-up)
  - constant folding, copy propagation, dead code elimination
  - benchmark harness (`BENCHMARKS.md`)
  - optimization flags (`-O0`/`-O1` or similar)
- **Exclusions:** inlining, escape analysis, loop optimizations, unless
  measurements justify them.
- **Acceptance criteria:**
  - All earlier suites pass at every optimization level.
  - The verifier runs after every pass in tests.
  - `-O0` vs `-O1` cross-check agreement.
  - Benchmark report showing measured effect versus the Phase 4 baseline,
    whatever the result.
- **Required tests:**
  - per-pass unit tests on IR golden files
  - optimization cross-checks
  - IR-transform fuzzing
  - register-allocator stress tests
- **Depends on:** Phase 4.
- **Risks:** subtle allocator bugs; passes that assume no overflow.
- **Completion evidence:** cross-check and benchmark reports committed. D-010
  revisited with data. Compile-latency data checked against D-008's revisit
  triggers.

## Phase 6: Composite value types without the heap (S3)

- **Scope:**
  - arrays, structs (value semantics, copying, comparison)
  - `float64`/`float32` (SSE)
  - string constants (static data, `len`, indexing with bounds-check panics)
  - printing of strings and floats
- **Exclusions:** `&`, pointers, slices, string concatenation.
- **Acceptance criteria:** differential agreement, including the gc float
  print format.
- **Required tests:** layout tests (`unsafe.Sizeof`/`Offsetof` agree with
  `types.Sizes`), copy-semantics tests, NaN/±0/Inf tests.
- **Depends on:** Phase 5.
- **Risks:** ABI handling of aggregates; float formatting fidelity.

## Phase 7: Heap, pointers, slices (S4)

- **Scope:**
  - `&`, pointers, `new`
  - heap allocation of address-taken variables
  - slices, `make`, `append`, `copy`
  - string concatenation and conversion
  - nil checks
  - the interim allocator per D-010 (resolved before this phase)
- **Exclusions:** garbage collection, maps, interfaces.
- **Acceptance criteria:**
  - Differential agreement.
  - Memory growth is documented if the allocator does not collect.
- **Required tests:** aliasing tests, `append` growth-semantics tests (only on
  behavior the spec defines), bounds and nil panic tests.
- **Depends on:** Phase 6, D-010.
- **Risks:** escape semantics; relying on gc's unspecified `append` capacity.

## Phase 8: Methods, interfaces, closures, maps, defer/panic/recover, generics (S5)

- **Scope:** each item gets its own design note before implementation:
  - methods and method values
  - interfaces and type assertions/switches
  - closures
  - maps (including the randomized-iteration-order policy)
  - `defer`, `panic(v)`, `recover`
  - generics (monomorphization vs. dictionaries: undecided)
- **Acceptance criteria:** differential agreement; panic-value printing
  matched or documented as a divergence.
- **Depends on:** Phase 7.
- **Risks:** this is the largest scope jump. Split it into sub-phases if any
  item proves large.

## Phase 9: Garbage collection

- **Scope:** the collector chosen in D-010, with root scanning (stack maps or
  conservative scanning) and write barriers if required.
- **Acceptance criteria:** long-running allocation workloads stay within a
  documented memory bound; all earlier suites pass.
- **Required tests:** GC stress tests, a finalizer-free subset first,
  randomized allocation fuzzing.
- **Depends on:** Phases 5 and 7.
- **Risks:** missing roots, which are silent memory corruption.

## Phase 10: Concurrency

- **Scope:**
  - goroutines, stack strategy, scheduler (user-level first, threads later)
  - channels, `select`
  - `sync/atomic` semantics per the memory model
  - the optimizer constraints from `SEMANTICS.md`
- **Acceptance criteria:** differential agreement on deterministic concurrent
  programs; a deadlock-detection message or a documented divergence.
- **Depends on:** Phase 9, D-011.
- **Risks:** nondeterminism makes testing hard; requires a seeded scheduler for
  reproducibility.

## Phase 11: Packages, standard library, conformance

- **Scope:**
  - multi-file and multi-package compilation
  - an importer design consistent with D-002
  - compiling standard-library packages from source, starting with ones that
    have no assembly or `linkname` dependencies
  - reflection as required
  - running `/usr/local/go/test` `// run` programs (the README states these
    are "applicable to gccgo and other Go tool chains")
- **Acceptance criteria:** a published, reproducible pass rate on the GOROOT
  test corpus, with every failure classified (`unsupported` vs. bug).
- **Depends on:** all earlier phases.
- **Risks:**
  - the standard library depends on gc runtime internals
  - full compatibility may require ABI or runtime decisions that revisit D-009

---

## Milestone 1 acceptance test

**Implemented in Phase 3** by `cmd/aether/m1_test.go`. The program is the
corpus file `cmd/aether/testdata/check/s1_03_milestone1.go`, copied by the test
to `m1_arith.go`, so its IR golden file is the reviewed
`s1_03_milestone1.ir`.

**Program** `testdata/e2e/m1_arith.go` (illustrative content):

```go
package main

func main() {
	a := 6
	b := 7
	var c int = a*b - 2
	c += 2
	{
		a := -a
		println(a, c)
	}
	big := 9223372036854775807
	big++
	println(a, big)
}
```

**Expected:** stdout empty; stderr equal to the following. This was verified in
Phase 0 by building this exact program with go1.26.1 and running it.

```
-6 42
6 -9223372036854775808
```

Exit status 0. This must be confirmed against the gc-built binary as part of
the test itself, not hard-coded alone.

**What the test must establish.** Each item names the check that establishes
it:

| # | Requirement | Check |
|---|---|---|
| 1 | The source is inside S1 and is accepted | P1 |
| 2 | Aether parses and type-checks it | P1 |
| 3 | Aether lowers it to its own IR | P1 |
| 4 | Aether's backend selects the instructions and emits them as assembly | P2 |
| 5 | GNU `as` assembles that output | P3 |
| 6 | GNU `ld` links the resulting objects and the runtime | P3 |
| 7 | The executable runs and produces the expected behavior | P4 |
| 8 | Invalid and unsupported programs fail clearly | P5 |
| 9 | Failed builds leave no misleading artifacts | P5 |
| 10 | The build is reproducible under documented conditions | P6 |
| 11 | The official Go compiler does not generate the executable | P3 (with P2) |

**Procedure:**

1. Build the reference with gc: `go build -o ref m1_arith.go`. Run `ref` and
   capture stdout, stderr, and exit status. gc is used here only as the
   oracle (D-014). It never touches `out`.
2. In a fresh test directory where the output path does not exist, run
   `aether check m1_arith.go` (exit 0), then
   `aether build -x -work -o out m1_arith.go` (exit 0). This keeps the work
   directory (`ARCHITECTURE.md` §8) and the command log.
3. Run `out` and capture stdout, stderr, and exit status. Apply checks P1–P6.

**Primary checks** (all required):

- **P1: Accepted, checked, and lowered by Aether.**
  - `aether check` accepts the program.
  - In-process, the frontend and gate accept it.
  - Lowering produces IR that passes the verifier and matches the reviewed IR
    golden file for this program.
  - The interpreter, run on that IR, reproduces the gc reference output.
- **P2: Aether emitted the program's instructions.**
  - In-process, the amd64 backend applied to the P1 IR produces assembly
    byte-identical to `prog.s` in the kept work directory.
  - `prog.s` defines `main.main`.
  - The runtime `.s` files in the work directory are byte-identical to
    `internal/amd64/rt/*.s` in the repository, and none defines `main.main`.
  - A dependency test confirms that `internal/amd64` imports no `go/*`
    package, so the backend works from IR alone.
- **P3: Independent reconstruction with GNU `as` and `ld`.**
  - In a separate fresh directory, the **test harness itself**, not Aether,
    reconstructs the executable:
    - It resolves `as` and `ld` to absolute paths.
    - It checks their version lines start with `GNU assembler` and `GNU ld`.
    - It runs them with the documented command lines and fixed environment
      (`ARCHITECTURE.md` §8).
    - Its inputs are the P2 in-process backend output and the repository
      runtime sources.
  - The reconstructed executable must be **byte-identical** to `out`.
  - The reconstruction uses only Aether-generated assembly, Aether's runtime
    sources, and GNU `as`/`ld`, with no Go toolchain involved. Byte identity
    therefore establishes that `out` is exactly what those inputs produce
    (requirements 5, 6, 11), without relying on Aether's own command log.
- **P4: The executable behaves correctly.**
  - `out`'s stdout, stderr, and exit status equal both the gc reference and the
    P1 interpreter result.
  - The values recorded in `SEMANTICS.md` (case S1-03) are a cross-check only.
    The live gc result is authoritative.
- **P5: Failures are clear and leave nothing misleading.** See the rejection
  checks below.
- **P6: Reproducible under documented conditions.** See the determinism check
  below.

**Supporting evidence.** Recorded and checked, but not sufficient on its own:
- The `-x` log lists exactly one `as` invocation per assembly file and one
  `ld` invocation, with the same tools, arguments, and environment the harness
  used in P3. The only other processes are the two identification commands,
  `as --version` and `ld -V`, which come first.
- The dependency test (`ARCHITECTURE.md` §8) shows that `os/exec` enters the
  dependency graph only through `internal/toolchain`, and that `go/importer` and
  `go/build` are not imported.
- **Restricted `PATH` rebuild.**
  - Set `PATH` to a fresh directory containing only symlinks to the resolved
    `as` and `ld` (both real binaries; E9).
  - Invoke Aether by absolute path, with the rest of the environment
    unchanged.
  - The build must succeed and produce an identical `out`.
  - If it fails, treat it as a driver bug to investigate. It is not, by
    itself, evidence for or against provenance.
- `readelf -h out` shows an ELF64 x86-64 executable, statically linked.
- `readelf -S out` shows none of `.go.buildinfo`, `.note.go.buildid`,
  `.gopclntab`. These sections are present in gc binaries (E1). Their absence
  is consistent with a non-gc build but does **not** prove one.
- `nm out` defines no `runtime.*` symbols. The defined symbols are only:
  - `main.main` and `_start`
  - the runtime-interface symbols (`ARCHITECTURE.md`)
  - linker-defined symbols (`__bss_start`, `_edata`, `_end`; E2)
- `objdump -d` shows the `main.main` instructions in `prog.o` and in `out`
  corresponding to `prog.s` (relocated addresses may differ).
- `go version out` is **not** used: it exits 0 even for non-Go files (E1).

**Rejection checks (P5).** Unless stated otherwise, the output path does not
exist before the build.
1. Each of these variants exits with code 3 (`unsupported`):
   - `/`
   - `import "fmt"`
   - `if`
   - a second function
   - `println("hi")`
   - `var x int = 1.0`
   - `package foo`

   Each must:
   - print an `unsupported` diagnostic naming the construct and its
     `file:line:col` position
   - create no file at the output path
   - produce no `.s`, `.o`, or executable in the work directory, because the
     gate stops before lowering
2. Invalid cases S1-N01 to S1-N05 (`SEMANTICS.md`) exit with code 1
   (`invalid`), with the same no-artifact checks.
3. A build whose `ld` step fails, simulated by a failing `ld` stand-in found
   first on `PATH`, exits with code 5 (`tool`) and creates no file at the
   output path.
4. **Pre-existing output.** The output path already holds a file with known
   content, and the build fails (any of the cases above). The file must be
   byte-for-byte unchanged afterwards: the driver neither deletes it nor
   partially overwrites it (`ARCHITECTURE.md` §8).

**Determinism check (P6).** Two builds are compared. The conditions are:

| Category | Requirement |
|---|---|
| Controlled: identical in both builds, recorded and asserted equal by the test | the Aether binary; the source bytes; command-line flags; resolved `as`/`ld` paths and version lines |
| Fixed by the driver | the tool environment (`LC_ALL=C` only); relative file names in the work directory |
| Deliberately varied by the test | the work-directory location; the test's current directory; the output path; one unrelated variable added to the invoking environment |

The program `.s` files and the executables must both be byte-identical.

**Not claimed:** reproducibility across different binutils versions, different
Aether builds, or different hosts.
