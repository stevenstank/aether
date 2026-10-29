# Aether Architecture

Status:
- **Implemented (Phase 1):** stages 1 (frontend) and 2 (subset gate),
  diagnostics, and the `aether check` driver.
- **Implemented (Phase 2):** stages 3 (lowering), 4 (IR, with verifier and
  printer), and 5 (reference interpreter), for S1.
- **Implemented (Phase 3):** stages 7 (amd64 backend), 8 (assembly and
  linking with GNU `as`/`ld`), and 9 (runtime), for S1, and `aether build`.
- **Design only:** the optimizer (stage 6), and everything beyond S1.

Decisions are referenced by ID from [`DECISIONS.md`](DECISIONS.md). Where this document says
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
    - an empty result list on `main` or `init` (`func main() ()`), which
      `go/types` wrongly reports as an error (E12)

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
  - **`println(a1, …, an)`.** Evaluate every argument, left to right, then
    emit one `println` instruction with those values as operands (D-021).
  - **`var x int`** without an initializer stores the constant 0 explicitly.
  - **`x op= y`** loads `x`, evaluates `y`, applies `op`, and stores to `x`.
    **`x++`/`x--`** load, add or subtract the constant 1, and store.
  - **Unary `+` and parentheses** produce no instruction. Unary `-` is `neg`.
  - **Constant declarations** produce no instruction; uses are constants.
  - **Function end.** `main`'s single block ends with a `ret` positioned at
    the closing brace.
- **Failure:** lowering only runs on gate-accepted programs. Anything it does
  not expect (a node kind, a non-`int` type, an unresolved identifier, a
  constant outside `int64`) stops it with an `internal` diagnostic at that
  position, and no IR is produced.
- **Driver:** `compile` in `cmd/aether/check.go` runs the frontend, gate,
  lowering, and verifier. `aether check` runs it on every program, so a
  lowering or verifier failure is reported as `internal` (exit 4) rather than
  the program being accepted. For programs that lower correctly, `check`'s
  output is unchanged.
- **Output:** an IR program.
- **Contract:** no `go/ast`, `go/types`, or `go/token` value is reachable from
  the IR.

### 4. IR (Aether, D-004, D-021)

The IR is `internal/ir`. It imports no `go/*` package. Its operations are
documented on their constants in `ir.go`, and this section is the contract a
backend or interpreter may rely on.

**Structure.**
- A **program** is a list of functions. Its entry point is the function named
  `main`. In S1 it is the only function, and it has no parameters or results.
- A **function** has a name, a source file, a position, a list of **slots**
  (its local variables), and a list of **blocks**. `Blocks[0]` is the entry.
- A **slot** has an ID equal to its index, a source name (for printing only;
  names may repeat), the type `i64`, and a position. A slot has no defined
  contents until it is stored to.
- A **block** is a list of instructions executed in order. The last is its
  only terminator.
- An **instruction** has an operation, a result type (`i64`, or none), a value
  number (`vN`; `-1` without a result), value operands, and, by operation, a
  slot or a constant. An instruction with a result is that value; operands
  point at earlier instructions. Value numbers are unique in the function and
  assigned in creation order.

**Operations.** `i64` is 64-bit two's complement. No operation traps.

| Op | Operands | Result | Meaning |
|---|---|---|---|
| `const` | the constant | `i64` | the constant |
| `load` | slot | `i64` | the slot's current contents |
| `store` | slot, value | none | writes the value to the slot |
| `add`, `sub`, `mul` | 2 values | `i64` | low 64 bits of the exact result |
| `neg` | 1 value | `i64` | `0 - x`, wrapping: `neg` of the minimum is the minimum |
| `println` | 0 or more values | none | writes the values to standard error in signed decimal, separated by single spaces, then a newline; write errors are ignored |
| `ret` | none | none | terminator: returns from the function |

**Evaluation order and effects.** Instructions execute in block order. The
only observable effects are `println` writes, which happen in instruction
order. Because a `println`'s operands are values, all of them are computed
before any of its output. Lowering emits operands left to right, and the
right-hand sides of an assignment before its stores.

**Text format.** `Program.String` prints the IR deterministically for golden
tests. There is no parser.

For `func main() {` on line 3, `x := 6` on line 4, and `println(x, -x)` on
line 5, each indented by one tab:

```
func main @prog.go:3:6
  slot s0 x i64 @4:2
b0:
  v0 = const 6 @4:7
  store s0, v0 @4:2
  v1 = load s0 @5:10
  v2 = load s0 @5:14
  v3 = neg v2 @5:13
  println v1, v3 @5:2
  ret @6:1
```

**Invariants.** `ir.Verify` checks all of these and reports every violation,
naming the function, block, and instruction:
1. There is exactly one function named `main`, and function names are unique.
2. Every function and slot has a file and a position. Slot IDs equal their
   indices, and slots have type `i64`.
3. A function has at least one block. Block IDs equal their indices. Every
   block except the entry is reported as unreachable, because S1 has no branch
   instructions.
4. Every block ends with exactly one terminator, and no terminator appears
   before the end.
5. Every instruction has a valid operation and a position. Its result type,
   value number, and operand count match its operation, and only `const` has
   a constant and only `load` and `store` have a slot.
6. Every operand is a value-producing instruction defined earlier in the same
   block, of type `i64`. Value numbers are not reused.
7. Every slot operand belongs to the function, and every `load` follows a
   `store` to the same slot.
8. **SSA level** (Phase 5): each value is defined once, and every definition
   dominates its uses. φ-nodes appear only at the start of a block and have one
   operand per predecessor.

With no branches, invariants 6 and 7 are exact dominance and
definite-assignment checks. S2 (D-023, from increment 4.1) keeps invariant 6
block-local (values cross blocks only through slots), requires every block to
be reachable, and replaces invariant 7 with a "must be stored" dataflow over
the CFG. That dataflow starts the entry block at ∅ and every other block at all
slots, and iterates to the greatest fixed point. This section describes the
S1 IR that exists today; D-023 is the contract for each extension until its
increment updates this section.

**What lowering guarantees beyond the verifier.** The verifier checks the
IR's form, not its meaning. Lowering guarantees, and the IR goldens and the
differential tests check, that:
- each `const` holds the exact value `go/types` computed for a constant
  expression;
- each Go variable has its own slot, and every use of it refers to that slot;
- the order of instructions is Go's evaluation order (`SEMANTICS.md` §Subset
  S1);
- every position is the position of the source construct the instruction
  implements.

A verifier failure on lowered IR is an `internal` error.

**Determinism.** Printing the same IR always produces identical bytes. Block
and value numbering never depend on map iteration order.

### 5. Reference interpreter (Aether, D-005)

- `internal/interp` executes IR directly. It imports only `ir`, and its tests
  build IR by hand, so it is checked independently of lowering.
- It verifies the program first and executes nothing if the IR is malformed.
- It exists to give tests an oracle and to locate bugs. It is not a product
  feature, has no performance goals, and has no CLI command (D-021).

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

The **Phase 3 backend** (`internal/amd64`, D-007, D-022) is deliberately
naive. There is no register allocation and no optimization: each IR
instruction maps to a fixed instruction sequence. The output is GNU assembly
in AT&T syntax. The backend consumes only IR: `internal/amd64` imports no
`go/*` package, which a dependency test enforces.

**Frame layout.** `main` is emitted as the global function `main.main`:

```
 8(%rbp)                  return address
 0(%rbp)                  saved %rbp
-8(%rbp) .. -8*S(%rbp)    slots s0 .. s(S-1)
-8*(S+1)(%rbp) ..         values v0 .. v(V-1)
```

- **Size** is `8*(S+V)` rounded up to a multiple of 16, computed with
  checked arithmetic (`amd64.NewFrame`). A frame over 1 GiB is an internal
  error, so offsets always fit a 32-bit displacement.
- **Tuples (increment 4.3).** A call with several results has one extra
  value cell per result, after the ordinary values' cells; they are filled
  from the outgoing area right after the call, and `extract` copies one.
- **Blocks (increment 4.1).** Blocks are emitted in order, each under the
  local label `.L<function index>.b<block ID>`. A comparison is `cmpq` and
  a signed `setcc` into a 0-or-1 cell; `branch` tests its cell and jumps
  to either target.
- **Calls (increment 4.2, ABI-S0, D-011).** A function with calls also
  reserves an outgoing area of `8*max(P+R)` bytes at the bottom of its frame,
  included in Size; its own arguments and result are at `16+8*i(%rbp)`.
  Every prologue, `main.main`'s included, first checks the frame against
  `aether.stacklimit` and jumps to `aether.stackoverflow` if it does not
  fit.
- **Stack.** After the code, the backend emits the read-only 8-byte datum
  `aether.mainstack = 16 + Size`: the bytes `main.main` uses below `_start`'s
  call (return address, saved `%rbp`, frame). The runtime maps a stack that
  large (§9), so a frame never depends on the process stack limit
  (`ulimit -s`). Before this (F1), a frame over the default 8 MiB stack
  crashed with SIGSEGV.
- **Prologue:** `pushq %rbp; movq %rsp, %rbp; subq $Size, %rsp` (no `subq`
  for an empty frame). **Epilogue** (`ret`): `movq %rbp, %rsp; popq %rbp;
  ret`.
- **Alignment.** `_start` calls `main.main` on that stack with `%rsp` 16-byte
  aligned, so it
  is 8 mod 16 on entry, 0 after the push, and stays 0 after subtracting
  `Size`. Every runtime call is therefore made with `%rsp` aligned.
- **Registers.** Generated code uses only `%rax` (scratch) and `%rdi` (helper
  argument), besides `%rbp` and `%rsp`. No value is kept in a register across
  instructions, so nothing needs saving around calls: every value lives in the
  frame, which the helpers do not touch.

**Instruction selection.** `int` is a 64-bit two's-complement value. Every
selected instruction already has Go's wraparound semantics, so no operation
needs a flag check or a special case.

| IR | Assembly |
|---|---|
| `vN = const c` | `movq $c, vN` if `c` fits a sign-extended 32-bit immediate; otherwise `movabsq $c, %rax; movq %rax, vN` |
| `vN = load sK` | `movq sK, %rax; movq %rax, vN` |
| `store sK, vA` | `movq vA, %rax; movq %rax, sK` |
| `vN = add/sub/mul vA, vB` | `movq vA, %rax; addq/subq/imulq vB, %rax; movq %rax, vN` (`imulq` keeps the low 64 bits) |
| `vN = neg vA` | `movq vA, %rax; negq %rax; movq %rax, vN` (`negq` maps `MinInt64` to itself) |
| `println vA, vB, …` | `movq vA, %rdi; call aether.printint; call aether.printsp; movq vB, %rdi; call aether.printint; …; call aether.printnl` |
| `ret` | the epilogue |

Each instruction's code is preceded by a comment holding the IR instruction
and its line and column. The output contains no paths, times, or host data,
and ends with an empty `.note.GNU-stack` section (no executable stack).

**Later:**
- Phase 4: control flow and `bool` first (increment 4.1, D-023), with block
  labels and compare-and-branch sequences. From increment 4.2, calls use the
  accepted stack convention ABI-S0 (D-011): arguments and results in a
  caller-owned outgoing area at the bottom of the frame, and a stack-bound
  check before every prologue's `pushq %rbp`. The outgoing area counts in the
  frame size, which stays at most 1 GiB.
- Phase 5: instruction selection over SSA, register allocation (algorithm
  not chosen), a register-based successor to the Phase 4 convention, and a
  frame layout that reserves room for future stack maps (D-010).

### 8. Assembly and linking (GNU host tools, D-007, D-008)

GNU `as` and `ld` are accepted as initial and potentially long-term
dependencies (D-008).

**What each side does.**
- **Aether owns:** instruction selection, register allocation, stack layout,
  assembly emission, and the semantics of the generated program.
- **`as`:** translates Aether's assembly into ELF relocatable objects.
- **`ld`:** constructs the static executable from Aether's program object and
  runtime objects.

**Commands.** In the work directory, with the environment `LC_ALL=C` only:

```
as --64 -o prog.o prog.s
as --64 -o print.o print.s
as --64 -o start.o start.s
ld -static -nostdlib -e _start -o a.out prog.o print.o start.o
```

The runtime files are processed in name order, and nothing else is linked.
Only these fixed names are passed to the tools; the user's paths never are,
so none can be read as an option.

**Driver requirements** (`internal/toolchain`):
- **Availability and identity check before use.**
  - Locate each tool on `PATH`, which is also the override mechanism
    (D-022), and resolve it to an absolute path.
  - Confirm it is the GNU tool: the first line of `as --version` starts with
    `GNU assembler`, and that of `ld -V` with `GNU ld`.
  - Confirm it can target x86-64 ELF: `as --version` names an `x86_64-`
    target, and `ld -V` lists the `elf_x86_64` emulation.
  - `aether build` does not run the executable, so it does not check the
    host; the tests that run executables skip on hosts other than
    linux/amd64.
- **Clear errors.** Any failure is a `tool` diagnostic naming:
  - the tool
  - the path used or searched
  - the version line, if one was obtained
  - the tool's stderr
- **Process choke point.** This is the only package that starts processes.
  A test enforces that, among Aether's packages, only `internal/toolchain`
  imports `os/exec` or `syscall`, or calls `os.StartProcess`. It also uses
  `go list -deps` to
  check that, apart from paths through `internal/toolchain`, no Aether
  package reaches `os/exec` transitively, for example through `go/importer`
  (E9).
- **Fixed tool environment.** Tools run with an explicit environment
  containing only `LC_ALL=C`, which E9 showed is sufficient. They run by
  absolute path, with the work directory as the current directory. The
  invoking user's environment therefore cannot change tool behavior, and tool
  messages are stable.
- **Command log.** With `-x`, the driver logs to stderr:
  - every command it runs, including the two identification commands, as
    `LC_ALL=C /abs/path args…`
  - each tool's version line, as `# as version: …`
  - the work directory, as `cd DIR`

  The log is supporting provenance evidence. The primary checks reproduce the
  build independently (`ROADMAP.md` §Milestone 1).
- **Work directory layout.** The work directory is a fresh temporary directory
  per build. It contains:
  - `prog.s` (the program assembly) and `prog.o`
  - one `.s`/`.o` pair per runtime file (`print.s`, `start.s`)
  - the link output, `a.out`

  It is created only after the program has been accepted and emitted. It is
  removed after the build, whether it succeeded or failed, unless `-work` is
  given, in which case `WORK=DIR` is printed to stderr and the directory is
  kept, also after a failure.
- **Output handling.**
  - `ld` writes `a.out` in the work directory.
  - Only after every step has succeeded is it copied to a temporary file next
    to the output path, made mode 0755, and renamed over the output path.
  - On any failure the output path is neither created nor modified. A file
    already there is left untouched, and the temporary file is removed.
  - Before building, an output path that is a directory, a symbolic link
    (even to a missing file), another non-regular file such as a FIFO, or the
    input file itself (also through a hard link, or an input that is a
    symbolic link to the output) is refused (exit 2).
  - **Limits.** The output check runs before the build, so a change to the
    path made concurrently is not detected (time of check to time of use).
    The final `rename` never writes through a symbolic link and fails on a
    directory, but it would replace a regular file or a hard link created in
    the meantime. Cleanup runs on every error path in the process, not when
    the process is killed by a signal, which can leave the work directory or
    a `.aether-out-*` file behind. The output is not `fsync`ed, so publishing
    is atomic for concurrent readers but not durable across a crash.
- **Reproducibility** (D-008):
  - fixed argument lists, a fixed tool environment, and fixed relative file
    names inside a fresh work directory
  - no absolute or temporary paths, timestamps, or build-ids in the generated
    assembly or the linker options

  Identical inputs and configuration give byte-identical output. The
  work-directory path and the output file name do not affect the bytes (E6,
  E9).

### 9. Runtime (Aether, D-009)

- **Phase 3 (implemented):** hand-written GNU assembly:
  - `rt/start.s`: `_start` clears `%rbp`, maps the main stack, switches
    `%rsp` to its top (page-aligned), calls `main.main`, then
    `exit_group(0)`.
    - The stack is `aether.mainstack` bytes plus a 4096-byte reserve for the
      helpers (which use at most 56 bytes below `%rsp`), rounded up to whole
      pages, with a `PROT_NONE` guard page below (`mmap`, then `mprotect`).
    - If either call fails, `_start` writes `fatal error: cannot allocate the
      main stack` to fd 2 and exits with status 2 (`SEMANTICS.md`, D-022).
  - `rt/print.s`: `aether.printint`, `aether.printsp`, `aether.printnl`.
    - They write to fd 2 with raw `write` syscalls, through a local routine
      (`aether.write`) that continues short writes, retries `EINTR`, and
      gives up silently on any other error or a write that makes no progress
      (D-009, E7).
    - `aether.printint` formats into a 32-byte buffer on its stack. It
      converts the value to a non-positive number and divides by 10 with
      `idivq`, so `MinInt64` is never negated.
  - The only global symbols are `_start`, the three helpers, and
    `aether.write`; internal labels are local (`.L…`).
- **Phase 4:** `aether.printbool` (increment 4.1). From increment 4.2
  (implemented in the runtime and backend; the gate does not yet accept
  functions), a stack of at least 256 MiB mapped with the Phase 3 flags, a
  software stack limit `L` in `aether.stacklimit`, stored by `_start` before
  it calls `main.main`, and `aether.stackoverflow` (D-009 Phase 4
  amendment).
  From 4.5, the minimal panic exit (status 2); from 4.6, `_start` calls
  `main.init` before `main.main`. Each symbol enters the boundary table below
  when it is implemented.
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
| `_start` | runtime | Process entry. Maps a stack of max(256 MiB, `aether.mainstack`) bytes, stores its limit in `aether.stacklimit`, calls `main.main` on it, then exits 0. Exits 2 with a fatal error if the stack cannot be mapped. |
| `aether.mainstack` | compiler | Read-only 8-byte datum: the bytes `main.main` uses below `_start`'s call, excluding the helpers' own use. |
| `main.main` | compiler | Aether-compiled `main`. No arguments, no results. Called with `rsp` 16-byte aligned before `call`. |
| `main.<name>` | compiler | Every other Aether-compiled function, called only from compiled code under ABI-S0 (D-011). |
| `aether.stacklimit` | runtime | Writable 8-byte word: the stack limit `L` every prologue checks against (D-009, D-011). |
| `aether.stackoverflow` | runtime | Entered by `jb` from a prologue, with `%rsp` at least `L` − 8. Writes `fatal error: stack overflow` to fd 2 and exits 2; uses at most 24 bytes below `L`. |
| `aether.printint`, `aether.printsp`, `aether.printnl` | runtime | Print one `int64` (in `%rdi`) in decimal / a space / a newline to fd 2. No result; `%rsp` 16-byte aligned before `call`; the helper preserves `%rbx`, `%rbp`, `%r12`–`%r15` and may clobber every other general-purpose register (a subset of the System V convention). Write errors other than `EINTR` are ignored. This interface covers only runtime helpers. It does not decide D-011 (D-022). |
| `aether.printbool` | runtime | Prints `true` if `%rdi` is 1 and `false` if it is 0, to fd 2, under the same interface as `aether.printint`. Uses 8 bytes below `%rsp` (its return address). |
| `aether.write` | runtime | Writes `%rdx` bytes at `%rsi` to fd 2 under the same rules. For the runtime's own use (`_start`'s fatal error); generated code does not call it. Clobbers `%rax`, `%rcx`, `%rdx`, `%rsi`, `%rdi`, `%r11`. |

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

**Implemented in Phase 1:**
- `cmd/aether`: `check`, plus the stage ordering in `check.go`.
- `internal/diag`
- `internal/frontend`
- `internal/gate`

**Implemented in Phase 2:**
- `internal/ir`
- `internal/lower`
- `internal/interp`

**Implemented in Phase 3:**
- `internal/amd64`, with the runtime in `internal/amd64/rt/*.s`
- `internal/toolchain`
- `cmd/aether`: `build`, in `build.go`

The other rows are still candidates.

| Candidate path | Contents | Depends on | Stability |
|---|---|---|---|
| `cmd/aether` | CLI driver (`aether build`, `aether check`) | everything below | CLI flags and exit codes are a contract |
| `internal/diag` | Diagnostic type, classes, rendering, exit codes | — (no `go/*`) | stable within the repo |
| `internal/frontend` | Load, parse, type-check; produce the checked package | `go/*`, `diag` | internal |
| `internal/gate` | Subset gate | `frontend`, `diag` | internal |
| `internal/ir` | IR types, builder, verifier, printer | — (no `go/*`) | **contract:** text format + invariants |
| `internal/lower` | Typed AST → IR | `frontend`, `ir`, `diag` | internal |
| `internal/interp` | IR interpreter | `ir` | internal |
| `internal/opt` | Optimization passes (Phase 5+) | `ir` | internal |
| `internal/amd64` | isel, frames, regalloc, asm emission; runtime sources in `internal/amd64/rt/*.s`, embedded (§9) | `ir` | internal |
| `internal/toolchain` | Locate and run `as` and `ld` | — | internal |

Dependency rules. They are enforced by `cmd/aether/deps_test.go` on the import
graph from `go list -deps`, for each package once it exists. They govern
compiler code, not test files. Tests may run processes, for example `go list`,
the built `aether` binary, and gc as the oracle.
- `diag`, `ir`, and `amd64` must not import any `go/*` package. The backend
  sees only IR.
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
- **Rendering:** `file:line:col: <class>: <message>`, followed by
  ` [feature]` for `unsupported`, one line each, sorted by position,
  deterministic (D-018).
- **Driver ordering:** `cmd/aether/check.go` runs the stages in the D-003
  order. That order is the only place the precedence is encoded.
- **Propagation:**
  - Each stage returns its diagnostics.
  - A stage runs only if all earlier stages produced no errors.
  - Stages after the gate never produce `invalid` or `unsupported`.
  - Within one stage, errors are collected rather than stopping at the first,
    where practical. `go/types` supports this through `Config.Error`.
  - Panics inside Aether are recovered at the driver and reported as `internal`.
- **No output on failure:** no file is created at the output path on any error.
  This covers every class, including `tool`. Gate and frontend failures stop
  before any `.s` or `.o` is produced, and before any tool runs. Publishing
  the executable is atomic, and an existing file at the output path is left
  unchanged on failure (§8).

**CLI exit codes** (finalized in Phase 1, D-018; streams and usage errors are
specified there):

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | `invalid` |
| 2 | usage error; for `build`, also an unwritable work directory or output (D-022) |
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
- The register-based successor to ABI-S0 (Phase 5, D-011)
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
