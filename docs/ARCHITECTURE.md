# Aether Architecture

Status: **design only.** No stage described here is implemented. Decisions are
referenced by ID from [`DECISIONS.md`](DECISIONS.md). Where this document says
"proposed", the corresponding decision is not yet accepted.

## Pipeline

```
                        ┌──────────────── reused (D-001, D-002) ────────────────┐
  prog.go ──► go/parser ──► go/ast + token.FileSet ──► go/types ──► types.Info
                                                                   │
                         ┌─────────────── Aether-owned ────────────┘
                         ▼
                   subset gate (D-003) ──► unsupported diagnostics
                         │
                         ▼
                   lowering ──► IR, load/store form (D-004)
                         │          │
                         │          ├──► IR verifier
                         │          └──► reference interpreter (D-005, test oracle)
                         ▼
             [Phase 5] SSA construction + optimization passes (D-006, proposed)
                         │
           ─ ─ ─ ─ ─ ─ ─ ┼ ─ ─ ─ ─ ─ ─ ─  architecture-independent above
                         ▼                architecture-specific below
                   amd64 backend:
                     instruction selection
                     register allocation   (Phase 3: none, every value in a stack slot)
                     frame layout
                     GNU assembly emission (D-007)
                         │
                         ▼
     prog.s + Aether runtime .s (D-009) ──► as ──► .o ──► ld -static ──► ELF executable
                                            └── GNU host tools: translate and link only (D-007, D-008)
```

The candidate pipeline in the Phase 0 brief has been adjusted as follows:
- Parsing and semantic analysis are reused rather than built.
- A **subset gate** is added as an explicit stage.
- SSA is **deferred** until after the first executable.
- An **interpreter** hangs off the IR as a test oracle.
- Object generation and linking are delegated to GNU binutils. This is accepted
  as a potentially long-term boundary (D-008).

## Stages

### 1. Frontend (reused)

- **Responsibility:**
  - Read source files.
  - Parse with `go/parser` (`ParseComments | SkipObjectResolution`).
    `ParseComments` is required so the gate can see directive comments
    (D-001, E11).
  - Type-check with `go/types` (`GoVersion: "go1.26"`,
    `Sizes: types.SizesFor("gc","amd64")`).
  - Convert parser and checker errors into Aether diagnostics of class
    `invalid`.
  - Run Aether's program-level checks that `go/types` does not perform
    (D-002). In S1 that is one check: `package main` must declare
    `func main`. A violation is `invalid`.
- **Importer:** none. `Config.Importer` stays nil. Imports are rejected by the
  gate's pre-check before type checking. Aether must never import
  `go/importer` or `go/build`, because both depend on `os/exec` and can run
  the `go` command (E9).
- **Output:** a *checked package*: the file set, the `[]*ast.File`, the
  `*types.Package`, and the populated `*types.Info`.
- **Not done here:** any decision about whether Aether supports the program.

### 2. Subset gate (Aether, D-003)

- **Responsibility:** decide whether the program is inside the current
  supported subset (`SEMANTICS.md`). If it is not, emit an `unsupported`
  diagnostic naming the construct and its position.
- **Two passes:**
  - A pre-check on the raw files *before* type checking, for file-level
    constructs. In S1 it rejects:
    - more than one source file
    - a package name other than `main`
    - any import declaration, including `import "C"` (cgo)
    - `//go:` directive comments, `// +build` lines, and `//line` directives

    Imports must be rejected here: without an importer `go/types` reports
    them as errors, which would misclassify unsupported programs as invalid.
  - A typed walk after type checking. It visits **every** expression, including
    sub-expressions of constant expressions, because S1's literal and
    operator restrictions apply to constant expressions too (`SEMANTICS.md`
    §Subset S1). It checks types using `types.Info` rather than syntax. For
    example, a `println` argument must have recorded type `int`.
- **Precedence** (D-003): pre-check `unsupported` → `invalid` (parser, checker,
  program-level checks) → typed `unsupported`.
- **Output:** pass or fail, plus diagnostics.
- **Contract:** if the gate passes, every construct in the program is listed as
  supported in the support matrix.

### 3. Lowering (Aether)

- **Responsibility:**
  - Translate the typed AST into IR.
  - Fix evaluation order.
  - Desugar compound assignments and `++`/`--`.
  - Materialize constants from `types.Info` values.
  - Map `types.Object` to IR locals.
  - Map `token.Pos` to IR positions.
- **Rules that S1 correctness depends on:**
  - **Constants.** Any expression with a non-nil `types.Info.Types[e].Value`
    becomes one IR constant, taken from that value with `constant.Int64Val`.
    Its operands are never lowered as arithmetic.
  - **Variables.** Identifiers in `Info.Defs` create a new local slot.
    Identifiers in `Info.Uses` refer to the existing slot. This covers
    shadowing, and short-variable redeclaration, which is an assignment
    (S1-10).
  - **Tuple assignment.** Evaluate every right-hand operand into a temporary,
    then store to the targets left to right.
  - **`println(a1, …, an)`.** Evaluate every argument into a temporary first.
    Then emit, in order: print-int `a1`, print-space, print-int `a2`, …,
    print-int `an`, print-newline. With no arguments, emit only
    print-newline. These are IR runtime-call instructions. The interpreter
    executes them directly, and the backend turns them into calls to the
    runtime helpers.
  - **Function end.** `main`'s single block ends with a `return` terminator.
- **Output:** an IR program.
- **Contract:** no `go/ast`, `go/types`, or `go/token` value is reachable from
  the IR.

### 4. IR (Aether, D-004)

- **Structure:** program → functions → blocks → instructions, with values typed
  by Aether IR types.
- **Tools:**
  - a verifier
  - a deterministic text printer
  - later, a text parser so passes can be tested from hand-written IR
    (proposed; only if golden tests need it)

**IR invariants.** The verifier enforces these after lowering and after every
pass:

1. Every block ends with exactly one terminator, and no terminator appears
   before the end.
2. Every branch target is a block of the same function. The entry block has no
   predecessors.
3. Every instruction operand is defined in the same function, and operand types
   match the instruction's signature.
4. Loads and stores reference slots of matching type.
5. Every instruction has a position, or is explicitly marked as synthetic.
6. **SSA level** (Phase 5): each value is defined once, and every definition
   dominates its uses. φ-nodes appear only at the start of a block and have one
   operand per predecessor.

**Determinism.** Printing the same IR always produces identical bytes. Block
and value numbering never depend on map iteration order.

### 5. Reference interpreter (Aether, D-005)

- Executes IR directly and implements the subset's run-time semantics,
  including the `println` format.
- It exists to give tests an oracle and to locate bugs. It is not a product
  feature and has no performance goals.

### 6. Optimizer (Aether, Phase 5+, D-006 proposed)

- Passes are IR → IR functions. Each pass is individually switchable for
  testing.
- Every pass is followed by the verifier in test builds.
- No pass may change observable behavior. That covers:
  - output and exit status
  - panics and their order
  - wraparound results
- A pass may not reorder side effects, and it may not assume that signed
  overflow does not occur.
- Correctness is established by the tests in `TESTING.md` §Optimization
  cross-checks.
- The first candidate passes are constant folding, copy propagation, and dead
  code elimination. Each is justified only with `BENCHMARKS.md` measurements.

### 7. Backend (Aether, architecture-specific)

The **Phase 3 approach** (D-007) is deliberately naive:
- Every IR value and local gets an 8-byte, 8-byte-aligned, `rbp`-relative
  stack slot.
- Each instruction loads its operands into fixed scratch registers, computes,
  and stores the result.
- The frame is `rbp`-based, and its size is rounded so that `rsp` is 16-byte
  aligned at every `call`.
- The output is GNU assembly in AT&T syntax.

This makes register allocation unnecessary for the first executable. The
backend consumes only IR: `internal/amd64` must not import `go/ast`,
`go/types`, `go/token`, or `go/parser`.

**Integer representation (S1).**
- `int` is a 64-bit two's-complement value held in a 64-bit register or slot.
- The S1 operations map to instructions whose 64-bit results already have Go's
  wraparound semantics:
  - `+` → `addq`
  - `-` → `subq`
  - `*` → `imulq`, low 64 bits
  - unary `-` → `negq`, which maps `MinInt64` to itself
- Constants are materialized with `movq` for values that fit a sign-extended
  32-bit immediate, and `movabsq` otherwise.
- No S1 operation needs a flag check or a special case.

**Runtime calls (S1).** Each print-int, print-space, or print-newline IR
instruction becomes a `call` to the corresponding runtime helper, using the
runtime-helper interface in §Runtime integration boundary.

**Later** (Phase 5):
- instruction selection over SSA
- register allocation (algorithm not chosen)
- a documented calling convention (D-011)
- frame layout that reserves room for future stack maps (D-010)

### 8. Assembly and linking (GNU host tools, D-007, D-008)

GNU `as` and `ld` are accepted as initial and potentially long-term
dependencies (D-008).

**What each side does.**
- **Aether owns:** instruction selection, register allocation, stack layout,
  assembly emission, and the semantics of the generated program.
- **`as`:** translates Aether's assembly into ELF relocatable objects.
- **`ld`:** constructs the static executable from Aether's program object and
  runtime objects.

**Commands.** The driver runs `as --64` on the generated `.s` and on each
runtime `.s`, then `ld -static -nostdlib -e _start`, with no other inputs or
libraries.

**Driver requirements** (`internal/toolchain`):
- **Availability and identity check before use.**
  - Locate each tool (`PATH` by default; an override mechanism is defined in
    Phase 3), and resolve it to an absolute path.
  - Confirm it is the GNU tool (`as --version` / `ld -v` version line).
  - Confirm it can target x86-64 ELF.
  - Confirm the host is linux/amd64 when the build is going to run the
    executable.
- **Clear errors.** Any failure is a `tool` diagnostic naming:
  - the tool
  - the path used or searched
  - the version line, if one was obtained
  - the tool's stderr
- **Process choke point.** This is the only package that starts processes.
  A test enforces that, among Aether's packages, only `internal/toolchain`
  imports `os/exec` or calls `os.StartProcess`. It also uses `go list -deps` to
  check that, apart from paths through `internal/toolchain`, no Aether
  package reaches `os/exec` transitively, for example through `go/importer`
  (E9).
- **Fixed tool environment.** Tools run with an explicit environment
  containing only `LC_ALL=C`, which E9 showed is sufficient. They run by
  absolute path, with the work directory as the current directory. The
  invoking user's environment therefore cannot change tool behavior, and tool
  messages are stable.
- **Command log.** With `-x`, the driver logs:
  - every command it runs: absolute tool path, arguments, and environment
  - each tool's version line

  The log is supporting provenance evidence. The primary checks reproduce the
  build independently (`ROADMAP.md` §Milestone 1).
- **Work directory layout.** The work directory is a fresh temporary directory
  per build. It contains:
  - `prog.s` (the program assembly) and `prog.o`
  - one `.s`/`.o` pair per runtime file
  - the unrenamed link output

  The directory is removed after the build, unless `-work` is given, in which
  case its path is printed.
- **Output handling.**
  - `ld` writes to a temporary name in the work directory.
  - The result is moved to the output path only after every step has
    succeeded. It is renamed when both are on the same file system; otherwise
    it is copied to a temporary file next to the output path and that file is
    renamed.
  - On any failure the output path is neither created nor modified. A file
    already there is left untouched.
- **Reproducibility** (D-008):
  - fixed argument lists, a fixed tool environment, and fixed relative file
    names inside a fresh work directory
  - no absolute or temporary paths, timestamps, or build-ids in the generated
    assembly or the linker options

  Identical inputs and configuration give byte-identical output. The
  work-directory path and the output file name do not affect the bytes (E6,
  E9).

### 9. Runtime (Aether, D-009)

- **Phase 3:** hand-written GNU assembly containing:
  - `_start`: align the stack, `call main.main`, then `exit_group(0)`.
  - Integer printing and space/newline helpers.
    - They write to fd 2 with raw `write` syscalls.
    - Short writes are continued and `EINTR` is retried.
    - Other errors are ignored (D-009, E7).
    - Integer formatting must handle `MinInt64` without negating it.
- **Phase 4:** adds the minimal panic exit (status 2).
- **Source layout:** runtime sources live in `internal/amd64/rt/*.s`, a
  subdirectory with no Go files. They are embedded into the compiler with
  `//go:embed` and written to the work directory at build time. E9 showed that
  this layout works with `go build`, `go vet`, and `go test`. GAS-syntax `.s`
  files placed directly in a Go package directory break `go build`.

## Runtime integration boundary

The compiler and runtime meet at an explicit, documented interface. In Phase 3
that interface is:

| Symbol | Provided by | Contract |
|---|---|---|
| `_start` | runtime | Process entry. Calls `main.main`, then exits 0. |
| `main.main` | compiler | Aether-compiled `main`. No arguments, no results. Called with `rsp` 16-byte aligned before `call`. |
| runtime print helpers (names fixed in Phase 3) | runtime | Print one `int64` in decimal / a space / a newline to fd 2. Planned calling interface, to be fixed in Phase 3 with the runtime source: the integer argument in `%rdi`; no result; `rsp` 16-byte aligned before `call`; the helper preserves `rbx`, `rbp`, `r12`–`r15` and may clobber every other general-purpose register (a subset of the System V convention). This interface covers only runtime helpers. It does not decide D-011. |

Rules:
- The compiler may only call runtime symbols listed in this table (later, in a
  dedicated document if it grows).
- The runtime may only assume what the table states about compiled code.

Some Go features need far more runtime support than others:

| Feature | Runtime needs |
|---|---|
| `println` (ints) | write syscall, number formatting |
| `/` and `%` | panic path |
| bounds checks, nil checks | panic path |
| strings | static data, later concatenation, which needs the heap |
| slices, `append`, `new`, escaping locals | heap allocator (D-010) |
| maps | hashing, allocator, iteration-order randomization policy |
| interfaces | type descriptors, itabs |
| closures | heap-allocated captured variables |
| `defer`/`recover` | defer records, unwinding |
| goroutines, channels, `select` | scheduler, stacks, possibly threads |
| reflection | complete type metadata |
| the standard library | nearly all of the above, plus assembly-implemented functions and `//go:linkname` dependencies |

Staging follows `ROADMAP.md`.

## Candidate package boundaries (non-binding, D-016)

Packages are created only when code exists. Everything lives under one module.

| Candidate path | Contents | Depends on | Stability |
|---|---|---|---|
| `cmd/aether` | CLI driver (`aether build`, `aether check`) | everything below | CLI flags and exit codes are a contract |
| `internal/diag` | Diagnostic type, classes, rendering | — | stable within the repo |
| `internal/frontend` | Load, parse, type-check; produce the checked package | `go/*`, `diag` | internal |
| `internal/gate` | Subset gate | `frontend`, `diag` | internal |
| `internal/ir` | IR types, builder, verifier, printer | — (no `go/*`) | **contract:** text format + invariants |
| `internal/lower` | Typed AST → IR | `frontend`, `ir`, `diag` | internal |
| `internal/interp` | IR interpreter | `ir` | internal |
| `internal/opt` | Optimization passes (Phase 5+) | `ir` | internal |
| `internal/amd64` | isel, frames, regalloc, asm emission; runtime sources in `internal/amd64/rt/*.s`, embedded (§9) | `ir` | internal |
| `internal/toolchain` | Locate and run `as` and `ld` | — | internal |

Dependency rules (each enforced by a test once the package exists):
- `ir` and `amd64` must not import `go/ast`, `go/types`, `go/token`, or
  `go/parser`. The backend sees only IR.
- `os/exec` may enter the dependency graph only through `toolchain`:
  - only `toolchain` imports `os/exec`
  - no other Aether package's own imports pull it in transitively (packages
    that import `toolchain`, such as `cmd/aether`, are allowed)
  - nothing imports `go/importer` or `go/build` (E9)
- Nothing imports `cmd/aether`.
- Architecture-specific code lives only under `internal/amd64` (or
  `internal/<arch>`).
- A shared target interface is extracted only when a second architecture
  exists.

## Diagnostics and error propagation

- **Diagnostic fields:** class (`invalid` | `unsupported` | `internal` |
  `tool`), position (file:line:col), message, and, for `unsupported`, the
  feature name used in `SEMANTICS.md`.
- **Rendering:** `file:line:col: <class>: <message>`, sorted by position,
  deterministic.
- **Propagation:**
  - Each stage returns its diagnostics.
  - A stage runs only if all earlier stages produced no errors.
  - Stages after the gate never produce `invalid` or `unsupported`.
  - Within one stage, errors are collected rather than stopping at the first,
    where practical. `go/types` supports this through `Config.Error`.
  - Panics inside Aether are recovered at the driver and reported as `internal`.
- **No output on failure:** no file is created at the output path on any error.
  This covers every class, including `tool`. Gate and frontend failures stop
  before any `.s` or `.o` is produced. Linking is atomic (§8).

**Proposed CLI exit codes** (finalized in Phase 1):

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | `invalid` |
| 2 | usage error |
| 3 | `unsupported` |
| 4 | `internal` |
| 5 | `tool` |

The distinct `unsupported` code is what lets tests separate a limitation from a
bug (`TESTING.md`).

## How the architecture supports incremental growth

- A new language feature touches the gate, lowering, the IR (possibly new
  instructions), the interpreter, and the backend. It does not touch the
  frontend.
- SSA (Phase 5) is added as a pass over the existing IR, plus stricter verifier
  invariants. Phase 3 lowering stays valid.
- Register allocation replaces the slot-per-value strategy inside the backend.
  The IR contract does not change.
- The binutils boundary is accepted long-term (D-008). If a concrete
  requirement ever justifies an in-house assembler or ELF writer, it would
  replace only `internal/toolchain` and the final emission step. Everything
  earlier is unaffected.
- A custom parser or type checker (revisiting D-001 or D-002) would replace
  `frontend` and the gate's inputs. The IR boundary keeps `go/*` out of the rest
  of the compiler.

## Undecided areas

- SSA construction method, and whether SSA shares the IR package (D-006)
- GC strategy (D-010)
- Calling convention (D-011)
- Register allocation algorithm
- Representation of interfaces, closures, defer records, and maps
- Goroutine stack strategy
- Multi-package compilation, and how imported packages are type-checked and
  compiled
- Debug info (DWARF)

## Major risks and mitigations

| Risk | Mitigation |
|---|---|
| Silent miscompilation | Gate default-deny; the verifier after every pass; three-way differential tests (gc / interpreter / native) |
| `go/*` reuse obscuring what Aether owns | Documented ownership boundaries; no `go/*` types in the IR |
| Runtime scope explosion (GC, scheduler) | Strict phase staging; runtime features decided individually (D-009, D-010) |
| Premature abstraction | D-016: packages and interfaces only when used |
| Early backend designs blocking GC later | D-010 and D-011 require a stack-map and stack-check story before Phase 5 and Phase 10 designs are finalized |
| Host tool dependence | Accepted (D-008). Isolated in `internal/toolchain`, with identity checks, clear `tool` errors, version recording, and reproducibility tests. Revisit triggers are listed in D-008. |
| Mistaking the absence of gc markers for provenance | The Milestone 1 primary checks establish provenance by independently reconstructing the executable from Aether's emitted assembly with GNU `as`/`ld` and comparing bytes. The command log, ELF section inspection, and symbol inspection are supporting evidence only (`ROADMAP.md`). |
| Gate accepts a valid-Go form lowering does not handle (for example `var x int = 1.0`, redeclaration) | S1 spells these forms out (`SEMANTICS.md`); the gate walks constant sub-expressions; there are dedicated unsupported and S1-10 tests; lowering reports anything unexpected as `internal` |
