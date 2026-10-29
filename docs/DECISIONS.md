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
throwaway check run outside the repository: E1–E11 during Phase 0, and later
experiments in the phase their note names (E12 in Phase 1, E13 and E14 at
the Phase 4 design gate). Their results are summarized in
[Phase 0 research notes](#phase-0-research-notes) at the end of this file,
which also holds the later experiments. Nothing here is supported by
benchmarks: none have been run.

## Summary

| ID | Title | Status |
|----|-------|--------|
| D-001 | Reuse `go/parser`, `go/ast`, `go/token` for the initial frontend | Accepted |
| D-002 | Reuse `go/types` and `go/constant` for initial type checking | Accepted |
| D-003 | Explicit subset gate and diagnostic classification | Accepted |
| D-004 | Aether-owned typed CFG IR, non-SSA at first | Accepted |
| D-005 | Reference interpreter for the IR | Accepted |
| D-006 | SSA timing and construction method | Proposed |
| D-007 | Executable path: GNU assembler text, `as`, `ld` | Accepted |
| D-008 | GNU `as`/`ld` as the long-term encoding and linking path; no in-house encoder, ELF writer, or linker without a concrete requirement | Accepted |
| D-009 | Aether-owned minimal runtime; no gc runtime, no libc | Accepted (early phases only; Phase 4 stack amended 2026-10-26) |
| D-010 | Memory management and garbage collection | Proposed |
| D-011 | Calling convention for Aether-compiled functions | Accepted (ABI-S0, 2026-10-26) |
| D-012 | Initial supported Go subset (S1) | Accepted |
| D-013 | Dependency policy | Accepted |
| D-014 | Testing strategy with the official toolchain as oracle | Accepted |
| D-015 | Go toolchain version and module creation | Accepted |
| D-016 | Package layout: create packages only when code needs them | Accepted |
| D-017 | Reuse `golang.org/x/tools/go/ssa` as Aether's IR | Rejected |
| D-018 | CLI exit codes and diagnostic output | Accepted |
| D-019 | `func main() ()` is `unsupported`, not `invalid` | Accepted |
| D-020 | `func main()` without a body is `unsupported` | Accepted |
| D-021 | The S1 IR instruction set and its contract | Accepted |
| D-022 | The Phase 3 backend, runtime interface, and `aether build` | Accepted |
| D-023 | The S2 IR extension, introduced in stages | Accepted (staged, 2026-10-26) |

## Review log

| Date | Event | Decisions affected |
|---|---|---|
| 2026-10-24 | Phase 0 register created | D-001 – D-017 |
| 2026-10-24 | Owner Phase 0 review: S1 accepted with clarifications; module path confirmed; GNU `as`/`ld` accepted as initial and potentially long-term dependencies; Milestone 1 provenance checks restructured | D-007, D-008, D-012, D-015 |
| 2026-10-24 | Pre-Phase 1 audit (E7–E11), covering the items below. No status changed. | D-001, D-002, D-003, D-006, D-009, D-010, D-011, D-012, D-013 |
| 2026-10-25 | Phase 3 fix-up F1: the runtime maps `main`'s stack from a size the compiler declares. No status changed. | D-009, D-022 (D-011 noted) |
| 2026-10-26 | Phase 4 design gate (E13), owner approval: the D-009 Phase 4 stack amendment accepted (256 MiB default, `mmap` flags `0x20022` without `MAP_NORESERVE`, documented failure limitations); D-011 accepted as ABI-S0; D-023 accepted with staged introduction and its 4.1 subset. No Phase 4 code exists yet. | D-009, D-011, D-021, D-022, D-023 |

The pre-Phase 1 audit made these corrections and clarifications:
- Corrected the `/` and `%` rationale in D-012.
- Added the `ParseComments` parser mode to D-001 (the gate needs directive
  comments).
- Added Aether-side program checks that `go/types` does not perform (D-002).
- Added the `tool` diagnostic class and a classification precedence (D-003).
- Pinned down ambiguous S1 forms in D-012: constant-expression operands,
  redeclaration, `println` arguments, and the blank identifier.
- Recorded runtime `println` behavior (D-009).
- Stated a resolving experiment for each Proposed decision.

| Date | Event | Decisions affected |
|---|---|---|
| 2026-10-24 | Phase 1 implementation (E12): D-018 added; D-003 precedence recorded as implemented; `go.mod` created per D-015; S1 gate edge cases found by tests, the GOROOT sweep, and fuzzing recorded | D-002, D-003, D-012, D-015, D-018 |
| 2026-10-24 | Phase 1 closure audit: D-019 and D-020 record two implemented classifications; D-018 corrected and implemented for syntax-error positions under `//line`; fuzz incident FI-1 recorded in `TESTING.md` | D-018, D-019, D-020 |

---

## D-001: Reuse `go/parser`, `go/ast`, `go/token` for the initial frontend

**Status:** Accepted

**Context.** Aether needs a correct Go parser with accurate source positions.
Writing one is well understood but large. It does not advance the parts of the
project that make Aether an independent compiler: IR, optimizer, and backend.

**Decision.** Parse with `go/parser` into `go/ast`, using mode
`parser.ParseComments | parser.SkipObjectResolution`:
- `SkipObjectResolution` because `ast.Object` is deprecated.
- `ParseComments` because without it comments, including `//go:` and
  `//line` directives, are dropped from the AST (E11), and the subset gate
  could not reject them.

Use `go/token.FileSet` for positions inside the frontend.

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

**Rationale.** `go/types` implements the same type-checking rules as gc's
checker (`types2` is a port of `go/types`, per `cmd/compile/README.md`).
Experiment E3 showed that it reports "declared and not used", out-of-range
constant conversion, and constant division by zero. These are compile-time
rules Aether would otherwise need to reimplement before it could reject
invalid programs.

**Consequences.**
- Compile-time semantics, including the values of *constant expressions*,
  come from `go/types`. Aether must not claim to own them.
  - Lowering reads a constant's value from `types.Info.Types[e].Value`. That
    value is already converted to the expression's type: `1.0` used as `int`
    is recorded as kind `Int`, value 1 (E8).
  - Lowering extracts the value with `constant.Int64Val` and treats a
    non-exact result as an `internal` error. It never re-evaluates the
    expression with machine arithmetic.
- `go/types` is **not** all of gc's compile-time checking. Some program-level
  rules are enforced elsewhere in gc, so Aether's frontend must check them
  itself. For example, `go/types` accepts `package main` with no `func main`,
  while `go build` rejects it with "function main is undeclared in the main
  package" (E8, E9). These checks run only when type checking succeeded
  (E12).
- `go/types` and gc's checker are not identical. `go/types` wrongly rejects
  `func main() ()`, which gc and the spec accept (E12). Where they disagree,
  the gate's pre-check intercepts the construct, so valid Go is never
  classified as `invalid`. New disagreements are bugs to record and handle
  the same way.
- `go/types` implements no run-time semantics. What remains Aether's job:
  - deciding layouts for codegen (using the same `Sizes`, so `unsafe.Sizeof`
    agrees)
  - lowering semantics
  - evaluation order of non-constant expressions
  - all run-time behavior
- **No importer** is installed (`Config.Importer` stays nil) until Phase 11.
  Imports are rejected by the subset gate's pre-check (D-003).
  - Aether must not import `go/importer` or `go/build`. Both transitively
    depend on `os/exec` (E9), and gc's importer can run the `go` command. That
    would put the Go toolchain inside the compiler and bypass the process
    choke point (`ARCHITECTURE.md` §8).
  - The Phase 11 importer design must preserve this.

**Evidence.** `go doc go/types.Config`, `go doc go/types.Info`,
`go doc go/types.SizesFor` (local source); experiments E3, E8, E9.

**Revisit when.** Type checking dominates compile latency, Aether needs
information `types.Info` does not expose, or the owner wants semantic analysis
ownership as a project goal.

---

## D-003: Explicit subset gate and diagnostic classification

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
| `invalid` | The program is not valid Go | `go/parser`, `go/types`, and Aether's program-level checks that `go/types` does not perform (D-002) |
| `unsupported` | Valid Go outside Aether's declared subset | subset gate |
| `internal` | Aether malfunctioned (an ICE) | any later stage |
| `tool` | A required external tool is missing, unidentified, or failed (D-008) | toolchain driver |

Command-line usage errors are not diagnostics about the program. They have
their own exit code (`ARCHITECTURE.md` §Diagnostics).

**Precedence**, so classification is deterministic:
1. Pre-check `unsupported`: file-level constructs rejected before type
   checking, listed in `ARCHITECTURE.md` §2. Imports must be among them,
   because `go/types` cannot check them without an importer.
2. `invalid`.
3. Typed `unsupported`.

A program that is both invalid and uses an unsupported type is therefore
`invalid`. A program with an import is `unsupported` even if it has other
errors.

**As implemented in Phase 1** (`cmd/aether/check.go`), each step runs only if
every earlier step reported nothing:
1. More than one source file → `unsupported` (checked on the arguments).
2. Syntax errors → `invalid`. The pre-check needs a parsed file; running it
   on a partial AST would make the result depend on parser error recovery.
3. File-level pre-check → `unsupported`.
4. Type errors, then program-level checks → `invalid`. Program-level checks
   run only if type checking succeeded, because after type errors the
   package scope can be incomplete (E12).
5. Typed walk → `unsupported`.

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

**Evidence.** Phase 1 tests: the precedence cases in
`cmd/aether/testdata/check/precedence_*.go`, the GOROOT `// run` sweep (zero
`internal` results), and fuzzing of the whole check pipeline (E12).

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
  (`docs/ARCHITECTURE.md` §4, Invariants).

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

**Phase 2 note.** The load/store form was implemented without difficulty for
S1. D-021 fixes the concrete S1 instruction set and refines three details
above: slots are declared on the function instead of by an `alloc`
instruction, positions record the file once per function, and the only S1
terminator is `ret`.

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

**Resolving experiment.** On the completed Phase 4 IR:
- Implement promotion of non-address-taken slots on the S2 suite.
- Confirm that the interpreter gives identical results before and after
  promotion, and that the verifier passes.
- Record the implementation complexity, and the static instruction counts
  against the Phase 4 slot-per-value backend.

**Revisit / resolve when.** Phase 4 is complete. Resolve with a design note
added to this entry.

---

## D-007: Executable path: GNU assembler text, `as`, `ld`

**Status:** Accepted. D-008 (owner review, 2026-10-24) extends this path beyond
the early phases.

**Context.** The first milestone must produce a native Linux x86-64 executable
whose program logic comes from Aether's backend.

**Decision.** The amd64 backend emits GNU assembler source in AT&T syntax. The
driver runs `as --64` on it and on Aether's runtime assembly (D-009), then links
with `ld -static -nostdlib`, entry symbol `_start`. No libc, no C compiler, no
Go toolchain in the build path.

**Ownership boundary.** Aether chooses every instruction, register, stack slot,
symbol, and data layout. `as` only encodes instructions Aether has fully
specified and writes an ELF relocatable object. `ld` only resolves symbols
and lays out the static executable. D-008 defines the details.

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

**Revisit when.** A trigger listed in D-008 occurs.

---

## D-008: GNU `as`/`ld` as the long-term encoding and linking path

**Status:** Accepted (owner review, 2026-10-24). This was originally registered
as a Proposed evaluation of an in-house encoder, ELF writer, and linker. That
option is kept below as an alternative that was considered and not
prioritized.

**Context.** D-007 put GNU binutils in the build path for the first milestone.
The open question was whether to plan to replace them.

**Decision.**
1. GNU `as` and `ld` are accepted as the initial, and potentially long-term,
   tools for encoding and linking.
2. **Aether owns:**
   - instruction selection
   - register allocation
   - stack layout
   - calling convention
   - assembly emission
   - symbol naming
   - data layout
   - the runtime
   - the semantics of the generated program
3. **`as` only:** translates Aether-generated assembly into ELF relocatable
   objects.
4. **`ld` only:** combines Aether's objects and runtime objects into the final
   static executable.
5. No custom assembler, ELF object writer, or linker is to be prioritized
   unless a concrete engineering requirement justifies one. That requirement
   must be recorded as a new decision that supersedes this one.

**Tool requirements.** These are binding on the driver (`internal/toolchain`,
`ARCHITECTURE.md` §8).
- **Availability check before use.**
  - Locate `as` and `ld` (on `PATH` by default; an explicit override mechanism
    is defined in Phase 3).
  - Confirm they are GNU tools: the first line of `as --version` starts with
    `GNU assembler`, and `ld -v` starts with `GNU ld` (both observed in E6).
  - Confirm they can target x86-64 ELF.
  - The host and target platform is checked too: Aether only targets
    linux/amd64, and running produced executables requires a linux/amd64 host.
- **Clear errors.** A missing, non-GNU, or failing tool produces a diagnostic
  of class `tool` (exit code 5, per the proposal in `ARCHITECTURE.md`). The
  diagnostic names the tool, the path searched or used, the version line when
  available, and the tool's stderr. No executable is left at the output path
  (`ARCHITECTURE.md` §Diagnostics).
- **Version recording.** The tool version lines are recorded in `-x`/verbose
  output and in test logs.
- **Reproducible builds.** With identical inputs and configuration, output is
  byte-identical:
  - Identical inputs means the same Aether binary, source bytes, flags, and
    `as`/`ld` versions.
  - Tools run by absolute path, with fixed argument lists and a fixed
    environment (`LC_ALL=C` only; E9), in a fresh work directory, with fixed
    relative file names.
  - Generated assembly must not contain absolute or temporary paths,
    timestamps, or host-dependent data. In particular, any `.file` directive
    may name only the source path as given on the command line.
  - No build-id or other nondeterministic linker option is passed.

**Alternatives considered.**
1. **An in-house x86-64 encoder plus an ELF relocatable writer, keeping `ld`.**
   - It would need correct encoding (REX prefixes, ModRM/SIB, displacement
     sizes, immediates) for every emitted instruction, plus branch relaxation,
     section, symbol, and string tables, and relocations (`elf(5)`, gABI,
     psABI).
   - **Benefit:** no `as` dependency, and encoding becomes testable byte for
     byte against `as`.
   - **Not prioritized:** large cost; it does not change any semantic
     decision Aether makes.
2. **A full in-house linker as well.**
   - **Benefit:** fully hermetic builds.
   - **Not prioritized:** adds static-linking and layout work on top of
     option 1, with no current requirement.
3. **Use binutils only through Phase 5, then reassess.** This was the original
   Proposed plan. It was replaced by this decision so that early backend work
   is not shaped around a replacement that may never be needed.

**Rationale.** The project's ownership goal is about the decisions that
determine the generated program. Under this boundary every one of those
decisions stays with Aether. `as` and `ld` are mature, well-documented, and
available on the target platform. Phase 0 experiments (E2, E6) showed they
produce static, libc-free executables reproducibly.

**Consequences.**
- GNU binutils is a host prerequisite for `aether build`, and it is documented
  in `README.md`.
- Hosts without GNU binutils are unsupported. Tests that need the tools skip
  with a visible message (`TESTING.md`).
- Milestone 1 provenance is established by having the test harness rebuild
  the executable independently from Aether's emitted assembly with GNU
  `as`/`ld` and comparing bytes (`ROADMAP.md` §Milestone 1, check P3). It does
  not rely on Aether owning the encoder.
- Debug info (DWARF), if added, would also be emitted as assembler directives.

**Revisit when** any of these concrete requirements appears:
- `as`/`ld` time measurably dominates compile latency on recorded workloads
  (`BENCHMARKS.md`).
- A required platform lacks GNU binutils.
- Hermetic, tool-free builds become a stated requirement.
- A needed encoding or linking feature cannot be expressed through GAS or GNU
  ld.

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
  `exit_group(0)`. Amended below: since F1, `main.main` runs on a stack the
  runtime maps, and from Phase 4 increment 4.2 that stack has a fixed
  minimum size and a software bound.
- Printing support for `println` of integers (signed decimal, space-separated,
  trailing newline, to file descriptor 2, matching gc; see E1), using the raw
  `write` syscall.
  - Short writes are continued and `EINTR` is retried.
  - Any other write error is ignored, and the program continues. gc
    ignores write errors too: with stderr closed or on `/dev/full`, a gc-built
    `println` program still exits 0 (E7).
- For Phase 4, a minimal panic path: print the message, `exit_group(2)`.

**Amendment (2026-10-25, F1, detailed in D-022).** `_start` no longer runs
`main.main` on the process stack. It maps a stack sized from the compiler's
`aether.mainstack` datum, with a guard page, and exits with status 2 and a
fatal error if it cannot. The process stack limit no longer decides whether a
valid program runs. This covers Phase 3 only, where `main.main` is the only
function and its stack need is known exactly at build time. It is not a
general stack strategy: with calls of unknown depth (Phase 4 recursion) no
size is known at build time, and overflow must be detected rather than
excluded. The Phase 4 amendment below replaces it from increment 4.2.

**Amendment (2026-10-26, Phase 4 stack; accepted by the owner, E13).** From
Phase 4 increment 4.2, when functions and recursion arrive, `_start` maps one
runtime stack and the generated code enforces its bound. The guard page is
not what detects overflow.
- **Size.** S = max(DEFAULT, `aether.mainstack`) with DEFAULT = 256 MiB. The
  `max` keeps Phase 3 programs whose `main` frame is larger than DEFAULT
  running.
- **Mapping.**
  - `total` = 4096 + round_up(S + 4096, 4096): a guard page, then the stack
    rounded up to whole pages.
  - `mmap(NULL, total, PROT_READ|PROT_WRITE, 0x20022, -1, 0)`, where `0x20022`
    is `MAP_PRIVATE|MAP_ANONYMOUS|MAP_STACK`, the flags Phase 3 already uses.
    `MAP_NORESERVE` (`0x4000`) is deliberately not used.
  - `mprotect(base, 4096, PROT_NONE)`: the lowest page is the guard.
- **Layout.** [`base`, `base`+4096) is the guard page. [`base`+4096,
  `base`+8192) is a 4096-byte reserve, used only as stated in D-011. The stack
  limit is `L` = `base` + 8192, and frames may use [`L`, `top`), where `top` =
  `base` + `total`. Since `total` − 8192 ≥ S ≥ `aether.mainstack`, the check
  in `main.main`'s own prologue always passes.
- **Startup order.** `_start` stores `L` in `aether.stacklimit`, a writable
  8-byte `.bss` word, **before its first call into Aether code**. If it did
  not, the word would be 0 and every bound check would pass. `_start` then
  sets `%rsp` to `top` and calls `main.main`. From increment 4.6, which adds
  package initialization (D-023), it calls `main.init` first, then
  `main.main`. It then calls `exit_group(0)`.
- **Overflow.** A prologue check that fails jumps to `aether.stackoverflow`,
  which writes `fatal error: stack overflow` to fd 2 and exits with status 2
  (`SEMANTICS.md`, planned divergence V-2).
- **Mapping failure.** As in Phase 3: `fatal error: cannot allocate the main
  stack`, exit 2 (V-1).

**What is guaranteed:**

| Failure | Outcome | Guaranteed |
|---|---|---|
| Overflow detected by a prologue check | `fatal error: stack overflow`, exit 2 | Yes, deterministically for a given program and stack size. Write errors are ignored, so the message can be lost if fd 2 is unwritable, but the status is still 2. |
| `mmap` or `mprotect` fails | V-1 message, exit 2, before any Aether code runs | Yes |
| Memory runs out while stack pages are first written | Can be killed by SIGKILL (the out-of-memory killer, system-wide or a memory cgroup), or get SIGSEGV or SIGBUS; no message | **No.** SIGKILL cannot be caught, and the runtime installs no signal handlers. |
| A compiler or runtime defect writes below the reserve | SIGSEGV if the write hits the guard page; a write more than 4096 bytes below can corrupt other memory instead | **No.** This is a bug, not supported behavior. |

**Why no `MAP_NORESERVE`.** Neither flag set guarantees physical memory.
Pages are allocated on first write, and `mmap(2)` warns that without a
reservation "one might get SIGSEGV upon a write if no physical memory is
available". The flag only changes when the kernel may refuse the mapping:
- **Overcommit mode 0** (the default; measured on this host, E13): an ordinary
  mapping larger than about RAM plus swap is refused (14 GiB succeeded,
  16 GiB failed with `ENOMEM`). With `MAP_NORESERVE` even 1024 GiB succeeded.
  The kernel marks the two as `ac` (accounted) and `nr` in the `VmFlags` of
  `/proc/<pid>/smaps`.
- **Mode 1:** neither is ever refused. **Mode 2:** the kernel ignores
  `MAP_NORESERVE` and charges both against `CommitLimit`. Both statements come
  from the kernel's overcommit documentation and were not run here (changing
  the mode needs a system setting change).

At 256 MiB the flag changes nothing on a host with enough RAM and swap. On a
small host it would turn a clean startup failure (V-1) into a possible later
kill, so it is not used.

**Limitations (documented, not handled):**
- Out-of-memory termination while the stack is in use cannot be made clean.
- A guard-page fault means a defect, and produces no diagnostic.
- With a stack of at least 256 MiB, a small `ulimit -v` makes every program
  fail at startup with V-1. Phase 3 programs ran under such limits. Measured
  on 2026-10-29 by bisection on this host, for the 9,240-byte executable of
  `s1_03_milestone1`: it runs with `ulimit -v 262328` (KiB) and fails with
  V-1 at 262327, a threshold of 256 MiB plus 184 KiB. Other programs and
  hosts can differ slightly. `TestRuntimeStack` checks a failure at 128 MiB
  and a run at 1 GiB.
- Runaway recursion uses up to S bytes of memory before it overflows.
- Aether frames are larger than gc's, so Aether overflows at a smaller
  recursion depth than gc does at its 1e9-byte limit. The limit is stated in
  bytes, not calls.

**Required tests** (none has run yet; they belong to increment 4.2): see
D-011 and `ROADMAP.md` Phase 4.

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

**Resolving experiment.**
- Emit a pointer map for one function in the Phase 5 backend, then check it
  against a hand-written expected map.
- Measure peak RSS of Phase 7 workloads under a bump allocator (`/usr/bin/time
  -v`).
- The owner decides whether that growth is acceptable as an interim step.

**Resolve when.** Before Phase 7 begins.

---

## D-011: Calling convention for Aether-compiled functions

**Status:** Accepted (2026-10-26, owner approval after the Phase 4 design
gate, E13): option 3, as the stack-based convention **ABI-S0**. It is required
before Phase 4 increment 4.2, which introduces calls; increment 4.1 does not
depend on it.

**Context.** Phase 3 has only two kinds of calls:
- `_start → main.main`, with no arguments.
- `main.main →` the runtime print helpers. These use the narrow
  runtime-helper interface in `ARCHITECTURE.md` §Runtime integration boundary,
  which is fixed in Phase 3 and does not pre-empt this decision.

Phase 4 adds Aether-compiled functions with parameters and multiple results.
This decision covers those.

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

**Decision: ABI-S0.** Calls between Aether-compiled functions are direct calls
to the symbol `main.<name>`, defined in one documented place in the amd64
backend. A register-based successor follows in Phase 5 with the register
allocator. There is no foreign (System V) boundary; adding one needs its own
decision.
- **Words.** Each argument and result is one 8-byte word in canonical form
  (D-023): signed integers sign-extended to 64 bits, unsigned integers
  zero-extended, `bool` as 0 or 1.
- **Outgoing area.** A caller reserves A = 8·max(P+R) bytes over all its call
  sites (P arguments, R results; 0 without calls) at the **bottom** of its
  frame. A is part of the frame size, Size = round_up(8·(slots + values) + A,
  16), and **Size ≤ 2³⁰** (`maxFrame`), outgoing area included; a larger frame
  is an internal error.
- **Offsets.** The caller stores argument i at `8i(%rsp)`, calls, and reads
  result j from `8(P+j)(%rsp)`. The callee sees the same words at
  `16+8i(%rbp)` and `16+8(P+j)(%rbp)`, because `call` and `pushq %rbp` lower
  `%rsp` by 16 before `movq %rsp, %rbp`.
- **Caller.** It never moves `%rsp` after the prologue, so nothing is cleaned
  up after a call. It copies every result into a value cell before its next
  call (D-023 tuple storage), and assumes nothing about the argument words
  after the call.
- **Callee.** It may write anywhere in its argument and result words, and must
  write every result word before `ret`. It restores `%rbp` and `%rsp`, so
  `ret` pops only the return address.
- **Alignment.** `%rsp` ≡ 0 (mod 16) at every call; so 8 at entry and 0 after
  `pushq %rbp`. Size is a multiple of 16, so every call made from a body,
  including calls to runtime helpers, is aligned.
- **Registers.** The callee preserves only `%rbp` and `%rsp`. Every other
  general-purpose register, the flags, and the vector registers may be
  clobbered. The direction flag is clear at entry and exit. The frame-pointer
  chain (`0(%rbp)` holds the caller's `%rbp`) is mandatory.
- **Prologue.** Before anything is written below the return address:

  ```
  f:  movq  aether.stacklimit(%rip), %rax
      addq  $Size+8, %rax
      cmpq  %rax, %rsp
      jb    aether.stackoverflow
      pushq %rbp
      movq  %rsp, %rbp
      subq  $Size, %rsp      # omitted when Size is 0
  ```

  `%rax` is free at entry because ABI-S0 passes nothing in registers. The
  jump is a `jb`, not a call, so nothing is pushed. The epilogue is unchanged
  from Phase 3: `movq %rbp, %rsp; popq %rbp; ret`.
- **Recursion** needs nothing special: every activation has its own frame,
  and every prologue, including `main.main`'s, is checked.
- **Boundaries.**
  - Aether to Aether: ABI-S0.
  - Aether to runtime helpers: the existing helper interface (argument in
    `%rdi`; the helper preserves `%rbx`, `%rbp`, `%r12`–`%r15`). It is
    stricter than ABI-S0, so it is compatible. Every helper must state its
    stack use below `%rsp`, which must fit the reserve (below).
  - Process entry: `_start` calls `main.main` (and from increment 4.6
    `main.init`) as ABI-S0 calls with no arguments or results, and relies on
    no register surviving them.
- **Compatibility.** There is no separate compilation, so the compiler and
  runtime always change together, and ABI-S0 carries no stability promise.

**Why the bound check is correct.** With `L` the stack limit and `top` the
stack top (D-009 Phase 4 amendment):
- **Invariant I:** `%rsp` ≥ `L` at every call into Aether code. `_start`
  calls with `%rsp` = `top` ≥ `L`.
- The `call` writes the return address at [`rsp_call` − 8, `rsp_call`), at
  least `L` − 8: inside the reserve, above the guard page. The callee's entry
  `%rsp` is E = `rsp_call` − 8 ≥ `L` − 8.
- The callee writes the saved `%rbp` at [E−8, E) and its frame, outgoing area
  included, at [E−8−Size, E−8). Its lowest write is E − 8 − Size, so the
  requirement is E − 8 − Size ≥ `L`, that is **E ≥ T = `L` + Size + 8**.
- The four check instructions do not touch the stack, so `%rsp` = E when it
  compares, and execution falls through exactly when E ≥ T.
- After the prologue, `%rsp` = E − 8 − Size ≥ `L`, and the body never moves
  `%rsp`, so every call the body makes satisfies invariant I.
- **No wraparound.** `L` < 2⁴⁷ (a user-space address from `mmap` with no
  hint), and Size + 8 ≤ 2³⁰ + 8 < 2³¹, so the sign-extended 32-bit immediate
  is exact and positive. T < 2⁴⁷ + 2³¹ cannot carry out of 64 bits. The only
  way to get a false "safe" result is a wrong `L`, which is why `_start` must
  store it before its first call (D-009).
- **Reserve.** Below `L`, only these are written: a return address (8 bytes);
  the helpers, from a checked body (`aether.printint`: 8 + 40 + 8 = 56 bytes;
  `aether.printsp` and `aether.printnl`: 8); and `aether.stackoverflow`,
  entered at `%rsp` ≥ `L` − 8, which aligns `%rsp` (up to 8 bytes) and calls
  `aether.write` (8 bytes), at most 24 bytes. `aether.write` uses no stack.
  All fit in the 4096-byte reserve. A new helper, such as Phase 4's panic
  path, must state its own bound.

**Phase 3 basis (F1, D-022).** `main.main` runs on a stack the runtime maps
itself. ABI-S0 keeps that model, and D-009's Phase 4 amendment fixes its size
and limit.

**Rationale.** A stack-only convention is the smallest one that supports
parameters, any number of results, and recursion with the existing
slot-per-value backend, which keeps no value in a register across
instructions. The check makes overflow a clean, deterministic error for any
frame size, including frames larger than the guard page.

**Consequences.**
- Every call costs a memory load, an add, a compare, and a branch in the
  prologue.
- The backend's `NewFrame` must include the outgoing area in its 2³⁰ bound.
- The runtime gains `aether.stacklimit` and `aether.stackoverflow` (D-009).

**Evidence (E13).** A hand-written assembly prototype of this convention, run
outside the repository:
- a recursive function with one argument and two results, checking the
  callee's result and `%rsp` alignment at every level: depths 0, 1, 1000, and
  1,000,000 exit 0;
- 1.5 million frames of 48 bytes in a 64 MiB stack, and unbounded recursion
  with 1 MiB frames: `fatal error: stack overflow`, exit 2;
- with the check removed, both overflow cases die with SIGSEGV instead;
- a `jb` to `aether.stackoverflow` defined in another object file links with
  static `ld` (an `R_X86_64_PLT32` relocation) and works taken and not taken.

**Not yet executed** (required tests in `ROADMAP.md` Phase 4): the boundary
entries E = T and E = T − 16, the helpers and the overflow handler at the
reserve's edge, argument offsets with P other than 1, and the D-009 sizing.
The prototype mapped with `MAP_NORESERVE` (`0x24022`); rerun with `0x20022`,
its results were identical.

**Revisit when.**
- Phase 5 adds the register-based successor. It must keep a scratch register
  free at function entry for the check.
- Multi-word values (strings and structs, Phase 6).
- Closures need a context register (Phase 8).
- `defer`, `recover`, and unwinding (Phase 8).
- The runtime installs signal handlers: they then need an alternate signal
  stack, since only the reserve lies below `L`.
- Goroutines (Phase 10): the limit becomes per goroutine; the prologue keeps
  its shape.
- Stack maps for garbage collection (D-010).

---

## D-012: Initial supported Go subset (S1)

**Status:** Accepted (owner review, 2026-10-24, with the clarifications below)

**Decision.** Subset S1 is defined precisely in `docs/SEMANTICS.md` §Subset S1:
- A single file, `package main`, no imports, and only `func main()`.
- Local `var`/`:=`/`const` declarations of type `int`, assignment, `+= -= *=`,
  and `++`/`--`.
- Binary `+ - *`, unary `+ -`, parentheses, and nested blocks.
- `println` with `int` arguments.
- Everything else is rejected as `unsupported`.

**Owner clarifications (binding):**
1. **`println`.** It is supported in S1 as an explicitly supported compiler
   builtin. This is not a guarantee that `println` stays in the Go language,
   nor a commitment to any `println` behavior beyond what `SEMANTICS.md`
   specifies.
   - Output goes to **stderr**, matching gc, which was re-verified on the full
     S1 case catalog (E4).
2. **Specification fidelity.** S1 follows the Go specification for:
   - integer overflow (wraparound)
   - constant expressions
   - evaluation order
   - type checking
3. **Constants versus run-time arithmetic.** Constant-expression semantics are
   **never** inferred from run-time arithmetic.
   - Constant expressions are exact, and their values come from `go/types`
     (D-002).
   - A constant that does not fit `int` is a compile error (`invalid`), even
     where the same arithmetic on variables would wrap at run time (E5).
4. **Rejection.** Unsupported constructs are rejected explicitly, never
   silently miscompiled (D-003).
5. **Documentation.** S1's limitations and the exact verified test cases are
   documented in `SEMANTICS.md` §Subset S1.
6. **Imports.** The no-import restriction stays until package compilation is
   a separately scoped milestone (Phase 11 in `ROADMAP.md`, or an earlier
   milestone if the roadmap is re-scoped).

**Audit clarifications (2026-10-24).** These remove ambiguities in the accepted
scope. They do not add features. Details are in `SEMANTICS.md` §Subset S1.
- **Constant-expression operands.** The S1 operator and literal restrictions
  apply to constant expressions too.
  - A constant expression may use only integer literals, S1 constants, `+ - *`,
    unary `+ -`, and parentheses.
  - Float, imaginary, rune, and string literals are `unsupported` even where
    the resulting constant is an `int`, as in `var x int = 1.0`, which
    `go/types` accepts (E8).
- **Short variable redeclaration** (`a, c := 3, 4` with `a` already declared)
  is part of `:=` and is supported. A redeclared identifier is recorded in
  `types.Info.Uses`, not `Defs`, and must be lowered as an assignment to the
  existing variable. Treating it as a new variable would be a silent
  miscompilation (E8; case S1-10).
- **`println` arguments** must have type `int` in `types.Info`. Untyped
  integer constants default to `int` there (E8) and are accepted. Arguments of
  type `rune`, `float64`, or any other type are `unsupported`.
- **The blank identifier** is `unsupported` in every position, declaration as
  well as assignment.
- **Program-level checks.**
  - A package name other than `main` is `unsupported`: Aether builds programs,
    not libraries.
  - `package main` without `func main` is `invalid`. `go/types` does not report
    this, so Aether checks it (D-002, E8).

**Phase 1 implementation (2026-10-24).**
- The gate enforces S1 by default-deny. Testing found forms that need explicit
  rules (E12), now listed in `SEMANTICS.md` §Subset S1:
  - `func main() ()`
  - `func main()` without a body
  - `iota`
  - implicit constant values
  - parenthesized assignment targets
  - uses of package-level declarations
- All of these are rejected as `unsupported`, so S1's scope is unchanged.

**Rationale.**
- The program is observable without imports: `println` is a predeclared builtin
  defined in the spec's Bootstrapping section, and gc writes its output to
  stderr (E1). This allows byte-exact differential testing against gc.
- `/` and `%` are excluded for two **different** reasons, and both need work
  that S1 does not otherwise require (spec §Arithmetic operators, §Run-time
  panics; behavior confirmed in E1 and E7):
  - **Division by zero.** A zero divisor at run time causes a run-time panic,
    which needs the Phase 4 panic path. A constant zero divisor is already a
    compile error (E3).
  - **`MinInt / -1` and `MinInt % -1`.** These do **not** panic. The spec
    defines `q = x` and `r = 0`, and the gc-built binary printed
    `-9223372036854775808 0` and exited 0. x86-64 `IDIV` faults on this
    case, and Linux kills the process with SIGFPE (E10). So the backend needs
    an explicit check and a separate code path, not panic support.
  - Division truncates toward zero, and the remainder has the sign of the
    dividend (`-7/2 == -3`, `-7%2 == -1`, E7). This matches `IDIV` apart from
    the two cases above.
  - Deferring both operators keeps S1 free of run-time panics and special
    cases.
- Shifts are excluded because a negative shift count panics at run time, and
  x86 shift instructions mask the count (E10), so counts at or above the width
  need explicit handling.
- Wraparound overflow (`+ - *`) is included deliberately to test that Aether
  never treats signed overflow as undefined.

**Alternatives.** Observing results through the process exit status instead of
`println`. That avoids runtime output code, but Go cannot set an exit status
without `os.Exit` (an import), so a valid Go program could not express it.

**Risk.** The spec says `print`/`println` "are not guaranteed to stay in the
language". If they are removed from Go, Aether may keep supporting `println`
as an Aether builtin for its own tests, but differential testing of S1 would
then need another output channel (for example `os`, once imports exist). This
is accepted.

**Revisit when.** S1 turns out to be unimplementable as specified, or `println`
is removed from Go or changes its gc behavior.

---

## D-013: Dependency policy

**Status:** Accepted

**Decision.**
- **Compiler code:** Go standard library only. Adding any module dependency
  requires a decision entry stating its purpose, alternatives, and long-term
  consequences.
- **External tools in the build path:** only GNU `as` and `ld` (D-007). The
  official Go toolchain must never be invoked to compile the input program.
- **External tools in tests:**
  - the official Go toolchain, as a semantic oracle
  - `readelf`, `nm`, and `objdump`, for inspection
  - GNU `as` and `ld`, run by the test harness itself to reconstruct
    executables independently for the provenance checks (`ROADMAP.md`
    §Milestone 1)

  These are host tools, not module dependencies.
- **Standard-library packages Aether must not import:** `go/importer` and
  `go/build`. Both transitively depend on `os/exec` and can invoke the `go`
  command (E9; D-002).
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
- `go.mod` was **not** created in Phase 0 or the Phase 0 review, because there
  was no Go code. It was created with the first real code in Phase 1, with
  `go 1.26` and module path `github.com/stevenstank/aether`, and no `require`
  or `toolchain` lines.
- The module path was confirmed by the owner on 2026-10-24. Both the fetch and
  push URLs of the `origin` remote were checked on that date and are exactly
  `https://github.com/stevenstank/aether.git`.
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

## D-018: CLI exit codes and diagnostic output

**Status:** Accepted (Phase 1). This finalizes the proposal that was in
`ARCHITECTURE.md` §Diagnostics.

**Context.** Tests and users must be able to tell, from the exit status alone,
whether a program was invalid, outside the subset, or hit an Aether bug
(D-003, `TESTING.md`).

**Decision.**

| Code | Meaning |
|---|---|
| 0 | success: for `check`, the program is inside the supported subset |
| 1 | `invalid` |
| 2 | usage error |
| 3 | `unsupported` |
| 4 | `internal` |
| 5 | `tool` |

- **Mixed classes.** If one run produces diagnostics of several classes, the
  code is chosen by precedence: `internal`, then `tool`, then `invalid`, then
  `unsupported`.
  - The classification order (D-003) normally yields a single class.
  - `internal` wins because no other result can be trusted once Aether has
    malfunctioned.
- **Streams.**
  - Diagnostics and usage errors go to **stderr**.
  - Only `aether help` writes to stdout.
  - A successful `check` prints nothing.
- **Rendering.**
  - Each diagnostic is one line: `file:line:col: class: message`.
  - Unsupported diagnostics are followed by ` [feature]`, using the feature
    names in `SEMANTICS.md`.
  - Diagnostics are sorted by position, then class, feature, and message.
  - Positions ignore `//line` directives, so they always name the real file,
    line, and column.
    - For type-checker and gate diagnostics, this uses unadjusted
      `token.Pos` positions.
    - For syntax errors, `go/parser` reports only adjusted positions. The
      frontend re-parses a copy with the directives disabled by a same-length
      substitution, which keeps offsets unchanged. Before the closure audit
      this was not done, and such errors could name a nonexistent file.
- **Usage errors (exit 2).** These are problems with the command line rather
  than the program:
  - no or unknown command
  - no input file
  - an unknown flag
  - a file name not ending in `.go`
  - an input file that cannot be read
- **Internal errors.** A panic anywhere in the check pipeline is recovered by
  the driver and reported as an `internal` diagnostic (exit 4). It is never
  reported as a problem with the program.

**Alternatives considered.**
- A single non-zero code for every failure. This is simpler, but tests could
  not tell a limitation from a bug without parsing messages.
- Reporting unreadable input files as `tool`. Rejected: `tool` is reserved for
  external tools (D-008).

**Rationale.** Distinct codes give the test harness a machine-readable
classification (`TESTING.md` table). Single-line, sorted diagnostics make
golden tests deterministic.

**Consequences.** The codes, streams, and rendering format are a contract.
Changing them requires a superseding decision.

**Evidence.** `internal/diag` tests (rendering, sorting, exit-code
precedence); `cmd/aether` tests (`TestRunUsage`, `TestRunMultipleFiles`,
`TestInternalErrorReporting`, `TestBinary`, the golden corpus).

**Revisit when.** A new class of failure appears that none of the codes
describes.

**Phase 3 note.** D-022 applies these codes to `aether build`: exit 5 for
`as`/`ld` problems, and exit 2 also for failures of Aether's own file
operations (the work directory and the output file).

---

## D-019: `func main() ()` is `unsupported`, not `invalid`

**Status:** Accepted (Phase 1 closure audit). It records the classification
implemented in Phase 1.

**Context.**
- An empty result list, `func main() ()` (likewise `func init() ()`), is
  valid Go: the spec requires only that `main` return no value, and gc builds
  it.
- `go/types` reports "func main must have no arguments and no return values",
  because it tests the result list for nil rather than for emptiness (E12).
- Left alone, a valid program would be classified `invalid`, contradicting
  D-003.

**Decision.** The gate's pre-check, which runs before type checking (D-003),
rejects an empty result list on `main` or `init` as `unsupported`, feature
`empty-result-list`.

**Alternatives considered.**
1. **Accept it as S1.** It means the same as `func main()`. But `go/types`
   would still report an error, so Aether would have to suppress that
   specific type-checker error, presumably by matching its message. That is
   fragile, and it means editing the output of a component Aether reuses.
2. **Classify it as `invalid`, following `go/types`.** Rejected: the spec and
   gc say it is valid Go, and the spec is normative (`SEMANTICS.md`).

**Rationale.** `unsupported` is accurate (valid Go, not accepted by Aether)
and safe (nothing is compiled). S1 is defined with the form `func main() { … }`,
so rejecting this alternate spelling does not narrow S1 as accepted.

**Consequences.** One valid spelling of an S1 program is rejected. If S1 ever
needs it, accepting it requires handling the `go/types` error.

**Evidence and tests.** E12. Corpus files
`unsupported_empty_result_list.go` and `unsupported_init_empty_result_list.go`
with goldens. `gate_test.go` cases "empty result list" and "empty result list
on init". `TestCorpusAgainstGc` confirms gc builds both files.

**Revisit when.** `go/types` changes its check, or S1 is extended to include
this form.

---

## D-020: `func main()` without a body is `unsupported`

**Status:** Accepted (Phase 1 closure audit). It records the classification
implemented in Phase 1.

**Context.**
- The spec allows a function declaration without a body: it "provides the
  signature for a function implemented outside Go, such as an assembly
  routine".
- `go/types` accepts `func main()` with no body.
- gc rejects it with "missing function body", because no assembly
  implementation is supplied.
- So the spec says the declaration is valid, gc says the build fails, and S1
  has no way to supply an implementation from outside Go.

**Decision.** The gate rejects a body-less `main` as `unsupported`, feature
`function-without-body`. The corpus records gc's differing verdict with
`// aether:gc invalid`.

A body-less `func init()` is a different case. It is invalid Go ("func init
must have a body"), reported by both `go/types` and gc, and Aether classifies
it `invalid`.

**Alternatives considered.**
1. **Classify it as `invalid`, following gc.** Rejected: the spec permits the
   declaration. gc's error concerns a missing external implementation, not the
   validity of the source, and the spec is normative.
2. **Report it as `internal`.** This was the original behavior, until fuzzing
   found it (E12). It is wrong: the case is reachable, not an Aether
   malfunction.

**Rationale.** The program is valid Go that needs a capability S1 lacks: a
function implemented outside Go. That is what `unsupported` means.

**Consequences.** Classification differs from gc for this one program shape.
The difference is recorded in the corpus header, so the gc cross-check stays
exact.

**Evidence and tests.** E12. Corpus files `unsupported_main_without_body.go`
(`// aether:gc invalid`) and `invalid_init_without_body.go`, with goldens.
`gate_test.go` case "main without body". Fuzz regression seed
`cmd/aether/testdata/fuzz/FuzzCheck/1b8f36b006382399`.

**Revisit when.** Aether supports functions implemented in assembly.

---

## D-021: The S1 IR instruction set and its contract

**Status:** Accepted (Phase 2). It refines D-004 and replaces the `println`
expansion previously described in `ARCHITECTURE.md` §3.
Its "no branches" rule, its single `i64` type, and the `println` operand rule
are extended by D-023 from Phase 4 increment 4.1; everything else stands.

**Context.** D-004 fixed the IR's shape and left the instruction set to
Phase 2. S1 needs integer values, mutable locals, wrapping `+ - *` and
negation, `println`, and the end of `main`. Anything more would be untested
speculation (D-016).

**Decision.** The IR for S1 (`internal/ir`) is:
- **Program → functions → slots and blocks → instructions.** The entry point is
  the function named `main`, whose entry block is `Blocks[0]`.
- **One type, `i64`.** Go's `int` lowers to it.
- **Slots, not `alloc`.** A function lists its local variable slots. Every S1
  local lives for the whole call, so an instruction would add nothing. A slot
  has **no initial contents**: lowering stores the zero value of `var x int`
  explicitly, and the verifier rejects a load not preceded by a store.
- **Instructions are values.** An instruction with a result is numbered
  `vN`, in creation order. Operands point at earlier instructions.
- **Nine operations:** `const`, `load`, `store`, `add`, `sub`, `mul`, `neg`,
  `println`, `ret`. Arithmetic wraps modulo 2⁶⁴ and never traps.
- **`println` is one instruction** whose operands are already-evaluated
  values. It writes them in signed decimal to standard error, separated by
  single spaces and followed by a newline, ignoring write errors. It is not
  expanded in the IR into print-int, print-space, and print-newline steps.
  The backend makes that expansion into calls to the runtime helpers, whose
  interface (`ARCHITECTURE.md` §Runtime integration boundary) is unchanged.
- **No branches.** `ret` is the only terminator. A function therefore has
  one reachable block, and the verifier rejects any other block as
  unreachable. Branch instructions come with control flow (S2).
- **Positions** are a line and column on every slot and instruction, with the
  file named once per function. Lowering gives every instruction a real
  source position, so there are no synthetic positions.
- The text format, the full list of invariants, and which of them lowering
  guarantees are in `ARCHITECTURE.md` §4.
- There is no `aether run-ir` command. Tests run lowered programs through the
  interpreter in-process, so the CLI and D-018 are unchanged.

**Alternatives considered.**
1. **Expand `println` in the IR** into print-int, print-space, and
   print-newline instructions, as Phase 0 planned. Rejected: every consumer
   would have to re-derive one output line from several instructions, and the
   rule that all arguments are evaluated before any output (E7) would depend
   on lowering's instruction order rather than on the instruction's meaning.
2. **Zero-initialize slots implicitly.** Rejected: an explicit store keeps
   every value the program observes visible in the IR, and lets the verifier
   catch a lowering bug that forgets an initializer.
3. **An `alloc` instruction per local** (D-004's wording). Deferred until a
   local's lifetime can be shorter than its function or its address can be
   taken.
4. **A user-facing `aether run-ir` command.** Not needed for Phase 2's
   purpose, testing, and it would add a CLI contract.

**Rationale.** It is the smallest instruction set that expresses S1 with
every operation's meaning stated on the operation. With no branches, "defined
earlier in the block" and "stored earlier in the block" are exact dominance
and definite-assignment checks, so the verifier is complete for S1.

**Consequences.**
- The backend must expand `println` itself (Phase 3).
- S2 adds branch terminators. The verifier's reachability, dominance, and
  load-before-store checks must then become control-flow aware.

**Evidence and tests.** `internal/ir` tests: the printer's exact output, and
29 verifier negative tests, one per invariant. `internal/lower` and the IR
goldens `cmd/aether/testdata/check/*.ir`. `internal/interp` unit tests.
`TestInterpAgainstGc` (now `TestDifferentialAgainstGc`): for the 12 corpus
programs expected to run and 200
generated S1 programs, the interpreter's stdout, stderr, and exit status
equal those of the gc-built binary.

**Revisit when.** S2 adds control flow, or a construct cannot be lowered to
this form without a new instruction.

---

## D-022: The Phase 3 backend, runtime interface, and `aether build`

**Status:** Accepted (Phase 3). It fixes what D-007, D-008, and D-009 left
to Phase 3: the runtime helper names and interface, the tool override
mechanism, and the `build` command's behavior. It does not decide D-011.

**Context.** Milestone 1 needs a backend, a runtime, and a driver. The
earlier decisions fixed their shape (GNU `as`/`ld`, AT&T syntax, a
slot-per-value backend, a hand-written assembly runtime) but not these
details.

**Decision.**
- **Backend** (`internal/amd64`). Every slot and every IR value has its own
  8-byte `%rbp`-relative stack slot; each instruction loads its operands into
  `%rax` (and `%rdi` for helper arguments), computes, and stores the result.
  Frames are rounded to 16 bytes and limited to 1 GiB; a larger frame is an
  internal error, never wrong assembly. The mapping is in `ARCHITECTURE.md`
  §7. `main` becomes the symbol `main.main`.
- **Main stack** (F1 fix-up, 2026-10-25). The backend emits the read-only
  datum `aether.mainstack = 16 + frame size`. `_start` maps that many bytes
  plus a 4096-byte helper reserve, rounded up to pages, with a `PROT_NONE`
  guard page below, and calls `main.main` on it. If `mmap` or `mprotect`
  fails, it prints `fatal error: cannot allocate the main stack` through the
  new runtime routine `aether.write` and exits with status 2 (`SEMANTICS.md`
  V-1). Before this, a frame larger than the process stack (8 MiB by
  default, less under a reduced `ulimit -s`) built successfully and crashed
  with SIGSEGV, while gc ran the same program. Lowering the 1 GiB limit was
  rejected: it would still fail under a smaller `ulimit -s`. Deriving a bound
  from `RLIMIT_STACK` was rejected: it needs the top of the kernel's stack
  mapping, which can only be inferred from the auxv layout, and it would
  still turn programs gc runs into failures.
- **Runtime helpers.** `aether.printint` (argument in `%rdi`),
  `aether.printsp`, and `aether.printnl`. They require `%rsp` 16-byte aligned
  at the call, return nothing, preserve `%rbx`, `%rbp`, and `%r12`–`%r15`, and
  may clobber every other general-purpose register. They write to fd 2,
  continue short writes, retry `EINTR`, and ignore other errors.
- **`println` expansion.** The backend expands each `println` instruction
  into one `aether.printint` call per operand, `aether.printsp` between
  operands, and `aether.printnl` at the end. The operands live in the frame,
  which the helpers do not touch, and are all computed before the first call
  (D-021).
- **Tools.** `as` and `ld` are found on `PATH` and resolved to absolute
  paths; `PATH` is the override mechanism. `as --version` must start with
  `GNU assembler` and name an `x86_64-` target; `ld -V` must start with
  `GNU ld` and list the `elf_x86_64` emulation (`-V` prints the `-v` version
  line followed by the emulations). A tool that exits 0 without writing its
  output is a tool failure.
- **`aether build [-x] [-work] -o OUT FILE.go`.**
  - `-o` is required. `OUT` may replace an existing regular file, only when
    the build succeeds. A directory, symbolic link, other non-regular file, or
    the input file itself is refused as a usage error, before building.
  - The program is classified, lowered, verified, and emitted before any tool
    is looked for, so invalid and unsupported programs never run a tool or
    create a work directory.
  - The executable is copied to a temporary file next to `OUT`, made mode
    0755, and renamed over `OUT`. It is always copied, never renamed from the
    work directory, which usually lies on another file system.
  - Failures of Aether's own file operations (creating the work directory,
    writing the output) are reported as `aether build: …` with exit 2, like
    D-018's unreadable input files.
- No `aether run-ir` command (D-021 stands).

**Alternatives considered.**
1. **One runtime call per `println`**, passing a count and a pointer to the
   values. Rejected: it needs a memory-passing convention for a variable
   number of arguments, which belongs to D-011.
2. **Tool override by flags or environment variables** (`-as`, `AETHER_AS`).
   Not needed: `PATH` already selects the tools, the identity check still
   applies, and the Milestone 1 tests use `PATH` stand-ins.
3. **A default output name** (as `go build` derives one). Deferred: an
   explicit `-o` avoids silently replacing a file the user did not name.
4. **Renaming the link output when it is on the same file system.** Copying
   always is simpler and has the same failure guarantees.

**Rationale.** Each choice is the smallest one that meets the Milestone 1
requirements, and keeps everything that determines the generated program on
Aether's side (D-008).

**Consequences.**
- Phase 4's calling convention (D-011) must coexist with the helper
  interface, which is not a general calling convention.
- Generated code is slow: every value goes through memory. Phase 5 replaces
  this.

**Evidence and tests.**
- `internal/amd64`: the assembly golden file, constant selection, frame
  layout and limits, native execution of every operation at integer
  boundaries against the interpreter, and an ABI harness that checks the
  preserved registers, `%rsp`, formatting of `MinInt64` and `MaxInt64`, and
  that a write to `/dev/full` is ignored. `TestRuntimeStack`: frames of 64
  MiB and 1 GiB under a 1 MiB stack limit, and the fatal error under
  `ulimit -v`. `TestRuntimeStackGuard`: the usable stack extends exactly to
  the documented size, the page below it faults, and the stack top is
  page-aligned.
- `internal/toolchain`: identification and rejection of tools, the fixed
  environment, and error contents.
- `cmd/aether`: `TestBuild*`, the Milestone 1 acceptance test (P1–P6), and
  `TestDifferentialAgainstGc` (gc, interpreter, and native on 212 programs),
  `TestBuildLargeFrame` (the F1 regression), and `TestBuildOutputRefusals`,
  `TestBuildCleanup`, and `TestBuildFileFailures` (output refusals, cleanup
  after tool failures, and failures of Aether's own file operations).

**Phase 4 note (2026-10-26).** D-011 is now accepted as ABI-S0, which
coexists with the helper interface. Phase 4 adds runtime symbols in stages:
`aether.printbool` in increment 4.1 (D-023), and `aether.stacklimit` and
`aether.stackoverflow` in 4.2 (D-009 amendment). Each enters the runtime
boundary table in `ARCHITECTURE.md` when it is implemented, and the exact
symbol set checked by the Milestone 1 test changes with it.

**Revisit when.** Phase 5 introduces register allocation.

---

## D-023: The S2 IR extension, introduced in stages

**Status:** Accepted (2026-10-26, owner approval after the Phase 4 design
gate, E13), with staged introduction. Each part enters the IR, verifier,
interpreter, and backend only in the increment named below, with its tests.
It extends D-004 and D-021 and supersedes D-021's "no branches" rule.

**Context.** S2 (`ROADMAP.md` Phase 4) needs control flow, `bool`, sized
integer types, calls with parameters and multiple results, run-time panics,
and package-level variables. The S1 IR has one block per function and one
type. SSA, φ-nodes, register allocation, and optimization stay in Phase 5
(D-006) and are not part of this decision.

**Decision.**
- **Values stay local to their block.** An operand must be defined earlier in
  the same block, as in S1. Anything that crosses blocks goes through a slot:
  variables, parameters, call results used later, and the temporaries of
  `&&` and `||`. The operand check stays exact, and the backend keeps one
  cell per value.
- **Types.** `bool`, `i8`, `i16`, `i32`, `i64`, `u8`, `u16`, `u32`, `u64`,
  and `tuple(T1, …, Tn)`, which only a multi-result `call` may have. `int`
  lowers to `i64`, `uint` and `uintptr` to `u64`, `byte` to `u8`, `rune` to
  `i32`. Signedness belongs to the type; an operation's meaning follows its
  operand type.
- **Canonical form.** Every value is held as 64 bits: signed types
  sign-extended from their width, unsigned types zero-extended, `bool` as 0 or
  1. A `const` holds this canonical 64-bit pattern in `Const` (an `int64`),
  which must be in range for its type: `bool` ∈ {0, 1}, `i8` ∈ [−128, 127],
  `u8` ∈ [0, 255], `u32` ∈ [0, 2³²), and so on; `u64` uses all 64 bits. Every
  operation on a type narrower than 64 bits produces the canonical form of
  its exact result reduced modulo 2^width. For example, `i8` −128 / −1 is 128
  in 64 bits, which is −128 in `i8`.
- **Terminators.** `jump bK`; `branch v, bT, bF` (v is `bool`); `ret v…`
  (operands match the function's results); `panic K`, where K is a closed set
  of run-time errors: `divide` ("integer divide by zero") and `shift`
  ("negative shift amount").
- **Operations.**
  - `const` (typed); `eq`, `ne`, `lt`, `le`, `gt`, `ge`, which produce `bool`
    (on `bool` operands, only `eq` and `ne`); `not` on `bool`.
  - `add`, `sub`, `mul`, `neg`: wrap at the type's width.
  - `and`, `or`, `xor`, `andnot`, `com` on integers.
  - `div`, `rem`: truncate toward zero; `Min / −1 = Min` and `Min % −1 = 0`.
    **Precondition:** the divisor is not zero.
  - `shl`, `shr`: the count is an unsigned value. A count at or above the
    width gives 0, or the sign fill for `shr` of a signed type.
  - `conv T→U` between integer types: the canonical form of the source's
    value modulo 2^width(U).
  - `param i` (entry block only); `call f(args…)` (direct calls only);
    `extract i` of a tuple.
  - `gload g`, `gstore g, v`.
  - `println`, whose operands may be any integer type or `bool`.
  - `print`, with the same operands and evaluation rule as `println`, but
    written with no separators and no trailing newline. Evidence: for `int`,
    E1 (`print(1, 2); print(3)` writes `123`); for `bool`, `uint8`, and
    `int8`, E13 (`true-1false255-128`). The format of the other sized types
    is not yet observed and is established by the required 4.4 tests against
    gc.
- **Explicit checks.** Lowering guards division by zero and negative signed
  shift counts with a comparison, a `branch`, and a block ending in
  `panic K`, then converts a checked count to an unsigned type. Every
  operation is therefore total on valid IR. If the interpreter meets a
  violated precondition, it reports an **internal error** (a lowering bug),
  never a Go panic.
- **Tuple storage.** A `call` with two or more results has a `tuple` type and
  its own R value cells, which the backend fills from the outgoing area
  immediately after the call, before any other call (D-011). `extract i`
  must take a tuple-typed `call` in the same block as its operand, with i in
  range, and copies cell i. A tuple is used only by `extract`.
- **Program.** `Globals` (name, type, position) start at zero, unlike slots,
  so loads of globals need no definite assignment. A synthetic `Init`
  function, emitted as `main.init` and called by `_start` before `main.main`,
  runs package initializers in `types.Info.InitOrder`. It cannot be inlined
  into `main`, because Go programs may call `main()` recursively (gc accepts
  it, E13). Functions gain `Params` and `Results`.
- **Positions.** Every instruction keeps a line and column, with one file per
  function (programs remain single-file). `branch`: its `if` or `for`
  keyword, or the `&&`/`||` operator. `jump`: its `break` or `continue`, or
  the closing brace that ends the block. `call`: the call expression.
  `panic`: the operator that can panic. Temporary slots: the operator that
  creates them. Globals: their identifier.

**Verifier invariants** (added or changed):
1. Every block is reachable from the entry. Lowering removes unreachable
   code (after `return`, `break`, or `for {}`) and renumbers blocks so IDs
   still equal indices.
2. Every block ends in exactly one terminator; `jump` and `branch` targets
   are blocks of the function; a `branch` condition is `bool`.
3. Operands are defined earlier in the same block, and their types are
   exactly what the operation requires (both operands of a binary operation
   have the same type).
4. **Definite assignment** is a forward "must be stored" dataflow over the
   reachable blocks:
   - `in(entry)` = ∅; every other block starts with `in` = all slots.
   - `out(b)` = `in(b)` plus the slots `b` stores.
   - `in(b)` = the intersection of `out(p)` over b's predecessors.
   - Iterate in block order until nothing changes. The result is the greatest
     fixed point; starting the non-entry blocks at ∅ would reject valid loops.
   - Within a block, a `load` of slot s is valid if s is in `in(b)` or is
     stored earlier in the block.
5. Each `const` is in range for its type (canonical form).
6. `param` appears only in the entry block, with an index and type matching
   `Params`. `ret` operands match `Results`. `main` and `Init` have neither.
7. A `call`'s callee is a function of the program; its argument types match
   the callee's `Params`; its type is none, the single result type, or the
   tuple of `Results`. `extract` follows the tuple-storage rule above.
8. Globals have unique names and valid types, and every `gload` and `gstore`
   names a global of the program.
9. Every `panic` kind is in the closed set.

**Interpreter.** Frames hold their slots and current block; values are kept
in canonical form. Increment 4.1 adds a step budget, for tests only, so a
lowering bug that loops fails instead of hanging.

Increment 4.2 makes `Run` report an exit status (0 for a normal return from
`main`), and adds IR calls under two requirements:
- **An interpreter-managed frame stack.** `Run` executes calls and returns
  by pushing and popping frames on an explicit stack (a slice of frames) in
  one loop, never by a Go call per IR call. Its Go stack use therefore does
  not grow with IR call depth. A depth limit alone cannot be justified as Go
  stack protection: the interpreter's Go stack use per IR call is unknown
  until it is written. Go's own stack overflow is a fatal error that cannot
  be recovered and would end the test binary, which is itself a gc-built
  program; gc-built programs limit a goroutine's stack to 1e9 bytes by
  default, as the gc output in E13 and E14 states. The frame stack is a
  **hard prerequisite**: the interpreter executes no IR `call`, recursive or
  not, until it exists. Its Go-stack safety rests on this design (no Go call
  per IR call), not on any test; the low-stack subprocess test in
  `ROADMAP.md` 4.2 is supporting evidence only.
- **A call-depth limit,** a fixed number of active frames, `main`'s
  included, defined as a constant in `internal/interp`. It gives overflow a
  defined result and bounds the frame stack's heap memory, which is at most
  the limit times the largest frame of the program (8 bytes per slot and per
  value). Its value is not chosen yet: increment 4.2 is not accepted until
  the value is fixed, recorded here with the heap bound it implies for the
  largest frame in the 4.2 tests, and the 4.2 interpreter tests pass at that
  value.
  **Proposed value (2026-10-29, pending owner approval):** `MaxDepth` =
  100,000 frames. Each frame is an 88-byte entry of the frame stack (a
  function pointer, a program counter, and three slice headers) plus
  8 bytes per parameter, slot, and value, each array rounded up to a Go
  allocation size class. The largest frames run at depth in the 4.2 tests
  are those of `r(n) = r(n+1) + 1` (1 parameter, no slot, 5 values):
  88 + 8 + 48 = 144 bytes, so about 14.4 MB for 100,000 frames, plus the
  frame stack's spare capacity from slice growth. This is computed, not
  measured. `TestStackOverflow` runs at this value: 100,000 frames return
  normally, 100,001 overflow.
  A `call` that would exceed the limit is not executed: the
  interpreter writes `fatal error: stack overflow` and a newline to stderr,
  after any output already written, and returns exit status 2, the native
  program's result (D-009, V-2).

The interpreter's limit counts frames and the native limit counts bytes, so
the two overflow at different depths. Overflowing programs are compared by
one rule, the same in `SEMANTICS.md` V-2 and `ROADMAP.md` Phase 4:

> The rule applies to test programs whose recursion never ends, so that the
> gc-built binary, the Aether-built executable, and the interpreter all
> overflow. The program prints nothing during its recursion. Let D be the
> text `runtime: goroutine stack exceeds`, which begins gc's stack-overflow
> diagnostic. Let P be the exact byte prefix of the gc-built binary's stderr
> before the first occurrence of D. P holds everything the program printed
> before the recursion and is compared exactly; it need not end in a newline,
> because `print` writes none. The gc-built binary's stderr must contain D.
> Its bytes from that occurrence on must begin with the stack-limit line: D,
> one space, one or more ASCII digits, the text `-byte limit`, and a newline.
> After the stack-limit line, the first newline-terminated line that begins
> with `fatal error: ` must exist and must be exactly
> `fatal error: stack overflow`. Beyond P and these checks, gc's stderr is
> not compared: the `runtime: sp=` line, the traceback, and any other lines
> are ignored. The stderr of the Aether-built executable and the stderr of the
> interpreter must each be exactly P followed by `fatal error: stack overflow`
> and one newline, with nothing after it. The gc-built binary, the
> Aether-built executable, and the interpreter must each write nothing to
> stdout and exit with status 2. If any of these checks fails, the test fails.
> A program that overflows in Aether but not under gc shows the V-2 depth
> difference and is not compared with gc by this rule.

The rule fits gc's actual output (E14): gc writes `println` output before
the overflow unbuffered, then its diagnostic, which starts with D. The rule
finds D anywhere rather than at a line start because a program that last
used `print` (4.4) leaves no newline before it. S2 programs print only
integers and `bool`, so P cannot contain D. The rule does not fix gc's limit
in the diagnostic (1e9 bytes in E14), only its form. The
test harness already captures each binary's complete stderr and exit status
(`gcRun` in `cmd/aether/interp_test.go`). `TestStackOverflowAgainstGc`
applies the rule, as `overflowPrefix`, to four programs natively and in the
interpreter (2026-10-29); `TestOverflowPrefix` checks that it rejects
malformed, unrelated, and incomplete gc output.

Increment 4.5 adds panics to the same exit-status result: a panic writes
`panic: runtime error: <message>` and a newline to stderr and exits 2.

**Stages.**

| Increment | IR parts introduced |
|---|---|
| **4.1** | `bool`; `eq`…`ge`, `not`; `jump`, `branch`; invariants 1–4; `println` of `bool`; the interpreter's step budget; runtime helper `aether.printbool` |
| 4.2 | `Params`, `Results`, `param`, single-result `call`, `ret v`; the interpreter's exit-status result and depth limit (stack overflow, exit 2); ABI-S0 (D-011) and the stack amendment (D-009) |
| 4.3 | `tuple`, `extract`, tuple storage |
| 4.4 | sized integer types, canonical-form rules, `conv`, bit operations, `println` of unsigned values (a new helper), `print` of every integer type and `bool` |
| 4.5 | `div`, `rem`, `shl`, `shr`, `panic K`, panic results (exit 2); a runtime panic helper with a stated stack bound |
| 4.6 | `Globals`, `gload`, `gstore`, `Init`; `_start` calls `main.init` |
| 4.7 | `switch` on integers, lowered to the above (no new IR) |

The **4.1 subset** is `bool` locals and constants, comparisons of `int` and
`bool`, `!`, `&&`, `||`, `if`/`else`, the three `for` forms, unlabeled
`break` and `continue`, and `println` of `int` and `bool`. It needs neither
D-011 nor the D-009 stack amendment.

**Alternatives considered.**
1. **SSA with φ-nodes now.** Deferred to Phase 5 (D-006): block-local values
   plus slots need no dominator tree and keep the slot-per-value backend.
2. **Signless types with signed and unsigned operations** (as in LLVM).
   Rejected: Go types carry signedness, and `println` formatting depends on
   it.
3. **Trapping `div`, `rem`, and shift operations** that panic inside the
   operation. Rejected: explicit checks keep every operation total and
   visible in the CFG, and Phase 5 can remove checks it proves unnecessary.
4. **Calls whose results go straight into slots.** Rejected: values stay
   values, and `tuple` plus `extract` is the established form (`go/ssa` uses
   it).
5. **Package initialization inlined into `main`.** Rejected: `main` can be
   called recursively.

**Consequences.**
- The verifier gains a dataflow analysis and a CFG reachability check.
- Lowering removes dead code and emits explicit panic checks.
- The runtime gains `aether.printbool` (4.1), later an unsigned printer and a
  panic helper, each with a stated stack bound (D-011).
- The roadmap's prerequisite moves: D-011 is required before 4.2, not 4.1.

**Evidence (E13).** `go/types` records `true`, `1 < 2`, `x < 4`, `!b`, and
`b && x > 0` passed to `println` as type `bool`, with constant values where
the expression is constant. gc prints `println(true, 3 < 2)` as
`true false`, wraps `int8` 127 + 1 to −128, gives `MinInt64 / −1 = MinInt64`
and `MinInt64 % −1 = 0`, and panics with `panic: runtime error: integer
divide by zero` and `panic: runtime error: negative shift amount` (exit 2).
No Phase 4 code has been written.

**Required tests.** Listed per increment in `ROADMAP.md` Phase 4.

**Revisit when.** Phase 5 introduces SSA (D-006); a construct in S2 cannot be
lowered to this form; or programs become multi-file, which would need a file
in every position.

---

## Phase 0 research notes

Experiments E1–E12 ran in a scratch directory outside the repository on
2026-10-24, on Ubuntu 24.04 (WSL2, kernel 6.6.87), go1.26.1 linux/amd64,
GNU as/ld 2.42. Later experiments ran on the same host with the same tools,
on the dates given in their own notes (E13: 2026-10-25 and 2026-10-26;
E14: 2026-10-27).

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

Experiments E4–E6 ran on 2026-10-24 during the owner review, in the same
environment.

- **E4: S1 case catalog against gc.** Each S1 program listed in
  `docs/SEMANTICS.md` §Subset S1, verified cases, was built with
  `go build` (go1.26.1) and executed. Results:
  - every program exited 0
  - every program wrote nothing to stdout
  - all output went to stderr
  - the expected outputs recorded in `SEMANTICS.md` are the observed bytes
- **E5: constant expressions versus run-time arithmetic.** `go build` rejected
  each of the following, while the equivalent run-time arithmetic on variables
  wrapped in E4:
  - `x := 9223372036854775807 + 1` → "(untyped int constant
    9223372036854775808) … (overflows)"
  - `const c int = 9223372036854775807; x := c + 1` → "c + 1 (constant
    9223372036854775808 of type int) overflows int"
  - `const c = -9223372036854775807 - 2; x := c` → "(untyped int constant
    -9223372036854775809) … (overflows)"

  An untyped constant whose *intermediate* value exceeds `int64`
  (`9223372036854775807*4 - 3*9223372036854775807`) was accepted, and its
  exact value was printed (E4 case S1-05).
- **E6: binutils reproducibility and identity.** The same `.s` input was
  assembled and linked:
  - in two directories with different absolute paths
  - under two different input file names

  Both pairs of executables were byte-identical. There was no `FILE` symbol
  (the input had no `.file` directive), no `.note`/build-id section, and no
  `.comment` section, so `ld` adds no build-id by default here.
  `as --version` printed `GNU assembler (GNU Binutils for Ubuntu) 2.42`, and
  `ld -v` printed `GNU ld (GNU Binutils for Ubuntu) 2.42`.

Experiments E7–E11 ran on 2026-10-24 during the pre-Phase 1 audit, in the same
environment.

- **E7: gc run-time division and `println` behavior.** gc-built binaries,
  run directly:
  - **Division.** With `lo := MinInt64; m1 := -1`, `println(lo/m1, lo%m1)`
    printed `-9223372036854775808 0` and exited 0, so there was no panic.
    `7/2, -7/2, 7/-2, -7/-2` printed `3 -3 -3 3`, and
    `7%2, -7%2, 7%-2, -7%-2` printed `1 -1 1 -1`.
  - **Division by zero.** `z := 0; println(1, 1/z)` printed only the panic
    message (`panic: runtime error: integer divide by zero`) and exited 2.
    Nothing from `println` appeared, so all arguments are evaluated before any
    output. `5 % z` gave the same panic message and exit 2.
  - **Write errors.** The S1-03 program run with stderr closed (`2>&-`) or
    redirected to `/dev/full` exited 0.
- **E8: `go/types` probes** (`GoVersion "go1.26"`, gc/amd64 sizes, no
  importer):
  - **Program-level rules.**
    - `package main` with no declarations: no error.
    - `package foo` with `func main`: no error.
    - `var main = 1`: "cannot declare main - must be func".
    - `func main() int`: "func main must have no arguments and no return
      values".
    - `func init() {}`: no error.
  - **Imports.** `import "fmt"` without an importer: "could not import fmt
    (Config.Importer not installed)".
  - **`println` argument types.** `println(0, -0, x, 1+2, 'a', 1.0)` recorded
    argument types `int int int int rune float64`.
    `println(9223372036854775808)`: "… (overflows)".
  - **Float literals as `int`.**
    - `var x int = 1.0`: the literal is recorded as type `int`, kind `Int`,
      value 1.
    - `const c = 2.5 * 2` used as `int`: type `int`, kind `Int`, value 5.
  - **Redeclaration.** In `a, b := 1, 2; a, c := 3, 4`, the second `a` appears
    in `Info.Uses` and not in `Info.Defs`; `c` appears in `Defs`. gc prints
    `3 2 4` (case S1-10).
- **E9: gc program rules, process dependencies, tool environment, runtime
  file layout.**
  - **Missing `main`.** `go build` of `package main` with no `main` failed
    (exit 1): "function main is undeclared in the main package".
  - **`os/exec` dependencies.** `go list -deps` showed that `os/exec` is a
    transitive dependency of `go/importer` and `go/build`. It is not a
    dependency of `go/parser`, `go/ast`, `go/token`, `go/types`,
    `go/constant`, `go/format`, `embed`, `os`, or `path/filepath`.
  - **Tool environment and output names.** `as` and `ld` ran correctly under
    `env -i LC_ALL=C` (an otherwise empty environment). Linking the same
    object to two different output file names gave byte-identical
    executables. `/usr/bin/as` and `/usr/bin/ld` are symlinks to real ELF
    binaries (`x86_64-linux-gnu-as`, `x86_64-linux-gnu-ld.bfd`).
  - **Runtime file layout.** In a scratch module, a GAS-syntax file
    `internal/amd64/rt/start.s` (a subdirectory with no Go files), embedded
    from package `internal/amd64` with `//go:embed rt/*.s`, worked with
    `go build ./...`, `go vet ./...`, and `go test ./...`. The same file placed
    directly in the Go package directory made `go build` fail, because Go's own
    assembler rejected it.
- **E10: x86-64 behavior the backend must work around.** Static programs
  assembled and linked with GNU `as`/`ld`:
  - `idivq` with `rax = MinInt64` (sign-extended by `cqto`) and divisor `-1`
    killed the process with SIGFPE ("Floating point exception", shell exit
    status 136 = 128 + 8).
  - `shlq %cl, %rdi` with `rdi = 5` and `cl = 64` left `rdi` unchanged (exit
    status 5). The shift count is masked, so the hardware does not produce
    Go's result for a count of 64, which is 0.
  - These all produced exactly the results gc produced for the same Go
    expressions (exit status 0 from a self-checking program):
    - `negq` of `MinInt64` gives `MinInt64`
    - `imulq` of 3037000500 by itself gives `-9223372036709301616` (the low
      64 bits; case S1-04)
    - `addq $1` to `MaxInt64` gives `MinInt64`
- **E11: `go/parser` comment retention.** For a file containing
  `//go:build linux` and `//go:noinline`:
  - mode `SkipObjectResolution` alone retained 0 comments
  - mode `ParseComments | SkipObjectResolution` retained both

E12 was gathered during Phase 1 implementation on 2026-10-24, in the same
environment. Unlike E1–E11, its findings came from the repository's own tests.

- **E12: classification edge cases found by Phase 1 testing.**
  - **`func main() ()`.** gc accepts an empty result list on `main` (exit 0),
    as the spec does, but `go/types` reports "func main must have no arguments
    and no return values". `go/types` (`resolver.go`) tests the result list
    for nil, while gc's `types2` tests its length. The gate's pre-check
    therefore rejects an empty result list on `main` or `init` as
    `unsupported` (`empty-result-list`), so valid Go is not classified as
    invalid.
  - **`func main()` without a body.** The spec allows a body-less function
    declaration ("provides the signature for a function implemented outside
    Go"), and `go/types` accepts it. gc reports "missing function body"
    because no assembly is supplied. Aether classifies it as `unsupported`
    (`function-without-body`). This was found by fuzzing: the gate had treated
    it as impossible and reported an `internal` error.
  - **Uses of package-level declarations.** Package-level `var` and `const`
    declarations are rejected, but they still type-check, so `main` can refer
    to them. The GOROOT sweep found three programs (`func5.go`, `method7.go`,
    `simassign.go`) where the gate reported such references as `internal`.
    They are now `unsupported` (`package-level-declaration`).
  - **`var main = 1`.** `go/types` reports "cannot declare main - must be func"
    and leaves `main` out of the package scope. The program-level "main is
    undeclared" check therefore runs only when type checking succeeded.
  - **Valid Go confirmed with gc.** Every corpus case expected to be accepted
    or `unsupported` was built with gc (go1.26.1) and is valid Go, except
    where its `// aether:gc` header records otherwise. These forms are valid
    Go that S1 rejects:
    - `var x int = 0i`, `var x int = 'a'`
    - `const ( a = 1; b )`
    - `(x) = 2`
    - `var _ = 1`
    - `select {}`
    - `for range 3 {}`
    - `package lib` with or without `func main`

    The cgo case is not built.

- **E13: Phase 4 design gate (2026-10-25 and 2026-10-26).** Run outside
  the repository, on the same host (overcommit mode 0, 11.5 GiB RAM, 3 GiB
  swap).
  - **ABI-S0 prototype** (hand-written assembly): the results under D-011.
  - **`mmap` flags.** The committed runtime passes `0x20022`, which `strace`
    decodes as `MAP_PRIVATE|MAP_ANONYMOUS|MAP_STACK`; the prototype passed
    `0x24022`, which adds `MAP_NORESERVE`. Both values match the kernel
    headers and Go's `syscall` constants.
  - **Overcommit mode 0.** An ordinary private anonymous mapping of 14 GiB
    succeeded and 16 GiB failed with `ENOMEM`; with `MAP_NORESERVE`, 1024 GiB
    succeeded. `/proc/<pid>/smaps` showed `VmFlags` `ac` for the first and
    `nr` for the second, and showed that the kernel can merge such a mapping
    with an adjacent one.
  - **gc behavior** (go1.26.1): the results under D-023; unbounded recursion
    prints `runtime: goroutine stack exceeds 1000000000-byte limit`, then
    `fatal error: stack overflow` and a traceback, and exits 2; a recursive
    call of `main()` builds and runs.
  - **`print` format** (gc, 2026-10-26): a program with
    `var u uint8 = 255`, `var i8 int8 = -128`, and
    `print(true, -1, false, u, i8); println(); print(); println(true, u, i8)`
    wrote exactly `true-1false255-128\ntrue 255 -128\n` to stderr, nothing to
    stdout, and exited 0.
- **E14: gc's stack-overflow output (2026-10-27).** Same host and tools. A
  program printing `println(1, 2)` and `println(true)`, then calling
  `println(r(0))` where `r(n)` returns `r(n+1) + 1`, built with gc (go1.26.1):
  stderr began with exactly `1 2\ntrue\n`, followed by
  `runtime: goroutine stack exceeds 1000000000-byte limit`, a `runtime: sp=…`
  line, and `fatal error: stack overflow` (its fifth line), then a
  traceback; stdout was empty; exit status 2. The run took about 4 s with a
  maximum resident set of about 1 GB, so each such gc program is expensive.

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
the Intel SDM (instruction encoding is delegated to GNU `as` per D-008); gc runtime GC and
stack-growth source in detail (deferred to D-010 and D-011).
