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
    S1-01 to S1-10 (`SEMANTICS.md`). These include the wraparound, constant,
    tuple-assignment, shadowing, redeclaration, and formatting edge cases.
  - a reviewed IR golden file for the Milestone 1 program (used by check P1)
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

## Phase 4: Functions, control flow, integer types (S2)

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
  used in P3, and no other process.
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
