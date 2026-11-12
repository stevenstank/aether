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
which also holds the later experiments. Benchmarks were first measured on
2026-11-05, the register-allocation baseline
(`benchmarks/2026-11-05-regalloc/REPORT.md`). Only D-026 and D-027 cite them.

## Summary

| ID | Title | Status |
|----|-------|--------|
| D-001 | Reuse `go/parser`, `go/ast`, `go/token` for the initial frontend | Accepted |
| D-002 | Reuse `go/types` and `go/constant` for initial type checking | Accepted |
| D-003 | Explicit subset gate and diagnostic classification | Accepted |
| D-004 | Aether-owned typed CFG IR, non-SSA at first | Accepted |
| D-005 | Reference interpreter for the IR | Accepted |
| D-006 | SSA timing and construction method | Accepted (2026-11-05, after the resolving experiment) |
| D-007 | Executable path: GNU assembler text, `as`, `ld` | Accepted |
| D-008 | GNU `as`/`ld` as the long-term encoding and linking path; no in-house encoder, ELF writer, or linker without a concrete requirement | Accepted |
| D-009 | Aether-owned minimal runtime; no gc runtime, no libc | Accepted (early phases only; Phase 4 stack amended 2026-10-26) |
| D-010 | Memory management and garbage collection | Proposed |
| D-011 | Calling convention for Aether-compiled functions | Accepted (ABI-S0, 2026-10-26); since D-031 the convention of `aether build -O0` only |
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
| D-023 | The S2 IR extension, introduced in stages | Accepted (staged, 2026-10-26; `MaxDepth` and `Init` rules 2026-11-03) |
| D-024 | Strict left-to-right evaluation order | Accepted (2026-11-03) |
| D-025 | The SSA form of the IR: φ and the SSA-level invariants | Accepted (2026-11-05) |
| D-026 | Register-allocation experiment RA-0 | Accepted (2026-11-06, owner approval; experiment complete, criteria met; shipped by D-027) |
| D-027 | `aether build` promotes to SSA and allocates registers | Accepted (2026-11-06, owner direction; its "no flag" clause superseded by D-028) |
| D-028 | `aether build -O0` selects the slot-based backend | Accepted (2026-11-06, owner direction) |
| D-029 | Instruction selection for comparisons, branches, and constants | Accepted (2026-11-06, owner direction; accepted as the default with one unresolved regression, b08_ackermann) |
| D-030 | Experiment ABI-R1, a register calling convention for internal calls | Accepted (2026-11-06, owner approval; experiment complete, all criteria met; shipped by D-031) |
| D-031 | `aether build` uses ABI-R1; `-O0` keeps ABI-S0 | Accepted (2026-11-07, owner approval) |
| D-032 | Floating point in the IR and the runtime (Phase 6 increment 6.1) | Accepted (2026-11-09, owner approval) |
| D-033 | String constants in the IR and the runtime (Phase 6 increment 6.2) | Accepted (2026-11-10, owner approval) |

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
| 2026-11-03 | Owner Phase 4 review: Phase 4 not accepted yet (conditions in `ROADMAP.md` Phase 4); `MaxDepth` = 100,000 approved and D-023's memory figures corrected; V-4 order retained as D-024; D-006 slot-promotion experiment authorized, conditional on Phase 4 acceptance | D-006, D-023, D-024 |
| 2026-11-03 | Owner final Phase 4 review: Phase 4 accepted; the 29 automated negative controls accepted; FI-1 closed as investigated, unexplained, and not reproduced (`TESTING.md`); D-023's `Init` rules approved; the D-006 experiment may start | D-006, D-023 |
| 2026-11-04 | Owner review of the D-006 experiment plan: part 1 (dominator analysis) accepted; SSA in `internal/ir`, marked per function; φ operands tied to explicit incoming edges; pruned SSA with no `undef`; D-025 created for the SSA contract | D-006, D-025 |
| 2026-11-05 | Owner review of part 2: the implemented SSA contract accepted as D-025, including parallel edges and SSA functions without φ; `ARCHITECTURE.md` §4 invariant 8 corrected to one operand per incoming edge | D-006, D-025 |
| 2026-11-05 | Owner closes the D-006 experiment after parts 3 and 4: D-006 accepted with the resolution recorded in its entry; the planted φ-lowering bugs are not added to the negative controls, since the permanent tests detect them | D-006 |
| 2026-11-06 | Register-allocation baseline and benchmark harness checked in (`BENCHMARKS.md`); D-026 created as Proposed. No owner decision yet. | D-026 |
| 2026-11-06 | Owner approves D-026: experiment RA-0 may start, under the plan, criteria, and thresholds recorded in the entry | D-026 |
| 2026-11-06 | Owner directs that RA-0 ship to `aether build` after its results; D-027 created | D-026, D-027 |
| 2026-11-06 | Owner directs that `aether build` gain `-O0`; D-028 created | D-027, D-028 |
| 2026-11-06 | Owner directs that the roadmap stop showing Phases 1 and 2 as pending review; their acceptance is recorded in `ROADMAP.md`. No earlier record of it exists. | — |
| 2026-11-06 | Owner accepts instruction selection as the default; the b08_ackermann regression is recorded as unresolved, and block alignment stays out of scope | D-029 |
| 2026-11-06 | Owner approves experiment ABI-R1 under the scope and criteria recorded in D-030. Shipping needs a new entry. No code exists yet. | D-011, D-030 |
| 2026-11-07 | Owner settles three D-030 criteria: frame sizes may shrink while the stack check's mechanism, frame chain, and epilogue stay unchanged; the 2% limit applies to aggregates, with per-offset results reported and significant outliers investigated; peak stack stays within the `isel` baseline, and b03 and b04 stay byte-identical only if they avoid ABI-R1's call paths. Implementation had not started. | D-030 |
| 2026-11-07 | Owner approves ABI-R1 as the default convention of `aether build` after the D-030 criteria were met; `-O0` keeps ABI-S0. D-031 created. | D-011, D-028, D-030, D-031 |
| 2026-11-09 | Phase 6 started with floating point; D-032 created as Proposed. Only its runtime formatter exists, with tests; no IR or gate change yet. | D-011, D-032 |
| 2026-11-09 | Owner accepts D-032 as proposed, with its three constraints | D-032 |
| 2026-11-09 | Phase 6.2 started with string constants; D-033 created as Proposed. Its IR, interpreter, backend, and runtime parts exist with tests; the gate does not accept strings yet. | D-033 |
| 2026-11-10 | Owner accepts D-033 | D-033 |

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

**Status:** Accepted (2026-11-05). The owner closed the resolving
experiment (parts 1–4) and accepted the resolution below.

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

**Owner review (2026-11-03).** The status stays Proposed. The owner
authorized the resolving experiment as the first Phase 5 work, starting only
after Phase 4 is accepted (accepted 2026-11-03, so it may start):
- It runs on the existing load/store IR and covers dominators, φ placement,
  renaming, and the verifier invariants of the SSA form.
- The construction algorithm, and whether SSA lives in the same IR package
  or a second one, stay open until the experiment gives evidence.
- No register allocation is part of it.

**Experiment record.** D-006 was Proposed throughout parts 1–4.
- Part 1 (2026-11-04): the dominator tree and dominance frontiers
  (`ir.Dominators`, `internal/ir/dom.go`). It is an analysis over the
  current IR and changes no contract. Accepted by the owner 2026-11-04.
- Owner decisions on the plan (2026-11-04): the SSA form lives in
  `internal/ir` and is marked per function; φ operands name their incoming
  edges explicitly; SSA is pruned, with no `undef`. The SSA contract is
  D-025; this entry stays the experiment decision.
- Part 2 (2026-11-04): the `phi` instruction, its printed form, and the
  SSA-level verifier of D-025, with hand-built tests. No slot promotion
  yet; lowering, the interpreter, and the backend are unchanged. The owner
  accepted D-025 on 2026-11-05. D-006 itself stayed Proposed.
- Part 3 (2026-11-05): `ir.Promote` converts an ordinary function to
  pruned SSA. It computes slot liveness, places φs in the iterated
  dominance frontiers (Cytron et al.) where the slot is live, and renames
  over the dominator tree. The construction is the dominance-frontier
  algorithm on the load/store IR, after lowering, which is unchanged. The
  interpreter executes φ as D-025 specifies.

  On the S2 suite the interpreter gives identical stderr and exit status
  before and after promotion, and the promoted IR verifies
  (`TestPromoteSuite`). The suite has 239 programs: 34 corpus `run`
  programs, 200 generated, and 5 that overflow the stack. It has 418
  functions and 9,351 slots: promotion removed 22,065 loads and 12,652
  stores, added 2,578 φs, and took IR instructions from 89,116 to 56,977.
  These are static IR counts, not machine instructions.

  `FuzzGenerated` now checks the same equivalence. The pass is
  `internal/ir/promote.go`, 221 lines.
- Part 4 (2026-11-05): the amd64 backend consumes SSA functions directly,
  with no machine IR. A φ has a frame cell like any value and emits no
  code. Each edge into a block with φs copies the operands for that edge
  into the φs' cells as one parallel copy: a copy is emitted once no
  remaining copy reads its destination, and a cycle is broken by saving
  one destination in `%rcx`. A `branch` puts each edge's copies only on
  the path for that edge: after the conditional jump, or under a label of
  its own (`.L<f>.b<n>.1`) when both targets have φs. Critical and
  parallel edges are thereby split in the emitted code; the CFG is not
  changed. Value cells are numbered by rank of value number, so the gaps
  promotion leaves take no frame space. For ordinary functions, whose
  numbering is dense, this is the same layout, and the slot-based assembly
  is byte-identical to that of the previous backend on all 384 programs
  compared (the corpus `run` programs, generated seeds 1–600, and the
  overflow programs). The change is in `internal/amd64/amd64.go`.

  Tests:
  - `internal/amd64/ssa_test.go`, run natively and in the interpreter:
    - a loop header whose φs swap, rotate three ways, fan out from an
      overwritten φ, and keep their own value;
    - every `branch` shape: φs only at the true target, only at the false
      target, at both, and parallel edges to one block, along critical
      edges;
    - a loop that exits from the block its back edge assigns.
  - `TestNativeSSASuite` builds every S2-suite program (239) twice, as
    lowered and with every function promoted. Both executables and the
    interpreter give the same stderr and exit status.
  - Six bugs were planted in the φ lowering:
    - Four were detected: sequential copies with no cycle breaking,
      operands looked up per predecessor instead of per edge, the wrong
      edge's copies, and a dropped copy.
    - Copies made before the branch was decided were detected only after
      the loop-exit test was added for it.
    - Saving a cycle's source instead of its destination is equivalent,
      since in a cycle every source is a destination.

  **Measurement.** `go test ./cmd/aether -run TestNativeSSASuite -v`
  (setting `AETHER_SSA_REPORT=FILE` also writes per-function figures).
  - Same programs, same Aether build, same flags, and the same runtime.
  - Promotion is the only difference.
  - Environment: go1.26.1, GNU as and ld 2.42, WSL2.
  - Instructions are counted in the emitted assembly; code bytes are the
    sizes of the `main.*` function symbols in the linked executable.

  On 547 functions (418 compiled functions plus 129 empty `main.init`
  stubs):

  | Static measure | Slot-based | SSA | Change |
  |---|---|---|---|
  | Machine instructions | 234,359 | 176,641 | −24.6% |
  | Code bytes | 1,419,429 | 1,034,373 | −27.1% |
  | Frame bytes, summed over functions | 563,520 | 332,928 | −40.9% |
  | Largest frame | 17,200 | 10,480 | −39.1% |
  | `aether.mainstack`, summed over programs | 545,056 | 324,144 | −40.5% |

  - No function is larger under SSA by any of these measures. 287
    functions are smaller, and 260 are unchanged: the stubs and the
    functions without slots.
  - Per function with more than one instruction, the ratio of SSA to
    slot-based instructions ranges from 0.48 to 1.00, with median 0.81.
  - No cycle of φ copies occurs in the suite (no `%rcx` save), so cycles
    are covered only by the hand-built tests.
  - These are static counts. Run time was not measured, and no run-time
    improvement is claimed. The stack used at run time scales with the
    frame sizes of the active calls; it was not measured.

**Resolution (design note, accepted 2026-11-05).**
- **Timing.** SSA arrives in Phase 5, after control flow and functions work
  natively, as proposed.
- **Construction.** SSA is built after lowering, by promoting the slots of
  the load/store IR (`ir.Promote`). The method is Cytron et al.: dominance
  frontiers (Cooper–Harvey–Kennedy dominators), φ placement in the
  iterated frontiers, pruned by slot liveness, and renaming over the
  dominator tree. Lowering is unchanged. Construction during lowering was
  not needed.
- **One IR package.** Both forms share `internal/ir`: the instruction set,
  printer, verifier, interpreter, and backend. They are distinguished by
  `Func.SSA`; D-025 is the SSA contract. No case needed a different data
  structure.
- **Backend input.** The current backend consumes SSA IR directly. Whether
  register allocation needs a separate machine IR is not decided here; it
  belongs to the register-allocation work.

**Final results.**
- Part 1: the dominator tree and dominance frontiers.
- Part 2: `phi` with explicit incoming edges, and the SSA verifier
  (D-025).
- Part 3: promotion and φ execution in the interpreter. Identical
  interpreter results on the 239 S2-suite programs.
  - IR instructions went from 89,116 to 56,977: 22,065 loads and 12,652
    stores removed, 2,578 φs added.
- Part 4: φ lowering in the backend. On the same suite, the slot-based
  and SSA executables and the interpreter agree, and the slot-based
  assembly is unchanged.
  - The static figures are in the table under part 4: 24.6% fewer machine
    instructions, 27.1% fewer code bytes, 40.9% fewer frame bytes, and no
    function larger under SSA.

**Measurement methodology.** The static method of part 4 is kept as the
method for later comparisons:
- the same programs, Aether build, flags, runtime, and tools;
- instructions counted in the emitted assembly;
- code bytes from the function symbols of the linked executable;
- frame sizes from each prologue;
- the environment recorded.

**Run-time performance and run-time stack usage were not measured.** The
figures are static counts, and no run-time improvement is claimed from them.

**Remaining limitations.**
- **No register allocation.** The SSA backend is as naive as the
  slot-based one. Every value, including each φ, is in memory, and the
  parallel copies go through `%rax` and `%rcx`. A φ copy runs on every
  edge into its block, even when the value is unchanged along that edge,
  except where the operand is the φ itself.
- **SSA is test-only.** Nothing in `aether build` or `aether check`
  promotes, and no flag selects it. Built executables are those of the
  slot-based path. `FuzzGenerated` checks promotion in the interpreter
  only, not natively.
- **φ cycles are covered only by hand-built tests.** No S2-suite program
  produces a cycle of φ copies, so the `%rcx` cycle breaking is exercised
  only by `TestNativePhiCopies`.
- There are no corpus golden files for SSA IR or SSA assembly.

**Negative controls.** The planted φ-lowering bugs were not added to the
controls of `TESTING.md`. Each one that changes behavior is detected by a
permanent test: `TestNativePhiCopies`, `TestNativePhiEdges`,
`TestNativePhiLoopExit`, or `TestNativeSSASuite`. The one bug the first
tests missed, copies made before a branch is decided, led to
`TestNativePhiLoopExit`.

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
gate, E13): option 3, as the stack-based convention **ABI-S0**. Since D-031,
ABI-S0 is the convention of `aether build -O0` only; the default build uses
ABI-R1 (D-030), which keeps ABI-S0's frame chain, stack check, and caller-saved
registers. It is required
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
    `int8`, E13 (`true-1false255-128`). The other types (`int16`, `int32`,
    `uint16`, `uint32`, `uint64`, at their extremes) were established in
    increment 4.4 (2026-10-29): `s2_i02_conversions_print` prints each with
    `print` and `println`, and the interpreter's and the executable's
    output equal gc's in `TestDifferentialAgainstGc`: decimal, signed by
    type, as `println` prints them.
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
  **`Init` rules (approved by the owner 2026-11-03):**
  - A program has an `Init` only when a package-level variable has an
    initializer (`types.Info.InitOrder` is not empty). Zero-valued globals
    and constants do not by themselves require one.
  - A `call` of `Init` is rejected by the verifier, and lowering never
    emits one.
  - When present, `Init` runs before `main`, initializing in `InitOrder`.
    A panic or stack overflow during initialization ends the program before
    `main` is entered as the program's entry.
  - An initializer may call `main` (Go forbids referring to `init`, not to
    `main`).
  - That the backend emits a `main.init` that only returns when there is no
    `Init` is an implementation detail, not part of this contract.
  Tests: `TestVerifyGlobals`, `TestLowerInit`, `TestGlobalsAndInit`,
  `TestPanicInInit`, `TestNativeGlobals`, `s2_g02_init_order`,
  `s2_g03_init_panic`, `s2_g05_init_calls_main`, and the `init` program of
  `TestStackOverflowAgainstGc`.
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
  **Value (proposed 2026-10-29, approved by the owner 2026-11-03):**
  `MaxDepth` = 100,000 frames. Each frame is a 104-byte entry of the frame
  stack (`unsafe.Sizeof` of the current `frame` struct: function and block
  pointers, a program counter, three slice headers, and the tuple map) plus
  8 bytes per parameter, slot, and value, each array rounded up to a Go
  allocation size class. The largest frames run at depth in the 4.2 tests
  are those of `r(n) = r(n+1) + 1` (1 parameter, no slot, 5 values):
  104 + 8 + 48 = 160 bytes, so about 16 MB for 100,000 frames, plus the
  frame stack's spare capacity from slice growth. This is a computed
  estimate for that frame configuration only, not a maximum for every
  program: a frame with more slots or values costs more. Measured
  separately (2026-11-01, memory profile of `TestStackOverflow`, three
  subtests): 172.8 MB of **cumulative** allocations in `interp.run`,
  counting every intermediate frame-stack array; this is total allocation,
  not memory in use at once. **Peak memory (RSS or live heap) has not been
  measured.** `TestStackOverflow` runs at this value: 100,000 frames return
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

## D-024: Strict left-to-right evaluation order

**Status:** Accepted (2026-11-03, owner decision on divergence V-4)

**Context.** The spec leaves unspecified the order of calls relative to
other operands (§Order of evaluation). Aether's lowering evaluates operands
strictly left to right. gc (go1.26.1, observed 2026-11-01) evaluates every
call of a statement first and the remaining operands after them, so a
global read before a call that assigns it, and a division or shift that
panics before a call, behave differently (`SEMANTICS.md` §Evaluation order,
V-4).

**Decision.**
- Lowering keeps evaluating operands strictly left to right, with side
  effects (calls, panics, loads of globals) in that order in the IR. `&&`
  and `||` short-circuit as the spec requires. Lowering is not changed to
  match gc.
- The order in the IR is a contract: no later pass, including every Phase 5
  optimization, may reorder a call, a panic check, or a global load or
  store relative to another.
- V-4 remains a documented divergence. Differential tests must not depend
  on it (`TESTING.md`).

**Alternatives.** Match gc's calls-first order. Rejected: that order is gc's
implementation behavior, not a spec guarantee, and could change between
gc versions; a fixed left-to-right order is simple to state and to check.

**Consequences.**
- Differential tests keep avoiding statements that combine a call with an
  operand the call can change or with a panicking operation.
- `TestGlobalReadOrder` pins the order without gc. Phase 5 passes must
  preserve it, checked by the existing suites at every optimization level.

**Revisit when.** The spec defines the order, or a later subset needs
gc-identical output for these statements.

---

## D-025: The SSA form of the IR: φ and the SSA-level invariants

**Status:** Accepted (2026-11-05, owner approval of the implemented
contract). The representation choices were directed by the owner on
2026-11-04. It extends D-021 and D-023 for functions in SSA form only.
Ordinary functions and their contract are unchanged. The experiment
context is D-006, accepted on 2026-11-05 when the experiment closed.

The owner's approval covers these points, all stated below:
- SSA functions share `internal/ir` with ordinary IR, told apart by
  `Func.SSA`.
- φ operands name explicit incoming CFG edges, `ir.Edge{From, Index}`,
  ordered by source block and then target index. Parallel edges remain
  supported.
- SSA is pruned, with no `undef`.
- Definitions are unique per function. Ordinary uses require the
  definition to dominate them; a φ operand requires dominance at its
  incoming edge.
- φs come first, are forbidden in the entry block, and cover every
  incoming edge exactly once.
- SSA functions have no slots, loads, or stores.
- An SSA function without φ is valid.

**Context.** The D-006 experiment promotes slots to SSA values. That needs
values that cross blocks, and φ to merge them where control flow joins.
Both change the IR contract (`ARCHITECTURE.md` §4).

**Decision.**
- **One package, two forms.** `ir.Func.SSA` marks a function as being in
  SSA form. A program may mix forms. The printer writes `ssa` after the
  function's signature. `Verify` applies the rules below to SSA
  functions, and every other rule to every function.
- **Edges.** An edge is a block and a target index of its terminator,
  `ir.Edge{From, Index}`, printed `bN.I`. Parallel edges can be represented:
  a `branch` may name the same block as both targets (the verifier does not
  forbid it, though no lowered corpus program contains one). Such a branch
  makes two distinct edges. A φ therefore takes one value **per incoming
  edge**, not per predecessor block.
- **`phi`.** `vN = phi T, bA.I: vX, bB.J: vY` produces the value given
  for the edge along which control entered its block. `Args[k]` is the
  value for `Edges[k]`. All φs of a block take their values together on
  entry, reading each operand as it was at the end of the edge's source
  block, before any of them is written. `phi` is not a terminator and has
  no other operands.
- **Pruned SSA, no `undef`.** The promotion pass (part 3) places a φ only
  where its slot is live. The ordinary form's definite-assignment rule
  (D-023: every load is preceded by a store on every path) then guarantees
  a value on every incoming edge, so the IR has no undefined value.

**Invariants of an SSA function.** `Verify` checks each of these, in
addition to the ordinary invariants (types, operand counts, terminators,
reachability, positions, calls, globals, and panics):
1. **No slots.** `Slots` is empty, and there are no `load` or `store`
   instructions.
2. **Single definition.** Value numbers are unique in the whole function
   (in ordinary functions, uniqueness within a block is what is checked),
   and no instruction appears twice.
3. **Dominance of uses.** Each operand of an instruction other than `phi`
   is an instruction of the same function. It must be defined either
   earlier in the same block or in a block that strictly dominates the
   use's block. A tuple (a multi-result `call`) is used only by `extract`,
   in the call's block.
4. **φ placement.** φs come before every other instruction of their
   block. The entry block has no φ.
5. **Incoming-edge completeness.** A φ's `Edges` are exactly the edges into
   its block from reachable blocks, each listed once. They are in canonical
   order: by source block ID, then by target index. An edge from a block
   that is not a predecessor, an index whose target is a different block, a
   repeated edge, and a missing edge are all errors. Matching only the set
   of predecessor blocks would not be enough when edges are parallel.
6. **Edge-wise availability.** The value for edge `bP.I` must be defined in
   a block that dominates `bP`, so it is available at the end of `bP` on
   every path. It may be a φ of the same block (for a loop), and it may be
   the φ itself.
7. **Type consistency.** A φ's type is `bool` or an integer type, and its
   number of values equals its number of edges. Every value has the φ's
   type.
8. **Entry rules.** No edge enters the entry block, so the entry has no
   predecessors, and `param` stays in the entry block.
9. Only `phi` has `Edges`, and a `phi` appears only in an SSA function.

An SSA function needs no φ. Values used across blocks by dominance alone
are valid. The interpreter and the backend execute them, because each keeps
one value per value number, or one cell per value, for the whole call
(`TestSSACrossBlockValues`).

If the CFG cannot be analyzed (a target that is not a block of the
function, or targets on a terminator other than `jump` or `branch`), those
errors are reported and the dominance checks (3, 5, 6, 8) are skipped.

**Execution (D-006 parts 3 and 4).** The interpreter executes φ as specified
above. The amd64 backend gives each φ a frame cell and makes the copies
for each edge as one parallel copy on that edge's path, with no change to
the CFG (D-006 part 4). Neither needed a contract change. Lowering produces
only ordinary functions, so SSA IR reaches either one only from tests.

**Alternatives considered.**
1. **φ operands matched to predecessor order.** Rejected by the owner: the
   meaning would depend on an order derived from the CFG, which a CFG edit
   silently changes.
2. **One operand per predecessor block, forbidding parallel edges in SSA.**
   It is sound, but it would make promotion rewrite the CFG. Explicit edges
   cost one index per operand.
3. **A second IR package for SSA.** Rejected by the owner: it would mean a
   second printer, verifier, and interpreter for a shared instruction set.
4. **Minimal SSA with `undef`.** Rejected by the owner: a new value with no
   defined meaning, which pruning makes unnecessary.

**Consequences.**
- Part 3 must produce φs in canonical edge order, and pruned.
- The rule that values are local to their block (D-023) now holds only in
  ordinary functions.
- `ARCHITECTURE.md` §4 invariant 8 states one operand per incoming edge.

**Evidence and tests.** `internal/ir/ssa_test.go`:
- valid fixtures: a diamond, a critical edge, a loop, nested loops with
  `continue`, a three-predecessor join with a `bool` φ, and parallel edges
- the exact printed form
- a negative test per invariant
- a check that a malformed CFG is reported without a crash
- a mutation sweep: every operand is replaced with every value of its type,
  and every φ edge is replaced, removed, or swapped. The verdicts are
  checked against dominance computed naively from its definition: 309
  mutants valid and 546 rejected, all as predicted.

`internal/amd64/ssa_test.go` (`TestSSACrossBlockValues`): hand-built SSA
functions with no φ, whose values (a parameter, a computed value, and call
results) are used in blocks their definitions dominate. Both arms of a
branch are taken. The interpreter and the native executable print the same
expected output.

Ordinary IR goldens and tests are unchanged.

**Revisit when.** Part 3 or part 4 shows a rule is missing or too strict,
or a later IR extension (memory, address-of) leaves some slots unpromotable.

---

## D-026: Register-allocation experiment RA-0

**Status:** Accepted (2026-11-06, owner approval of the plan, criteria, and
thresholds below; proposed the same day). The experiment is in progress.
The approval covers RA-0 as a test-only experiment. It does not let any
allocator reach built executables, which needs a new entry after the
results. Nothing in it changes built executables, the IR contract, or
ABI-S0 (D-011).

**Context.** The register-allocation baseline
(`benchmarks/2026-11-05-regalloc/REPORT.md`, measured under WSL2 at
`a3b64c7`) found:
- *Measured:* SSA promotion alone cut dynamic instructions by 12–35% and
  CPU time by 14–48% on nine S2 workloads.
- *Measured:* low register pressure. 98.6% of S2-suite functions have at
  most 12 values live at once (constants excluded), the largest has 16, and
  at most 5 values are live across any call.
- *Measured:* about half of dynamic instructions are compare/branch
  sequences, constants, and ABI-S0 call overhead, which allocation alone
  does not remove.
- *Inferred, not measured:* the gap between instruction and time ratios
  comes from store→load chains through stack cells. No hardware counters
  were available.

**Proposal.** A test-only experiment, like D-006, with no change to built
executables:
- Allocate directly on the SSA IR, with the assignment kept in
  `internal/amd64`. No machine IR.
- Greedy coloring in dominator-tree preorder (SSA-based coloring), with
  spill-everywhere for functions whose pressure exceeds the register count.
- 11 allocatable registers: `%rbx`, `%rsi`, `%rdi`, `%r8`–`%r15`. `%rax`,
  `%rcx`, and `%rdx` stay reserved scratch, so the existing division,
  shift, φ-cycle, and prologue sequences are unchanged.
- Values live across any call, and tuples, stay in cells.
- φs become parallel copies between locations.
- An independent allocation checker over the IR liveness and the assignment.

**Proposed acceptance criteria.** All of these, on the checked-in harness
(`BENCHMARKS.md`):
1. The S2 suite and the nine workloads give identical stderr and exit
   status for slot, SSA, RA-0, and the interpreter or gc oracle.
2. The checker passes on every function, also in `FuzzGenerated`, natively.
3. The assembly is deterministic.
4. Permanent tests detect planted bugs: interfering values sharing a
   register, a register value live across a call, a dropped edge copy, a
   wrong cycle break, and the wrong edge's copies.
5. The result is reported whatever it is.

**Proposed thresholds.** Expectations registered before the experiment:
- dynamic memory operations fall by at least 50% relative to SSA on b02,
  b03, b04, b07, and b09;
- CPU time (median of 15 pinned runs) improves by at least 10% over SSA on
  at least 5 of the 9 workloads, with non-overlapping min–max ranges.

Missing them is a recorded negative result. It would redirect Phase 5 work
to instruction selection and the calling convention, not to a more
sophisticated allocator.

**Alternatives.** Linear scan, Chaitin–Briggs coloring, and a separate
machine IR. The report (§5) compares them. Each is reconsidered with data if
the experiment shows spill cost, constraint handling, or the checker needs
them.

**Missing evidence.** Everything the experiment would produce. The model
estimate in the report (0.49–0.72× SSA's dynamic instructions for a full
allocator) is modeled, not measured, and RA-0 is expected to fall well short
of it, since it keeps the accumulator style.

**Experiment record.**
- Part 1 (2026-11-06): `internal/amd64/regalloc.go`: SSA value liveness,
  the allocation (candidates, spill-everywhere, coloring in dominator-tree
  preorder), and `checkAllocation`. The checker tracks register contents
  forward and shares no code with the allocation's liveness. Tests:
  hand-built functions (straight-line, loop, φ swap, calls, diamond, spill)
  and their expected locations; planted bad assignments; and a sweep that
  moves every allocated value to every other register (378 mutants
  accepted, 262 rejected), where the checker's verdict must match
  interference computed from liveness.
- Part 2 (2026-11-06): `amd64.EmitWith(prog, EmitOptions{Registers: true})`
  uses the allocation; `Emit`, which `aether build` uses, is unchanged.
  Results:
  - all 239 S2-suite programs built with registers give the interpreter's
    stderr and exit status (`TestNativeRASuite`);
  - the assembly is deterministic;
  - `FuzzGenerated` also emits with registers, which runs the checker, and
    runs the result natively; 60 s of fuzzing (17,560 executions) found
    nothing.
  - Static figures against SSA without registers: machine instructions
    176,641 → 173,586 (−1.7%, only coinciding φ copies disappear); frame
    bytes 332,928 → 89,472 (−73.1%).
  - The five planted bugs of criterion 4 were each caught by permanent
    tests: shared registers and register values across calls by the
    checker, and the dropped edge copy and wrong cycle break by the native
    tests in both packages. The wrong edge's copies were caught by the φ
    edge tests of `internal/amd64`, not by the S2 suite. The plants were
    temporary and are not part of the tests.
- Part 3 (2026-11-06): measurement with the harness's new `ra` variant,
  under WSL2 (`benchmarks/2026-11-06-ra0/REPORT.md`). All criteria and
  both thresholds are met:
  - dynamic memory operations against SSA fall by 98.4% (b02), 99.8%
    (b03), 50.2% (b04, narrowly: its hot loop needs 17 registers and
    spills), 88.5% (b07), and 92.1% (b09);
  - CPU time against SSA improves by 18–70% on all 9 workloads, with
    non-overlapping ranges (median ra/ssa 0.299–0.823);
  - dynamic instructions change by only 0–14%, since selection is
    unchanged. *Inferred, not measured:* memory dependency chains through
    cells were the dominant cost, as the baseline inferred.
  - Control: the slot and SSA measurements are byte-identical to the
    baseline's.

**Resolving experiment.** RA-0 itself, approved and completed 2026-11-06.
The owner decided from its results that RA-0 reaches built executables:
D-027.

---

## D-027: `aether build` promotes to SSA and allocates registers

**Status:** Accepted (2026-11-06, owner direction to ship RA-0 to
`aether build`, after the results of D-026).

**Context.** RA-0 met every criterion and threshold of D-026
(`benchmarks/2026-11-06-ra0/REPORT.md`). It allocates only SSA functions, so
shipping it also ships slot promotion (D-006), which until now ran only in
tests.

**Decision.**
- `aether build` compiles as before, then `emitProgram` (`cmd/aether/build.go`)
  promotes each function with `ir.Promote` and emits with
  `amd64.EmitWith(prog, EmitOptions{Registers: true})`, which checks every
  allocation with `checkAllocation`. A failure of either is an `internal`
  diagnostic, and no executable is produced.
- **Size limits.** A function stays slot-based if it has more than 10,000 IR
  instructions, or if blocks × slots exceeds 1,000,000. A function of more
  than 2,000 values after promotion gets no registers. Such a function is
  slower and otherwise correct. These paths are the ones the earlier suites
  tested.
- `amd64.Emit`, the slot-based and SSA paths, and the interpreter's
  unpromoted IR stay in place as test variants and oracles. The IR contract,
  ABI-S0, and the runtime interface do not change.
- There is no flag. `aether build` has one backend, and no way to ask for
  the slot-based one. Optimization flags remain a separate Phase 5 item.
  (Superseded by D-028, which adds `-O0`.)

**Why the limits exist.** The first attempt had none. Two runs of the test
suite ended with the session shutting down, and after the second the WSL2 VM
had restarted. Rerun under a 6 GB-per-process limit, `TestBuildLargeFrame` ran
out of memory in `EmitWith`: it builds a `println` of 1,100,000 constants, and
the allocator kept a set of live values per instruction, about 10^11 entries
for that function. That this caused the shutdowns is inferred; the kernel log
did not survive the restart. RA-0's own testing did not reach this, because its
largest function had 1,310 values. Promotion was not observed to fail; its limits are set from its
table sizes, not from a measured failure.

**Spilling changed.** Spill-everywhere restarted its scan after each spill,
which took 1 min 49 s on a function at the limit (1,000 parameters live at
once). It now makes one forward pass, 171 ms on the same function. On the
nine workloads, `summary.tsv`, `functions.tsv`, and `ops.tsv` stayed
byte-identical to the RA-0 data, so RA-0's dynamic counts and static sizes
describe what ships.

**Evidence.**
- `TestMilestone1`: `prog.s` is `emitProgram`'s output, and the independently
  rebuilt executable is identical.
- `TestBuildBackend` and `TestBuildBackendLimits`: the build uses
  `emitProgram`; no function of the S2 suite or the workloads reaches a limit
  (the largest has 1,310 values); a function over each limit builds and gives
  the right output.
- `go test ./...`, including the negative controls and the gc differential
  tests, passes through the shipped path, as does 60 s of `FuzzGenerated`
  (15,409 executions).
- Three workloads built by `aether build` are byte-identical to the harness's
  `ra` executables and print gc's output.
- Run times are the RA-0 session's. They were not measured again, since the
  counts and sizes did not change.

**Consequences.**
- Built executables change, and run faster on the workloads measured
  (`benchmarks/2026-11-06-ra0/REPORT.md`). Their output and exit status are
  expected to stay the same; the tests above check that on the programs they
  cover.
- Frames are smaller, so a deep recursion that overflowed the stack may now
  finish. Stack exhaustion stays a documented limit, not a guarantee of depth.
- A bug in promotion or allocation is now a miscompilation in built programs,
  with no flag to work around it. The checker catches allocations it can model.
  It does not read the emitted code, so emitter bugs depend on the native tests.
- The compiler does more work per function. Compile latency has not been
  measured (`BENCHMARKS.md`).
- Phase 5's acceptance criteria that need optimization levels (`-O0`/`-O1`
  cross-checks) are not addressed by this entry.

**Revisit when.** A real program hits a limit and its speed matters; compile
latency is measured and is a problem; or optimization flags arrive, which
should give a way to select the slot-based path.

---

## D-028: `aether build -O0` selects the slot-based backend

**Status:** Accepted (2026-11-06, owner direction). It supersedes D-027's "no
flag" clause and adds to the CLI of D-022. Exit codes do not change. Since
D-031 the default build's calling convention is ABI-R1; `-O0` is unchanged and
stays on ABI-S0.

**Context.** Since D-027, a bug in promotion or allocation is a miscompilation
in built programs, with no way to build around it. ROADMAP Phase 5 also lists
optimization flags and an optimization cross-check.

**Decision.**
- `aether build -O0` emits with `amd64.Emit` on the unpromoted program: every
  value in a stack cell, no SSA promotion, no registers, and no size limits to
  apply. This is the backend `aether build` used before D-027.
- The default stays as D-027 made it. There is **no `-O1` or other level**: a
  flag the compiler cannot honor differently is not added until a second
  optimization level exists. `-O1` is a usage error (exit 2).
- `aether check` has no such flag, since it does not generate code.

**Alternatives.** `-O0` and `-O1` together, which would name the default but
select nothing new. An environment variable, which would hide the choice from
the command line. Both were passed over for the smallest contract that gives
a way out.

**Evidence.**
- `TestBuildO0`: the executable is byte-identical to what `amd64.Emit` gives,
  differs from the default build's, behaves the same, and combines with `-x`.
- `TestO0CrossCheck`: all S2-suite programs, built through the build path with
  and without `-O0`, give the interpreter's stderr and exit status.
- `TestBuildUsage`: `-O1` and a misplaced `-O0` are usage errors.

**Consequences.**
- The slot-based path is now a supported user-visible path, so it must keep
  passing the suites. Removing or changing `-O0` is a CLI contract change.
- The flag does not make a program correct by itself. It gives a second build
  to compare against when a miscompilation is suspected.
- Phase 5's "every optimization level" criteria now cover two backends, the
  default and `-O0`. Later passes must say which of them they apply to.

**Revisit when.** A second optimization level arrives, which is when `-O1` and
the meaning of the default should be decided.

---

## D-029: Instruction selection for comparisons, branches, and constants

**Status:** Accepted (2026-11-06, owner direction to implement instruction
selection; milestone accepted the same day as the default, at `ca5dbe2`, with
one unresolved regression). It applies to the default build only. `-O0`, and
`amd64.Emit`, are unchanged byte for byte and `-O0` stays the unoptimized
fallback. The IR, ABI-S0, the runtime interface, and exit codes do not change.

**Context.** RA-0 left the accumulator sequences in place, and its report
named `cmp; jcc` and immediates as the gap to gc.

**Decision.** `EmitOptions.Select`, which `emitProgram` sets
(`internal/amd64/select.go`):
- A constant that fits a sign-extended 32-bit immediate, or a `conv` of one
  whose result fits, has no location. Each use reads `$c`. Other constants load
  by the shortest form into their own register.
- A comparison or `not` whose only use is its block's `branch`, with only such
  immediates between, produces no value. The branch tests it directly. Nothing
  moves past code, so no call, panic check, or global access is reordered
  (D-024).
- Forms with an immediate operand for arithmetic, bitwise operations, shifts,
  and division, stores of immediates, direct phi copies, and jump threading
  through empty blocks and fall-through layout.
- `checkSelection` verifies each selection independently, like
  `checkAllocation`. A failure is an `internal` diagnostic.

**Alternatives.** A rewrite to a machine IR, which is more general and a far
larger change. Moving comparisons to their branch across code, which needs
liveness changes for little gain on these workloads. Direct register forms for
register-register operations, which were left for a separate change.

Block alignment was tried and left out by owner decision: it is a separate
optimization, and no variant tried improved every workload (the report has the
times and sizes).

**Evidence.** `benchmarks/2026-11-06-isel/REPORT.md`, whose method is that of
`BENCHMARKS.md`; the tests are listed in `TESTING.md`. Measured against the same
build without selection (`ra`), under WSL2 on one CPU, with times repeated at
`ca5dbe2`:
- Dynamic instructions fell 21.0% to 73.3% on all nine workloads, and static
  instructions 20.0% to 55.8%. Across the S2 suite and the workloads, static
  instructions fell 32.0% and frame bytes 30.7%, and no function grew.
- Median CPU time fell 11% to 36% on six workloads (b02, b03, b04, b06, b07, b09).
- b01 and b05 did not change: their ranges overlap.
- b08_ackermann got 20% slower.
- The ratio to gc went from 1.20-3.14 to 1.01-3.14 (1.01-3.16 in the repeat).

**Consequences.**
- A bug in selection is a miscompilation in built programs, and `-O0` avoids it.
- The harness's `ra` variant no longer is what `aether build` produces; `isel`
  is.
- **Unresolved regression.** b08_ackermann runs 20% slower despite 36% fewer
  instructions and the same memory operations. Its time is sensitive to block
  alignment, but `ra` aligned the same way stays about 13% ahead, so alignment
  does not remove the difference. **The cause is not established**: branch density
  is a hypothesis, and no hardware counters are available here.

**Revisit when.** Hardware counters or another CPU can test the b08 hypotheses;
code alignment is taken up; or register-register forms are.

## D-030: Experiment ABI-R1, a register calling convention for internal calls

**Status:** Accepted (2026-11-06, owner approval of the scope and criteria below;
three criteria settled by the owner on 2026-11-07). Implemented in
`internal/amd64/abir1.go`; every criterion met (`benchmarks/2026-11-07-abir1/`),
and shipped as the default by D-031. This approval authorizes the experiment only.
**Shipping ABI-R1 as the default needs a new entry after the results.** D-011 stays
in force: ABI-S0 remains the convention of `aether build`, with and without `-O0`,
throughout the experiment.

**Context.** ABI-S0 passes arguments and results through stack memory. The
investigation (`benchmarks/2026-11-06-abi/REPORT.md`) modeled candidate conventions
by rewriting the shipped assembly (`cmd/aether/bench_abi_test.go`):
- *Measured on the model:* median CPU time across code offsets changes by -30% on
  b08, -20% on b05, -13% on b01, -9% on b06, and -5% on b07; b02, b03, b04, and b09
  do not change. The model matched the interpreter on all 239 S2 suite programs.
- *Measured:* code placement alone moves some workloads by up to 57%, so results at
  a single layout are not sufficient.
- These are results of a model, not of an implementation.

**Decision: the convention.** For calls between Aether-compiled functions, when an
emission option that is off by default is set:
- **Arguments.** Arguments 0-5 are in `%rdi`, `%rsi`, `%r8`, `%r9`, `%r10`, and
  `%r11`, in that order. Each is one canonical word (D-023). Argument 6 onward stays
  stack-based at its ABI-S0 position: `8i(%rsp)` in the caller, `16+8i(%rbp)` in the
  callee.
- **Results.** A function with exactly one result returns it in `%rax`. A function
  with two or more returns **all** of them at their ABI-S0 positions.
- **Registers.** Every register except `%rbp` and `%rsp` stays caller-saved, as in
  ABI-S0. No value lives in a register across a call.
- **Moves.** The moves at a call site and at entry are parallel copies, with `%rax` as
  the temporary. The stack check needs `%rax` free at entry, so `%rax` is never an
  argument register (D-011, "Revisit when"). `%rcx` and `%rdx` stay reserved scratch.
- **Frame.** The stack check's mechanism, the frame chain, and the epilogue do not
  change. Frame sizes may shrink: the outgoing area A = 8·max(P+R) counts only calls
  with more than six arguments or more than one result, because the others use no
  outgoing word. Those calls keep the whole area, unused words included.
- **Entry precondition.** Argument registers hold their values only at entry, so:
  the entry block has no predecessor; before the last `param` runs, only `param`s and
  stores to slots may have run; and such a store touches only `%rax` and memory.
  Lowering produces this shape today, but the IR contract does not require it (it
  requires only that `param` is in the entry block). The IR contract does not change.
  A violation is an `internal` error when the option is set, checked independently of
  emission.

**Unchanged.** The IR, the allocator, instruction selection, the runtime and its
helper ABI (`rt/*.s`), exit codes, the CLI, ABI-S0 itself, `-O0`, and the harness's
`slot`, `ssa`, `ra`, and `isel` variants. A build uses one convention throughout. The
option has no command-line flag.

**Out of scope.** These are excluded even if a criterion would be easier to meet with
them. Each needs its own decision:
- **frameless leaves**, and any other removal of the frame chain or the stack check;
- **callee-saved registers**, and any caller knowledge of what a callee clobbers;
- allocator hints or coalescing, tail calls, code alignment, instruction scheduling,
  and results in more than one register.

**Correctness criteria.** All must hold with the option set. The option does not reach
the build path, so tests set it through `amd64.EmitOptions` as emitted by the default
build's pipeline, not through `aether build`.
1. `gofmt`, `go vet`, and `go test ./...` pass, and 60 s of `FuzzGenerated`, with its
   native check also running the option, exits 0.
2. The S2 suite and the nine workloads match the interpreter and the recorded gc
   output, and `TestSelectDifferential`'s programs agree across gc, the interpreter,
   `-O0`, and the option. A new cross-check compares the option with ABI-S0 on the
   suite, since `TestO0CrossCheck` goes through the build path.
3. New native tests, with values both in registers and in cells: 0 to 8 arguments;
   every permutation of arguments among argument registers that forces a copy cycle;
   arguments as immediates, cells, and constants too large for an immediate; 1 to 3
   results; nested calls; mutual recursion; and D-024's order with panics in arguments.
4. Functions above each size limit (`TestBuildBackendLimits`: over the promotion limit
   and over the allocator's) build and run under the option.
5. The stack boundary tests are updated for ABI-R1's frame sizes and pass: entry at
   E = T, at E = T − 16, overflow with arguments in registers, and the helpers at the
   reserve's edge. The ABI-S0 tests stay as they are. The D-011 bound still holds as
   stated. Peak stack is no higher than the `isel` baseline on any workload (the `isel`
   rows of `benchmarks/2026-11-06-abi/data/abi_summary.tsv`).
6. An independent check verifies each entry and call-site copy, and the entry
   precondition, as `checkAllocation` does for allocations. A failure is an `internal`
   error.
7. New negative controls are each detected: swapped argument registers, a dropped
   cycle break, a stale `%rax` result, a seventh argument read from a register, a frame
   missing a needed outgoing area, and a `param` after a clobbering instruction.
8. With the option off, `-O0` output and the `slot`, `ssa`, `ra`, and `isel` assembly
   stay byte-identical. With it on, the assembly is deterministic.
9. With the option on, b03 and b04 stay byte-identical to `isel` if they avoid ABI-R1's
   call paths, meaning that no Aether function in the workload is called with, receives,
   or returns a value. The test establishes this from the emitted assembly instead of
   assuming it. Otherwise, the requirement for that workload is semantic equivalence:
   the same stderr and exit status as `isel` and the recorded gc output. *Verified
   2026-11-07 at 67e19d1:* both avoid these paths. Their only calls are `main.main`'s to
   runtime helpers, whose ABI does not change, and `_start`'s to `main.init` and
   `main.main`, which pass and return nothing. Byte-identity therefore applies today.

**Performance criteria.** Fixed before implementation, against `isel` at the same
commit, by the method of `BENCHMARKS.md`: pinned, round-robin, with a discarded
warm-up, at the 8 code offsets of `TestABITime`, with at least 7 runs per offset.
1. CPU time improves by at least 10% on each of b01, b05, and b08 (ratio at most
   0.900), and by at least 5% on b06 (ratio at most 0.950). The model gave 0.874,
   0.801, 0.696, and 0.913.
2. No workload's aggregate is more than 2% slower (ratio at most 1.020). The limit does
   not apply to individual offsets.
3. On every workload, dynamic instructions and memory operations are no higher than
   the `r1` row of `benchmarks/2026-11-06-abi/data/abi_summary.tsv`.

The ratios in 1 and 2 are of the median of the eight per-offset medians. Only these
aggregates decide: on the model, single offsets reach 1.042 on b05 and 1.064 on b09
while their aggregates are 0.801 and 1.001. Every per-offset measurement is reported,
and significant outliers are investigated. An offset is a significant outlier when its
ratio differs from the workload's aggregate by more than the measured noise band
(`BENCHMARKS.md`, Regressions). The report gives each one's cause, or records it as
unresolved with measurements and hypotheses kept apart. An outlier does not change the
aggregate verdict, but the results are not complete until it has been investigated.

**Outcome rules.**
- Missing any criterion means ABI-R1 does not ship. The results are reported whatever
  they are, including every regression.
- Meeting them all does not ship it either, and the owner decides on a new entry.
- If meeting a criterion would need anything out of scope, the experiment stops and
  returns to the owner.

**Alternatives.** Keeping ABI-S0 without the `%rax` hop gains at most 5.5%. Frameless
leaves and callee-saved registers gain more on some workloads, with the costs in the
report, and stay separate so that each can be judged alone. System V and Go's
ABIInternal were set aside by D-011 and not measured.

**Consequences.**
- While the experiment runs, tests and the harness cover a convention no built
  executable uses.
- The harness gains a variant for the implementation, separate from the model's `r1`,
  and `TestABITime` times it. Existing rows do not change.
- Shipping would change what D-011, D-028's description of the default, and V-2's
  remark on frame sizes say, which the new entry must address.
- Arguments in registers at entry would have to be preserved by a growable stack's
  growth path (Phase 10, undecided). Nothing lives in a register across a call, so
  D-010's stack-map options are not constrained further.

**Revisit when.** The results are in; or the entry precondition stops holding, for
example if lowering or a pass puts other code before a `param`.

---

## D-031: `aether build` uses ABI-R1; `-O0` keeps ABI-S0

**Status:** Accepted (2026-11-07, owner approval).

**Context.** Experiment ABI-R1 (D-030) met every correctness and performance
criterion. Its evidence is in `benchmarks/2026-11-07-abir1/`, which holds the
raw timings, summaries, and environment:
- *Measured:* median CPU time across eight code offsets against `isel`, 11 runs
  per offset: b01 0.881, b05 0.827, b06 0.916, b07 0.954, b08 0.716. The rest are
  0.985 to 1.003, against the 1.020 limit.
- *Measured:* dynamic instructions and memory operations equal the model's `r1`
  row on all nine workloads, and peak stack is no higher than `isel`'s on any.
- *Not established:* the cause of the per-offset outliers. Every one was
  reproduced by an independent rerun, and none moves an aggregate past a limit.
  At b08's offset 24, ABI-R1 is no faster than `isel`.

**Decision.**
- `aether build` emits with ABI-R1: `emitProgram` sets `amd64.EmitOptions.ABIR1`.
  The convention, its entry precondition, and its checker are as in D-030.
- `aether build -O0` is unchanged: `amd64.Emit`, slot-based, on ABI-S0 (D-011,
  D-028). A build uses one convention throughout.
- Nothing else changes: the IR, the allocator, instruction selection, the runtime
  and its helper ABI, exit codes, and the CLI. The stack check, the frame chain,
  and the bound of D-011 still hold. Only the frame sizes may shrink.
- What D-030 excluded stays excluded, each needing its own decision: frameless
  leaves, callee-saved registers, and the rest of D-030's list.

**Alternatives.** Keeping ABI-R1 behind the option, which leaves the measured gain
unused. Shipping it only with `-O1`, which D-028 rules out while no second level
exists.

**Consequences.**
- A bug in ABI-R1 is now a miscompilation in built programs. `-O0` builds around
  it, and `TestO0CrossCheck` compares the two conventions on the S2 suite.
- The harness's `isel` variant stays ABI-S0 with selection, the baseline of D-030.
  `abir1` is what `aether build` produces.
- The register-based successor that D-011's "Revisit when" anticipated exists
  for internal calls. Changes to it, such as callee-saved registers, need new
  entries.

**Revisit when.** A pass or lowering change breaks the entry precondition; a
growable stack (Phase 10) needs the argument registers preserved on its growth
path; or a later convention is proposed.

---

## D-032: Floating point in the IR and the runtime (Phase 6 increment 6.1)

**Status:** Accepted (2026-11-09, owner approval as proposed, with three
constraints: a well-defined `f32` representation, strict separation of float and
integer operations, and gc-compatible amd64 float-to-integer conversion).

**Context.** Phase 6 adds `float64` and `float32` (`ROADMAP.md` Phase 6), and
requires gc's print format. go1.26.1 prints a float with
`strconv.AppendFloat(v, 'g', -1, bitSize)`: the shortest decimal that rounds back,
computed with the Dragonbox algorithm (`internal/strconv/ftoadbox.go`). Measured
with gc-built binaries on 2026-11-09: `println(1e6, 0.1, 123456.0, 1e-5, -0.0)`
writes `1e+06 0.1 123456 1e-05 -0`, `float32(0.1)` writes `0.1`, and infinities and
NaN write `+Inf`, `-Inf`, and `NaN`.

**Decision.**
- **Values.** Types `f64` and `f32`. A float is one canonical word (D-023): an
  `f64` holds its IEEE 754 bits, an `f32` its 32 bits zero-extended. Constants
  hold those bits. ABI-S0, ABI-R1, slots, globals, and the register allocator move
  floats as words; no value lives in a vector register across instructions.
- **Operations** are distinct from the integer ones, so that no pass written for
  integers applies to a float by accident: `fadd`, `fsub`, `fmul`, `fdiv`, `fneg`
  (sign flip) with IEEE 754 round-to-nearest-even at the type's precision and no
  fused operations; `feq`, `fne`, `flt`, `fle`, `fgt`, `fge` (NaN unordered:
  only `fne` is true); `itof` (correctly rounded, from any integer type);
  `ftof` (`f64`↔`f32`); `ftoi`, truncating toward zero. Out-of-range `ftoi` is
  implementation-specific in Go; the IR defines it as gc does on amd64 (measured
  2026-11-09): `int8`, `int16`, `int32`, `uint8`, and `uint16` through a 32-bit
  `cvttsd2si`, `int64` and `uint32` through a 64-bit one, and `uint64` as gc's
  split at 2⁶³, each then reduced to the type.
- **Runtime.** `aether.printfloat64` and `aether.printfloat32` (`rt/float.s`) port
  go1.26.1's Dragonbox and `'g'` formatting to assembly, under the helper interface
  of `aether.printint`, using at most 120 bytes below `%rsp`. Their table of 128-bit
  powers of ten, `rt/pow10.s`, is generated by `TestPow10Table`.

**Alternatives.** Holding floats in vector registers, which needs a second
register class in the allocator and ABI-R1; it can follow as an optimization.
gc's pre-1.26 format (`+1.000000e+000`), which no longer matches gc. Compiling the
formatter from Go source with Aether, which needs arrays and a runtime built by
the compiler.

**Consequences.** Every float operation moves its operands into `%xmm0` and
`%xmm1` and back, costing two or three moves each. The formatter follows a
specific Go release; a later change to gc's format is a new divergence to record.

**Evidence.** `TestPrintFloat` prints 644,612 float64 and float32 values natively
(special values, every binary exponent, powers of two and of ten with their
neighbors, short decimals, and random bit patterns) and requires the exact
`strconv` output for each. Two planted bugs, in the `e == -77` tie rule and in the
parity test, each made it fail.

---

## D-033: String constants in the IR and the runtime (Phase 6 increment 6.2)

**Status:** Accepted (2026-11-10, owner approval).

**Context.** Phase 6 adds string constants: static data, `len`, indexing with
bounds-check panics, and printing (`ROADMAP.md` Phase 6). Concatenation, slicing,
pointers, and conversions are excluded. Measured with gc-built go1.26.1 binaries on
2026-11-09: `print` and `println` write a string's bytes exactly, NUL and invalid
UTF-8 included; an index out of range panics with `index out of range [3] with
length 3`, an unsigned index printed unsigned (`[9223372036854775808] with length
3`), and a negative signed index with no length (`index out of range [-1]`), exit
status 2.

**Decision.**
- **Values.** Lowering represents a Go string as two words: a `ptr`, the address of
  constant bytes, and an `i64` length. Variables, globals, parameters, and results
  of type `string` become two slots, globals, parameters, or results, so slots,
  phis, the allocator, ABI-S0, and ABI-R1 move strings as words and do not change.
  Every string value refers to constant data; nothing creates string bytes at run
  time.
- **Data.** `Program.Strings` holds the distinct, non-empty constants. `str k`
  produces the address of string k; the empty string is the `ptr` constant 0 with
  length 0, which is also the zero value. The backend emits each string's exact
  bytes, with no terminator, under a local `.rodata` label.
- **Operations.** `loadb p, i` reads the byte at offset i, an `i64`, as a `u8`; its
  precondition is 0 ≤ i < the length, which lowering checks first with an unsigned
  comparison, branching to `panic index, i, n`, a new run-time error whose operands
  are the original index, of any integer type, and the length. A `ptr` is used
  only by `loadb`, by `print` and `println` as an operand immediately followed by
  its `i64` length, and as a word that is stored, loaded, passed, and returned.
- **Runtime.** `aether.printstr` (address in `%rdi`, length in `%rsi`) and
  `aether.panicindex`/`aether.panicindexu` (signed or unsigned index in `%rdi`,
  length in `%rsi`), under the helper interface (`ARCHITECTURE.md` §Runtime
  integration boundary).

**Alternatives.** A two-word string type in the IR, which every pass, the allocator,
and both calling conventions would have to learn. A `print` instruction per
string, which splits `println` and changes the IR of every existing program.

**Consequences.** Comparison of strings is not part of this increment; it needs a
runtime comparison of bytes and stays `unsupported`. The interpreter holds a `ptr`
as the string's index plus 1.

**Evidence.** `TestNativeStrings` prints and indexes strings holding all 256 byte
values, a NUL, and multibyte and invalid UTF-8, through slots, globals, a call with
seven string arguments, and a returned string, in the interpreter, the slot-based
backend, and the default backend, with identical output. `TestPanicIndex` checks
the panic message at the signed and unsigned extremes natively and in the
interpreter.

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
