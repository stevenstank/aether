# Aether Decision Register

This register records architectural decisions, why they were made, and what
would cause them to be revisited. Other documents refer to entries by ID
(`D-NNN`). If a document and this register disagree, this register wins and the
other document must be corrected.

**Statuses**

- **Proposed**: a recommendation with reasoning, but missing evidence or owner
  approval. Implementation must not depend on it without first resolving it.
- **Accepted**: adopted. Changing it requires a new entry that supersedes it.
- **Rejected**: considered and declined. Kept so it is not re-litigated without
  new evidence.
- **Superseded**: replaced by a later entry (named in the entry).

**Evidence conventions.** "Local source" means files in the installed Go
distribution at `/usr/local/go` (go1.26.1, linux/amd64), which is the
version-matched authoritative source for this environment. "Experiment" means a
throwaway check run during Phase 0 outside the repository. Its results are
summarized in [Phase 0 research notes](#phase-0-research-notes) at the end of
this file. Nothing here is supported by benchmarks: none have been run.

## Summary

| ID | Title | Status |
|----|-------|--------|
| D-001 | Reuse `go/parser`, `go/ast`, `go/token` for the initial frontend | Accepted |
| D-002 | Reuse `go/types` and `go/constant` for initial type checking | Accepted |
| D-003 | Explicit subset gate and three-way diagnostic classification | Accepted |
| D-004 | Aether-owned typed CFG IR, non-SSA at first | Accepted |
| D-005 | Reference interpreter for the IR | Accepted |
| D-006 | SSA timing and construction method | Proposed |
| D-007 | Initial executable path: GNU assembler text, `as`, `ld` | Accepted |
| D-008 | In-house instruction encoder, ELF writer, and linker | Proposed |
| D-009 | Aether-owned minimal runtime; no gc runtime, no libc | Accepted (early phases only) |
| D-010 | Memory management and garbage collection | Proposed |
| D-011 | Calling convention for Aether-compiled functions | Proposed |
| D-012 | Initial supported Go subset (S1) | Proposed |
| D-013 | Dependency policy | Accepted |
| D-014 | Testing strategy with the official toolchain as oracle | Accepted |
| D-015 | Go toolchain version and module creation | Accepted |
| D-016 | Package layout: create packages only when code needs them | Accepted |
| D-017 | Reuse `golang.org/x/tools/go/ssa` as Aether's IR | Rejected |

---

## D-001: Reuse `go/parser`, `go/ast`, `go/token` for the initial frontend

**Status:** Accepted

**Context.** Aether needs a correct Go parser with accurate source positions.
Writing one is well understood but large. It does not advance the parts of the
project that make Aether an independent compiler: IR, optimizer, and backend.

**Decision.** Parse with `go/parser` (mode `parser.SkipObjectResolution`, since
`ast.Object` is deprecated) into `go/ast`. Use `go/token.FileSet` for positions
inside the frontend.

**Alternatives considered.**
1. A hand-written lexer and parser from day one. This gives full ownership but
   costs significant time before any IR or codegen work, and adds a large
   surface for parser bugs that the official package does not have.
2. `cmd/compile/internal/syntax`. It is internal and cannot be imported.
   Copying it would conflict with the rule against copying the official compiler.

**Rationale.** Parsing Go is not the risky or educational core of this project.
`go/parser` accepts the full grammar of the installed Go version. Reusing it
means "accepted by the parser" says nothing about Aether's support, which keeps
the support boundary in one place (D-003).

**Consequences.**
- Aether does **not** own its lexer or parser. Documentation must say so.
- Syntax errors are worded by `go/parser`.
- `go/ast` is a tool-oriented tree. Aether must not leak `go/ast` types past
  lowering (D-004), so the frontend stays replaceable.

**Evidence.** `cmd/compile/README.md` (local source) states that the `go/*`
packages are "mostly unused by the compiler" and exist for tools, so reusing
them is not reusing gc's frontend. `go doc go/ast.Object` reports the deprecation.
Experiment E3 parsed and rejected a syntax error with a positioned message.

**Revisit when.** Parsing becomes a measured compile-latency bottleneck (see
`docs/BENCHMARKS.md`); a `go/parser` limitation blocks a needed feature; or the
owner decides that frontend ownership is a project goal in its own right.

---

## D-002: Reuse `go/types` and `go/constant` for initial type checking

**Status:** Accepted

**Context.** Go's type system includes untyped constants with arbitrary
precision, assignability, method sets, embedding, generics with inference, and
initialization order. Reimplementing it correctly takes years of work.

**Decision.** Type-check with `go/types.Config.Check` using
`GoVersion: "go1.26"` and `Sizes: types.SizesFor("gc", "amd64")`. Consume
`types.Info` (`Types`, `Defs`, `Uses`, `Implicits`, `Selections`, `Scopes`, and
later `Instances` and `InitOrder`). Constant values come from `go/constant`.

**Alternatives considered.**
1. An Aether type checker from the start. Full ownership, but it delays the
   backend and risks diverging from the specification in subtle ways.
2. No type checker, with lowering inferring types. Rejected: Go programs cannot
   be lowered correctly without full type information.

**Rationale.** `go/types` implements the same semantics as gc's checker
(`types2` is a port of `go/types`, per `cmd/compile/README.md`). Experiment E3
showed that it reports "declared and not used", out-of-range constant
conversion, and constant division by zero. These are compile-time rules Aether
would otherwise need to reimplement before it could reject invalid programs.

**Consequences.**
- Compile-time semantics, including constant folding of *constant expressions*,
  come from `go/types`. Aether must not claim to own them.
- What remains Aether's job: deciding layouts for codegen (using the same
  `Sizes` so `unsafe.Sizeof` agrees), lowering semantics, evaluation order of
  non-constant expressions, and all runtime behavior.
- Imports later need a `types.Importer` that matches whatever Aether uses to
  compile dependencies. That design is part of Phase 11 (`docs/ROADMAP.md`).

**Evidence.** `go doc go/types.Config`, `go doc go/types.Info`,
`go doc go/types.SizesFor` (local source); Experiment E3.

**Revisit when.** Type checking dominates compile latency, Aether needs
information `types.Info` does not expose, or the owner wants semantic analysis
ownership as a project goal.

---

## D-003: Explicit subset gate and three-way diagnostic classification

**Status:** Accepted

**Context.** Aether will support only a subset of Go for a long time. Valid
programs outside the subset must be rejected explicitly, never miscompiled.
Tests must be able to tell a missing feature apart from a compiler bug.

**Decision.** A dedicated *subset gate* runs between type checking and
lowering. It has two parts:
- A pre-type-check syntactic check for constructs that would make `go/types`
  fail for the wrong reason, such as `import` declarations when no importer
  exists.
- A typed check that walks the AST with `types.Info`.

Every diagnostic Aether emits falls into exactly one class:

| Class | Meaning | Source |
|---|---|---|
| `invalid` | The program is not valid Go | `go/parser`, `go/types` |
| `unsupported` | Valid Go outside Aether's declared subset | subset gate |
| `internal` | Aether malfunctioned (an ICE) | any later stage |

Lowering and later stages treat any construct the gate should have rejected as
an `internal` error, not a silent fallback.

**Alternatives considered.** Let lowering reject unknown nodes as it meets them.
This is simpler, but the support boundary ends up spread across the code and
cannot be checked against `docs/SEMANTICS.md`.

**Rationale.** It gives one authoritative implementation of the support matrix
in `docs/SEMANTICS.md`, and the classification lets tests distinguish
limitations from bugs (`docs/TESTING.md`).

**Consequences.** The gate and lowering can drift apart. Any mismatch shows up
as an `internal` error and is treated as a bug with a regression test.

**Evidence.** Design reasoning only. No implementation exists.

**Revisit when.** The subset becomes broad enough that the gate is mostly a
list of exceptions. It may then shrink to a deny-list.

---

## D-004: Aether-owned typed CFG IR, non-SSA at first

**Status:** Accepted for the overall shape. The concrete instruction set grows
with each supported subset and is defined during Phase 2.

**Context.** The IR is the main contract between frontend and backend, and the
place where Aether's ownership of compilation begins.

**Decision.** Aether defines its own IR:
- **Program → functions → basic blocks → instructions.** Each block ends in
  exactly one explicit terminator (`jump`, `branch`, `return`, later `panic` and
  `unreachable`).
- **Instructions produce typed values.** IR types are Aether's own, translated
  from `go/types` during lowering (`int64`, `int32`, …, `bool`, later pointers
  and aggregates). Go's `int` lowers to a 64-bit integer on amd64.
- **Locals start as explicit stack slots** (`alloc`/`load`/`store`) rather than
  SSA values. This is the simplest correct lowering of Go's mutable variables.
- **Positions:** every instruction carries an Aether-owned position (file
  index, line, column), not `token.Pos`, so the IR does not depend on `go/token`.
- **Evaluation order is explicit:** lowering serializes side effects into
  instruction order. Later stages may not reorder observable effects.
- **Textual form and verifier:** the IR has a deterministic text printer (for
  golden tests) and a verifier that checks structural and type invariants
  (`docs/ARCHITECTURE.md` §IR invariants).

**Alternatives considered.**
1. `golang.org/x/tools/go/ssa`. Rejected in D-017.
2. Generating code directly from the typed AST. This is the fastest route to a
   first executable, but it leaves no place for an interpreter, optimizations,
   or a second backend, so it would need rewriting.
3. SSA from the start. Deferred by D-006.

**Rationale.** A load/store CFG is simple to produce and to interpret, and easy
to check. Standard SSA construction can be layered on top of it without
changing the frontend.

**Consequences.** The first backend works on unoptimized, memory-heavy code.
That is acceptable because the first milestone measures correctness, not speed.

**Evidence.** The `go/ssa` doc (in module cache, x/tools v0.49.0) describes the
same strategy: a naive load/store form followed by a "lifting" pass. This shows
the approach is established practice. It does not prove anything about Aether.

**Revisit when.** Phase 2 shows the load/store form makes lowering of some
construct unreasonably complex.

---

## D-005: Reference interpreter for the IR

**Status:** Accepted

**Context.** When a natively compiled program misbehaves, the bug could be in
the frontend, lowering, optimization, or backend.

**Decision.** Build a small IR interpreter in Phase 2. It covers exactly the
supported subset and grows with it. It is a testing oracle, not a product
feature, and it has no performance goals.

**Alternatives considered.** Skip it and rely only on differential tests against
gc. That is cheaper at first, but every failure then needs manual bisection
across stages.

**Rationale.** Three-way comparison (gc output vs. interpreter vs. native) puts
each failure on one side of the IR boundary. The interpreter also serves as an
executable definition of IR semantics, which optimization passes are checked
against (`docs/TESTING.md`).

**Consequences.** It must be maintained alongside the IR. Runtime-heavy features
(goroutines, GC) may be interpreted with simplified models, and those must be
documented as such.

**Evidence.** Design reasoning only.

**Revisit when.** Maintenance cost exceeds its diagnostic value. Measure this by
whether failures are being localized with it.

---

## D-006: SSA timing and construction method

**Status:** Proposed

**Context.** SSA is needed for credible optimization and register allocation,
but it is not needed for the first executable.

**Proposal.** Introduce SSA in Phase 5, after control flow and functions work
natively (Phase 4). Build it from the load/store IR by promoting non-address-taken
local slots, in the same spirit as `go/ssa` "lifting". The SSA form is a stricter
invariant level of the same IR package, not a separate IR, unless Phase 5
experience shows the two need different data structures.

**Alternatives.** (a) SSA from Phase 2, which front-loads complexity before
anything runs. (b) Never use SSA and rely on dataflow over the non-SSA IR, which
makes register allocation and most optimizations harder.

**Missing evidence.**
- The construction algorithm (dominance-frontier based vs. direct construction
  during lowering) has not been chosen.
- It is unknown whether one IR package can hold both forms cleanly.
- It is unknown whether the backend should consume SSA directly or a separate
  machine IR.

**Revisit / resolve when.** Phase 4 is complete. Resolve with a design note
added to this entry.

---

## D-007: Initial executable path: GNU assembler text, `as`, `ld`

**Status:** Accepted for Phases 3 through 5. Longer-term handling is D-008.

**Context.** The first milestone must produce a native Linux x86-64 executable
whose program logic comes from Aether's backend.

**Decision.** The amd64 backend emits GNU assembler source in AT&T syntax. The
driver runs `as --64` on it and on Aether's runtime assembly (D-009), then links
with `ld -static -nostdlib`, entry symbol `_start`. No libc, no C compiler, no
Go toolchain in the build path.

**Ownership boundary.** Aether chooses every instruction, register, stack slot,
symbol, and data layout. `as` only encodes instructions Aether has fully
specified and writes an ELF relocatable object. `ld` only resolves symbols
and lays out the static executable. Both are replaceable (D-008).

**Alternatives considered.**
1. Emit ELF directly now, which needs an instruction encoder and ELF writer
   before the first executable (cost in D-008).
2. Emit C and use gcc. Rejected: gcc would do instruction selection and register
   allocation, so the backend would not be Aether's.
3. Emit Go assembler and use `go tool asm`/`go tool link`. Rejected: it brings
   the gc toolchain into the build path and ties Aether to gc's object format
   and runtime expectations.

**Rationale.** It is the smallest route to an executable. It keeps generated code
human-readable for debugging, and both tools were verified locally (E2).

**Consequences.**
- Builds depend on GNU binutils on the host. Verified: GNU as 2.42 and GNU ld
  2.42 on Ubuntu 24.04. Missing tools must produce a clear error.
- The Phase 3 backend may assign every IR value a stack slot (no register
  allocation). That is correct but slow, and it is replaced in Phase 5.
- AT&T is GAS's native syntax. Intel syntax with `.intel_syntax noprefix`
  also assembled in E2. AT&T was chosen to avoid GAS's Intel-mode ambiguity
  between symbol names and register or keyword names.

**Evidence.** Experiment E2.

**Revisit when.** Assembly text measurably dominates compile time; D-008 is
accepted; or a non-GNU host must be supported.

---

## D-008: In-house instruction encoder, ELF writer, and linker

**Status:** Proposed

**Context.** Owning machine-code emission end to end is a long-term project
goal. It is not required for early milestones.

**Proposal.** After Phase 5, evaluate replacing `as` with an Aether x86-64
encoder that writes ELF relocatable objects, and possibly replacing `ld` with a
static linker for Aether-only programs.

**Costs identified.**
- The encoder must handle x86-64 encoding (REX prefixes, ModRM/SIB, displacement
  sizes, immediates) for every instruction the backend emits, plus branch
  relaxation.
- The ELF writer needs section headers, symbol tables, string tables, and
  relocations (`elf(5)`; full gABI and psABI relocation types).
- Debug info (DWARF) is a further large step.
- In return, compilation becomes hermetic and binutils is no longer required,
  and encoding becomes testable by comparing against `as` output byte for byte.

**Missing evidence.** No measurement of how much time `as`/`ld` take. No
decision on whether hermetic builds matter to the owner.

**Resolve when.** Phase 5 is complete and `docs/BENCHMARKS.md` compile-latency
data exists.

---

## D-009: Aether-owned minimal runtime; no gc runtime, no libc

**Status:** Accepted for Phases 3 and 4. Each later runtime feature is decided
in its own phase (D-010 and others).

**Context.** Generating machine code is not the same as implementing Go. Even
`println`, process start, and exit need runtime support. Go's runtime
(`/usr/local/go/src/runtime`) assumes gc's ABI (for example R14 holding the
current goroutine and X15 being zero on amd64, per `abi-internal.md`), gc
linker-generated metadata (gc binaries carry `.gopclntab`, verified in E1), and
the full G/M/P scheduler model (`runtime/HACKING.md`).

**Decision.** Aether ships its own runtime, starting tiny:
- `_start`: establish 16-byte stack alignment, call `main.main`, then
  `exit_group(0)`.
- Printing support for `println` of integers (signed decimal, space-separated,
  trailing newline, to file descriptor 2, matching gc; see E1), using the raw
  `write` syscall.
- For Phase 4, a minimal panic path: print the message, `exit_group(2)`.

In Phases 3 and 4 the runtime is hand-written GNU assembly kept in the
repository. Once Aether can compile a large enough subset, runtime routines
should move to Go source compiled by Aether itself. Linux syscalls follow the
kernel convention in the psABI (`kernel.tex`: number in `rax`, arguments in
`rdi, rsi, rdx, r10, r8, r9`, `rcx`/`r11` clobbered).

**Alternatives considered.**
1. Link against gc's runtime. This needs gc ABI compatibility and gc metadata
   formats, and would make Aether a gc-compatible backend rather than an
   independent compiler. It is not ruled out forever, but rejected for now.
2. Runtime in C linked with libc. This adds a C compiler and libc to the
   trusted path and hides syscalls and startup behind libc.

**Rationale.** It is the smallest runtime that can support the first milestone.
The boundary between compiler and runtime stays visible and owned.

**Consequences.** Every runtime feature is Aether's work. Panic messages,
tracebacks, and formatting will differ from gc's until implemented. Each
difference is listed in `docs/SEMANTICS.md`.

**Evidence.** `abi-internal.md`, `runtime/HACKING.md` (local source);
psABI `kernel.tex` (see research notes); experiments E1 and E2.

**Revisit when.** Phase 9 or 10, when GC and the scheduler are designed, and
reusing or adapting gc's runtime must be re-evaluated explicitly.

---

## D-010: Memory management and garbage collection

**Status:** Proposed (open)

**Context.** Heap allocation enters with pointers, slices, and closures
(Phases 7 and 8). Go requires automatic memory management, but no program in
the early subsets needs the heap.

**Options under consideration.**
1. A non-collecting bump allocator as an interim step. This is simple and
   correct for short programs, but memory grows without bound. It is
   acceptable only if documented as non-conforming.
2. A conservative mark-sweep collector. It needs no stack maps, but it can
   retain garbage and cannot move objects.
3. A precise collector. This requires pointer maps for stack frames and globals
   and safe points, which places requirements on the backend and register
   allocator.

**Recommendation.** Use option 1 in Phase 7. Choose between 2 and 3 before
Phase 9. Even if option 3 is deferred, the backend design in Phase 5 should
leave room for stack maps.

**Missing evidence.** The cost of retrofitting stack maps onto the Phase 5
register allocator. The owner's tolerance for a non-conforming interim
allocator.

**Resolve when.** Before Phase 7 begins.

---

## D-011: Calling convention for Aether-compiled functions

**Status:** Proposed

**Context.** Phase 3 only needs `_start → main.main` (no arguments). Phase 4
adds functions with parameters and multiple results.

**Options.**
1. **System V AMD64** (integer arguments in `rdi, rsi, rdx, rcx, r8, r9`;
   `rbx, rbp, r12–r15` callee-saved; stack 16-byte aligned before `call`; per
   the psABI). Well documented and interoperable with C. Only two integer
   return registers, so Go multi-results need memory.
2. **Go ABIInternal** (`RAX, RBX, RCX, RDI, RSI, R8, R9, R10, R11` for integer
   arguments and results; R14 for the goroutine; X15 zero; per
   `abi-internal.md`). It matters only if gc interop is ever wanted, and its
   fixed registers presuppose gc's runtime.
3. **An Aether-internal convention**, initially all stack-based (simplest),
   later register-based.

**Recommendation.** Option 3, stack-based in Phase 4, with a single documented
place in the amd64 backend that defines it. A register-based convention would
follow in Phase 5 with the register allocator. SysV would be used only at
explicit foreign boundaries, if any are added. The prologue must leave room for
a future stack-growth check (Phase 10).

**Missing evidence.** Whether goroutine stack strategy (segmented, copying, or
fixed-large) constrains the convention. Not yet investigated.

**Resolve when.** At the start of Phase 4.

---

## D-012: Initial supported Go subset (S1)

**Status:** Proposed, pending owner confirmation of scope

**Proposal.** Subset S1, defined precisely in `docs/SEMANTICS.md` §Subset S1:
- A single file, `package main`, no imports, and only `func main()`.
- Local `var`/`:=`/`const` declarations of type `int`, assignment, `+= -= *=`,
  and `++`/`--`.
- Binary `+ - *`, unary `+ -`, parentheses, and nested blocks.
- `println` with `int` arguments.
- Everything else is rejected as `unsupported`.

**Rationale.**
- The program is observable without imports: `println` is a predeclared builtin
  defined in the spec's Bootstrapping section, and gc writes its output to
  stderr (E1). This allows byte-exact differential testing against gc.
- `/` and `%` are excluded because they can panic at run time (division by zero)
  and have a special overflow case (`MinInt / -1 == MinInt` per the spec), which
  `IDIV` traps on. Both need runtime panic support, which arrives in Phase 4.
- Shifts are excluded because a negative shift count panics at run time.
- Wraparound overflow (`+ - *`) is included deliberately to test that Aether
  never treats signed overflow as undefined.

**Alternatives.** Observing results through the process exit status instead of
`println`. That avoids runtime output code, but Go cannot set an exit status
without `os.Exit` (an import), so a valid Go program could not express it.

**Risk.** The spec says `print`/`println` "are not guaranteed to stay in the
language". If they are removed, Phase 3 tests would need `os` support. This is
acceptable for now.

**Resolve when.** The owner confirms the subset (open question in the Phase 0
report).

---

## D-013: Dependency policy

**Status:** Accepted

**Decision.**
- **Compiler code:** Go standard library only. Adding any module dependency
  requires a decision entry stating its purpose, alternatives, and long-term
  consequences.
- **External tools in the build path:** only GNU `as` and `ld` (D-007). The
  official Go toolchain must never be invoked to compile the input program.
- **External tools in tests:** the official Go toolchain (as a semantic oracle),
  plus `readelf`, `nm`, and `objdump` for inspection. These are host tools, not
  module dependencies.
- Candidates noted but **not adopted**: `golang.org/x/tools/go/ssa` (D-017),
  `golang.org/x/tools/go/packages` (only relevant for multi-package loading,
  Phase 11), `golang.org/x/perf/cmd/benchstat` (benchmark statistics, decided
  when benchmarking starts).

**Rationale.** Reusing standard-library packages is still reuse, and each one
is documented as such (D-001, D-002). Third-party modules would spread that
reuse without review.

**Revisit when.** A concrete need is blocked by the policy.

---

## D-014: Testing strategy with the official toolchain as oracle

**Status:** Accepted

**Decision.**
- The official Go toolchain, at a recorded version, is the semantic oracle for
  differential tests.
- The reference program is built with `go build` and then executed, never
  with `go run`. E1 showed `go run` reports exit status 1 when the program
  exited with 2.
- Comparison covers stdout, stderr, and exit status, with documented
  normalization only where Aether's runtime is known to differ.
- Full details are in `docs/TESTING.md`.

**Rationale.** gc is the de facto reference implementation. The spec is the
normative reference (`docs/SEMANTICS.md`), and gc is how spec interpretations
are checked mechanically.

**Consequences.** Behavior the spec leaves unspecified (for example some
evaluation orders) must not be differentially tested without marking it as such.

**Revisit when.** gc and the spec are found to disagree. The spec wins, and the
test is marked.

---

## D-015: Go toolchain version and module creation

**Status:** Accepted

**Decision.**
- Aether is implemented with, and targets the language version of, **Go 1.26**.
  This environment has go1.26.1 installed, and its spec reads "Language version
  go1.26 (Jan 12, 2026)".
- `go.mod` is **not** created in Phase 0, because there is no Go code. It is
  created with the first real code in Phase 1, with `go 1.26` and module path
  `github.com/stevenstank/aether`, derived from the `origin` remote.
- No `toolchain` line unless a reason appears.

**Rationale.** A `go.mod` without code would only make the repository look
initialized, which the project rules forbid.

**Revisit when.** A Go release changes semantics Aether depends on, or the
repository moves.

---

## D-016: Package layout: create packages only when code needs them

**Status:** Accepted

**Decision.** `docs/ARCHITECTURE.md` lists *candidate* package boundaries. A
package is created only when it gets real code and tests. Everything is under
`internal/` except the command, so nothing becomes a public API by accident.
Architecture-specific code lives under a per-target directory from the start.
Shared abstractions (for example a target-description interface) are extracted
only when a second user exists.

**Rationale.** It avoids both a monolith and premature framework design.

**Revisit when.** A boundary proves wrong during implementation. Update
`docs/ARCHITECTURE.md` in the same change.

---

## D-017: Reuse `golang.org/x/tools/go/ssa` as Aether's IR

**Status:** Rejected

**Context.** `go/ssa` already lowers typed Go to an SSA form with basic blocks.

**Reasons for rejection.**
- Its own documentation states that the form "is intentionally close to the
  source language to facilitate construction of source analysis tools. It is
  not intended for machine code generation."
- It is a third-party module dependency (D-013).
- Adopting it would hand Aether's middle end to another project, which defeats
  a central goal.

**Possible future use.** It could serve as a test-only cross-check of lowering
semantics. That would need its own decision entry.

**Evidence.** `golang.org/x/tools@v0.49.0/go/ssa/doc.go` (module cache).

---

## Phase 0 research notes

All experiments ran in a scratch directory outside the repository on
2026-10-24, on Ubuntu 24.04 (WSL2, kernel 6.6.87), go1.26.1 linux/amd64,
GNU as/ld 2.42.

- **E1: gc observable behavior.** Program `x := 6; y := 7; println(x*y - 1 + 1)`
  built with gc printed `42` to **stderr**; stdout was empty; exit 0.
  `int64` wraparound: `MinInt64 - 1` printed `9223372036854775807`, and
  `-MinInt64` printed `-9223372036854775808`. Runtime integer division by zero
  printed `panic: runtime error: integer divide by zero` plus a goroutine
  traceback; the built binary exited with **2**, but `go run` reported exit
  **1**. gc binaries contain `.go.buildinfo`, `.note.go.buildid`, and
  `.gopclntab` sections. The gc binary for the `println` program was 1,821,436
  bytes. `go version <file>` printed "not a Go executable" for a non-gc binary
  but **still exited 0**, so its exit status cannot be used as a provenance check.
  `print(1, 2); print(3)` wrote `123` with no separators and no newline, so its
  format differs from `println`'s. The S1 milestone program in
  `docs/ROADMAP.md` was built and run with gc, and its expected output there is
  the observed output.
- **E2: binutils path.** Hand-written assembly using `_start` and the raw
  `exit`/`exit_group` syscalls, with no libc, assembled with `as --64` and
  linked with `ld -static -nostdlib`. The result was a static ELF64 executable
  (4,624 bytes, entry `0x401000`) with no Go sections. Both Intel-syntax and
  AT&T-syntax inputs worked. Dotted symbol names such as `main.main` are accepted.
  Two rebuilds from identical inputs were byte-identical (one trial only, not a
  reproducibility guarantee).
- **E3: `go/types` diagnostics.** With `GoVersion "go1.26"` and gc/amd64 sizes:
  - `x := 1` → `declared and not used: x`
  - `var x int8 = 200` → `... (overflows)`
  - `x / 0` → `invalid operation: division by zero`
  - `x := }` → a parser error with position
  - a valid program → no error.

**Sources consulted:**
- Local Go 1.26.1: `doc/go_spec.html`, `doc/go_mem.html` ("Version of June 6,
  2022"), `src/cmd/compile/README.md`, `src/cmd/compile/abi-internal.md`,
  `src/runtime/HACKING.md`, `test/README.md`, and `go doc` for `go/types`,
  `go/parser`, `go/ast`, `go/constant`.
- `golang.org/x/tools@v0.49.0/go/ssa/doc.go` (module cache).
- The System V AMD64 psABI LaTeX source from
  `gitlab.com/x86-psABIs/x86-64-ABI`, master at commit `e1ce0983`
  (2025-03-12), files `low-level-sys-info.tex` and `kernel.tex`. The rendered
  PDF was not consulted.
- `man 5 elf`, `man 2 syscall`, `/usr/include/x86_64-linux-gnu/asm/unistd_64.h`.

**Not consulted:** the ELF gABI specification text (only `elf(5)` was checked);
the Intel SDM (encoding details are deferred to D-008); gc runtime GC and
stack-growth source in detail (deferred to D-010 and D-011).
