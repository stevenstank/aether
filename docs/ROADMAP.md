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

## Phase 0: Foundation and architecture ← current

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
- **Completion evidence:** owner review. Proposed decision D-012 (subset S1) is
  confirmed or amended.

## Phase 1: Frontend driver, diagnostics, subset gate

- **Objective:** `aether check prog.go` classifies any single-file program as
  `invalid`, `unsupported` (for S1), or acceptable.
- **Scope:**
  - `go.mod` (D-015)
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
  - golden diagnostic tests (one file per unsupported feature, plus invalid
    cases)
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
  - differential tests (interpreter vs. gc)
  - wraparound edge cases (`MaxInt64+1`, `MinInt64-1`, `-MinInt64`, products
    that overflow)
  - shadowing tests
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
  - `internal/toolchain` (`as`, `ld`)
  - `-S` to keep the assembly, `-x` to print commands (D-007, D-009)
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
  - determinism: building twice gives byte-identical `.s` and executable
- **Depends on:** Phase 2.
- **Risks:**
  - stack alignment or syscall convention errors
  - an integer-printing edge case (`MinInt64`)
  - binutils version differences
- **Completion evidence:** the suite passes on a clean checkout; the command
  log shows only `as`/`ld`; the binary contains no gc sections.

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
- **Completion evidence:** cross-check and benchmark reports committed. D-008
  and D-010 revisited with data.

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

1. Build with gc: `go build -o ref m1_arith.go`. Run `ref` and capture stdout,
   stderr, and exit status.
2. `aether build -x -S -o out m1_arith.go`.
3. Assert that `out` exists, is an ELF64 x86-64 executable (`readelf -h`), and
   is statically linked.
4. Run `out` and capture stdout, stderr, and exit status.
5. Assert the result equals both the gc result and the interpreter result for
   the same program.

**Provenance assertions** (the logic came from Aether):

1. The `-x` command log contains only `as` and `ld` invocations. No program
   named `go`, `compile`, `asm`, `link`, `gcc`, or `cc` is executed.
2. `readelf -S out` shows none of `.go.buildinfo`, `.note.go.buildid`,
   `.gopclntab`. These sections are present in gc binaries (E1).
   - `go version out` is **not** used as evidence: it exits 0 even for non-Go
     files (E1).
3. `nm out` defines `main.main` and `_start`, plus only:
   - symbols listed in the runtime interface table (`ARCHITECTURE.md`), and
   - linker-defined symbols (`__bss_start`, `_edata`, `_end`, observed in E2).

   There are no `runtime.*` symbols.
4. The `main.main` instructions in `objdump -d out` correspond, mnemonic for
   mnemonic, to the `main.main` body in Aether's emitted `.s`.

**Rejection assertions:**

1. A variant using `/` exits with code 3.
   - The diagnostic names the `/` position and the feature.
   - No output file is created.
2. Variants using `import "fmt"`, `if`, a second function, and
   `println("hi")` behave the same way.
3. A variant with an unused variable exits with code 1 (`invalid`).

**Determinism assertion:** two builds produce byte-identical `.s` files and
executables.
