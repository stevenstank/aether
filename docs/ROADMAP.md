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

## Phase 1: Frontend driver, diagnostics, subset gate (next; awaiting authorization)

- **Objective:** `aether check prog.go` classifies any single-file program as
  `invalid`, `unsupported` (for S1), or acceptable.
- **Scope:**
  - `go.mod`: module `github.com/stevenstank/aether`, `go 1.26` (D-015)
  - frontend wrapper, with `go/parser` and `go/types` configured per D-001/D-002
  - diagnostic type and rendering
  - CLI exit codes (finalize the proposal in `ARCHITECTURE.md`)
  - subset gate for S1, including the pre-type-check import rejection
- **Exclusions:** IR, code generation.
- **Deliverables:** `cmd/aether` with `check`; `internal/diag`,
  `internal/frontend`, `internal/gate`.
- **Acceptance criteria:**
  - Every S1 construct is accepted.
  - Every construct in S1's "explicitly unsupported" list produces an
    `unsupported` diagnostic with the correct position and exit code 3.
  - Invalid programs produce exit code 1.
  - Output is deterministic.
- **Required tests:**
  - golden diagnostic tests: one file per unsupported feature, plus invalid
    cases S1-N01 to S1-N04 (`SEMANTICS.md`)
  - a test that verified cases S1-01 to S1-09 are accepted
  - a gate test sweep over `/usr/local/go/test/*.go` files marked `// run`.
    The sweep checks only that every program is classified without an
    `internal` error. Most will be `unsupported`.
  - a fuzz test: arbitrary input never causes an `internal` error or a panic
- **Depends on:** Phase 0.
- **Main risk:** misclassification at the gate/`go/types` boundary (for example,
  imports).
- **Completion evidence:** the test suite passes; the sweep reports zero
  `internal` errors; golden files are reviewed.

## Phase 2: IR and reference interpreter for S1

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
    S1-01 to S1-09 (`SEMANTICS.md`). These include the wraparound, constant,
    tuple-assignment, shadowing, and formatting edge cases.
  - a fuzz test of lowering + verifier on gate-accepted programs
- **Depends on:** Phase 1.
- **Risks:** IR design flaws discovered late; mitigated by keeping the
  instruction set minimal.
- **Completion evidence:** the differential suite passes; the support matrix is
  updated.

## Phase 3: First native executable (Milestone 1)

- **Objective:** `aether build` turns an S1 program into a static Linux x86-64
  executable whose program logic comes from Aether's backend. See
  [Milestone 1 acceptance test](#milestone-1-acceptance-test).
- **Scope:**
  - `internal/amd64`, with a naive slot-per-value backend
  - AT&T GNU assembly emission
  - the Phase 3 runtime `.s` (`_start`, print helpers)
  - `internal/toolchain` (`as`, `ld`), with the availability, identity, error,
    and reproducibility requirements of D-008
  - `-S` to keep the assembly, `-x` to print commands, `-work` to keep the
    work directory (D-007, D-008, D-009)
- **Exclusions:** register allocation, optimization, other functions, any
  run-time panic.
- **Deliverables:** `aether build -o out prog.go`; S1 rows at *Supported
  (native)*.
- **Acceptance criteria:** the Milestone 1 acceptance test passes. Three-way
  agreement (gc / interpreter / native) holds on the whole S1 suite.
- **Required tests:**
  - end-to-end tests
  - provenance tests
  - unsupported-construct tests through `build` (no output file is created)
  - determinism: building twice with identical inputs and configuration gives
    byte-identical `.s` and executable
  - tool-check tests: a missing or non-GNU `as`/`ld` gives a `tool`
    diagnostic, exit 5, and no output file
- **Depends on:** Phase 2.
- **Risks:**
  - stack alignment or syscall convention errors
  - an integer-printing edge case (`MinInt64`)
  - binutils version differences
- **Completion evidence:** the suite passes on a clean checkout, and every
  primary provenance check in the Milestone 1 acceptance test passes.

## Phase 4: Functions, control flow, integer types (S2)

- **Objective:** structured programs with functions run natively.
- **Scope:**
  - `bool` and comparisons, `&&`/`||`
  - `if`, `for` (three-clause, condition-only, infinite), `break`/`continue`,
    `switch` on integers
  - all sized integer types and conversions between them
  - `/ % << >> & | ^ &^`, with division-by-zero and negative-shift panics and
    the `MinInt / -1` rule
  - multiple top-level functions, parameters, multiple results, recursion
  - package-level `var` and `const` with `InitOrder`
  - `print`/`println` of `bool`
  - the minimal panic runtime path
- **Exclusions:** closures, methods, strings, the heap.
- **Prerequisite:** D-011 (calling convention) must be resolved.
- **Acceptance criteria:**
  - Differential agreement on the S2 suite.
  - Panicking programs match gc on the first stderr line and exit status 2
    (documented divergence).
- **Required tests:**
  - per-type wraparound and conversion tables generated against gc
  - control-flow tests
  - recursion depth test (within a documented stack limit)
- **Depends on:** Phase 3.
- **Risks:** calling-convention bugs; `IDIV` edge cases; flags handling.
- **Completion evidence:** the suite passes; `SEMANTICS.md` lists the panic
  divergence.

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

**This is not implemented. It is the specification for Phase 3.**

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

**Procedure:**

1. Build the reference with gc: `go build -o ref m1_arith.go`. Run `ref` and
   capture stdout, stderr, and exit status. gc is used here only as the
   oracle (D-014).
2. Start from a fresh test directory in which the output path does not exist.
   Run `aether build -x -S -work -o out m1_arith.go`. This keeps:
   - the generated assembly
   - the work directory containing the `.s` and `.o` files
   - the log of every external command run
3. Run `out`, capture stdout, stderr, and exit status, and apply the checks
   below.

**Primary provenance checks.** All six must pass. Together they establish
where the executable came from.

1. **Aether generated the assembly that contains the program's
   instructions.**
   - The test calls Aether's backend in-process on the same source and
     configuration, and asserts that its output is byte-identical to the
     program `.s` file that the `-x` log shows being passed to `as`.
   - That `.s` defines `main.main`, containing the program's instructions.
   - The only other assembly inputs are Aether's runtime files. They are
     byte-identical to the runtime sources in the Aether repository, and none
     of them defines `main.main`.
2. **GNU `as` assembled that output.**
   - The `-x` log shows one `as` invocation per assembly file: the program
     `.s` and each runtime `.s`. Each invocation names that file as input and
     a `.o` in the work directory as output.
   - The driver's recorded identity check for that `as` binary reports
     `GNU assembler` (D-008).
   - The program `.o` exists, and its `main.main` disassembly
     (`objdump -d`) matches the `main.main` body in the `.s` instruction for
     instruction, after normalizing operand syntax.
3. **GNU `ld` linked the resulting objects and runtime.**
   - The `-x` log shows exactly one `ld` invocation, identified as `GNU ld`.
   - Its inputs are exactly the program `.o` and the runtime `.o` files. There
     are no other objects, no `-l` libraries, and no C runtime start files.
   - Its output is the executable that ends up at `out`.
   - `out` defines `main.main` (from the program object) and `_start` (from
     the runtime object).
   - The `main.main` instructions in `out` match those in the program `.o`
     (relocated addresses may differ).
4. **The executable runs correctly.**
   - `out`'s stdout, stderr, and exit status equal both the gc reference and
     the Aether interpreter result for the same program.
   - The expected values recorded in `SEMANTICS.md` (case S1-03) are a
     cross-check only. The comparison against the live gc result is
     authoritative.
5. **The executable was not compiled by the official Go compiler.**
   - The `-x` log records every process Aether started. It contains only the
     `as` and `ld` invocations from checks 2 and 3. No program named `go`,
     `compile`, `asm`, `link`, `gcc`, or `cc` is executed.
   - A static test asserts that `internal/toolchain` is the only package that
     imports `os/exec`, so no process can start outside the logged path.
   - The build is repeated with `PATH` set to a directory containing only `as`
     and `ld`, and with `GOROOT` unset. It must still succeed and produce an
     identical executable.
6. **Unsupported constructs fail clearly and leave no misleading artifact.**
   See the rejection checks below.

**Supporting evidence.** This is recorded and checked, but it is not proof of
provenance on its own.
- `readelf -h out`: an ELF64 x86-64 executable, statically linked.
- `readelf -S out` shows none of `.go.buildinfo`, `.note.go.buildid`,
  `.gopclntab`. These are present in gc binaries (E1). Their absence is
  consistent with a non-gc build but does **not** prove one: a binary can be
  stripped or built by other means.
- `nm out` defines no `runtime.*` symbols. The defined symbols are only:
  - `main.main` and `_start`
  - the symbols in the runtime interface table (`ARCHITECTURE.md`)
  - linker-defined symbols (`__bss_start`, `_edata`, `_end`, observed in E2)
- `go version out` is **not** used: it exits 0 even for non-Go files (E1).

**Rejection checks** (primary check 6). For each case, the output path must
not exist before the build.
1. Variants using `/`, `import "fmt"`, `if`, a second function, and
   `println("hi")` each:
   - exit with code 3
   - print an `unsupported` diagnostic naming the construct and its
     `file:line:col` position
   - create no file at the output path
   - leave no `.s`, `.o`, or executable in the work directory, because the
     gate stops before lowering
2. Invalid cases S1-N01 to S1-N04 (`SEMANTICS.md`) exit with code 1
   (`invalid`), with the same no-artifact checks.
3. A build whose `ld` step fails (simulated with a failing `ld` stand-in on
   `PATH`) exits with code 5 (`tool`) and creates no file at the output path.
   The driver links to a temporary name in the work directory and renames it
   into place only after success, so a partial executable never appears at
   the output path.

**Determinism check.**
- Build twice with identical inputs and configuration:
  - the same Aether binary, source bytes, flags, environment, and `as`/`ld`
    paths and versions
  - each build in its own fresh work directory
- The test records the inputs and configuration of both builds and asserts
  they are equal, apart from the work-directory location.
- It then asserts byte-identical program `.s` files and byte-identical
  executables.
