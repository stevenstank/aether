# Aether Testing Strategy

Status: **strategy only.** No test infrastructure exists yet. Governing
decisions: D-003 (diagnostic classes), D-005 (interpreter oracle), D-014 (gc as
oracle).

## Principles

1. **Never silently miscompile.** Every test layer exists to catch accepted
   programs that behave incorrectly.
2. **The spec is normative; gc is the oracle.** Differential tests compare
   against gc. Where the spec leaves behavior unspecified, tests must not
   depend on gc's particular choice (`SEMANTICS.md` §Evaluation order).
3. **Every bug gets a regression test** before or with its fix.
4. **Tests are deterministic.** No dependence on time, map iteration order,
   scheduling, or host paths in compared output.
5. **Coverage is a smell detector, not a proof.** Coverage reports are used to
   find untested code. No coverage percentage counts as evidence of
   correctness, and none is required as a merge gate.

## Distinguishing compiler bugs from subset limitations

Every test program declares its expected outcome in a header comment. The
format, finalized in Phase 1, is:

```go
// aether:expect run
// aether:expect unsupported <feature>
// aether:expect invalid
// aether:gc valid|invalid|skip <reason>   (optional)
```

- `<feature>` is a name from the `SEMANTICS.md` feature table.
- The `aether:gc` line records what the official toolchain does with the file
  when that differs from the default: valid Go unless the file expects
  `invalid`. For example, a file that is `unsupported` because of an import,
  but also contains a type error, is `// aether:gc invalid`.
- `run` means the program is accepted by `aether check` and, since Phase 2,
  that its interpreted and native behavior equal gc's
  (`TestDifferentialAgainstGc`). The
  corpus is `cmd/aether/testdata/check/*.go`. Each file has a reviewed
  `.golden` file holding the exact stderr of `aether check`, and each `run`
  file also has a reviewed `.ir` file holding its lowered IR
  (`TestLowerCorpus`). Both are regenerated with
  `go test ./cmd/aether -run 'TestCheckCorpus|TestLowerCorpus' -update`.
- `TestCorpusAgainstGc` builds every corpus file with gc to confirm its
  declared validity.
- `TestFeatureTableMatchesGate` (`internal/gate`) checks that the gate's
  feature-name registry (`gate.Features()`) and the `SEMANTICS.md` feature
  table name exactly the same features.
  - It reports each name missing from either side.
  - The gate turns a rejection with an unregistered name into an `internal`
    error, so the registry cannot fall behind the code.

The harness classifies actual outcomes as follows:

| Expected | Actual | Classification |
|---|---|---|
| `run` | matches oracle | pass |
| `run` | different output or exit status | **bug** (miscompilation) |
| `run` | `unsupported` | **bug** if the feature is in the support matrix; otherwise the test's expectation is wrong |
| `run` | `internal` (exit 4) or Aether crash | **bug** |
| `unsupported X` | `unsupported X` | pass (a known limitation) |
| `unsupported X` | accepted | **bug** if X is not in the matrix (the gate leaks); otherwise update the test |
| `invalid` | `invalid` | pass |
| `invalid` | accepted or `unsupported` | **bug** (the frontend or gate is misclassifying) |
| any | `tool` (exit 5) | environment problem; reported, not counted as pass |

**Rule:** an `internal` error is always a bug, even when it is a "graceful"
refusal.

**Precedence:** classification follows D-003. A program containing an import is
`unsupported` even if it also has a type error, so such a program must not be
labeled `// aether:expect invalid`.

## Test layers

### Unit tests

Ordinary `go test` tests, one package per stage:
- **frontend:** configuration, error conversion
- **gate:** each rule
- **IR:** builder, verifier (including negative tests on malformed IR),
  printer
- **lowering:** each construct
- **interpreter:** each instruction
- **backend:** instruction selection for each IR op, frame layout, assembly
  syntax
- **toolchain:** missing-tool errors
- **dependency rules** (`ARCHITECTURE.md` §Candidate package boundaries):
  - `ir` and `amd64` import no `go/*` package
  - `os/exec` enters the dependency graph only through `toolchain`
    (`go list -deps`)
  - nothing imports `go/importer` or `go/build`

### Golden tests

Expected output is stored in `testdata` files and updated through an explicit
flag (`-update`). Golden-file diffs are reviewed like code. They cover:
- **Diagnostics:** rendered output for invalid and unsupported programs. This
  covers positions, classes, and deterministic ordering.
- **IR:** printer output after lowering, and after each optimization pass from
  Phase 5.
- **Assembly:** used sparingly. Assembly goldens are brittle, so they are kept
  for a few representative cases. Behavior tests remain primary.

### End-to-end tests

The full pipeline runs: source → `aether build` → execute → compare.

Implemented in Phase 3: `TestBuildCorpus` builds and runs every corpus
program expected to run, and `TestDifferentialAgainstGc` every differential
program. They run only when GNU `as` and `ld` are found on a linux/amd64
host. Otherwise they are **skipped with a visible
message naming the missing or unidentified tool**, never silently passed.

`TestBuildLargeFrame` (F1 regression) builds a generated S1 program with a
frame over 8 MiB and runs it, and a small corpus program, under `ulimit -s
8192` and `ulimit -s 1024`; both must produce their exact output and exit 0,
and a signal fails the test. `TestRuntimeStack` (`internal/amd64`) runs
hand-written `main.main` frames of 64 bytes, 64 MiB, and the 1 GiB maximum
under reduced stack limits, and checks that an unmappable stack (`ulimit -v`)
gives the V-1 fatal error and exit 2. Both were checked against two negative
controls: a runtime that ignores `aether.mainstack`, and one that keeps the
process stack. Each control made both tests fail with SIGSEGV.
`TestRuntimeStackGuard` writes at the lowest addresses the layout guarantees
and in the guard page below, and checks the stack top is page-aligned; a
runtime without the guard page or without the helper reserve fails it.

Output-safety tests (`TestBuildOutputRefusals`, `TestBuildCleanup`,
`TestBuildFileFailures`) cover:
- refused outputs: a symlink to an existing or a missing file, a FIFO, a hard
  link to the input, and an input that is a symlink to the output; the
  directory must be unchanged afterwards
- an empty `TMPDIR` after an `as` or `ld` failure, and `-work` keeping the
  work directory of a failed build
- exit 2 with the existing output unchanged when the work directory cannot
  be created (missing `TMPDIR`) or the output directory is not writable
  (skipped as root)

Negative controls (removing the regular-file check, the same-file check, or
the work-directory cleanup) each made the matching tests fail. Races on the
output path, interruption by a signal, and a failing final `rename` are not
tested (`ARCHITECTURE.md` §8, Limits).

Separate driver tests cover the tool-failure paths with stand-in tools on
`PATH` (missing, non-GNU, failing). Each must give a `tool` diagnostic, exit 5,
and no output file.

### Differential testing against gc

For each `// aether:expect run` program:
1. `go build -o ref prog.go`, then execute `ref`. Never use `go run`: it
   rewrites the exit status (E1).
2. Run the Aether interpreter (from Phase 2) and the Aether-built executable
   (from Phase 3). `TestDifferentialAgainstGc` (`cmd/aether`) builds every
   program with gc in one `go build` of a temporary module outside the
   repository, with `GOTOOLCHAIN=local`, and builds each with
   `aether build` in-process. Without GNU `as`/`ld`, the native comparison is
   skipped with a logged message.
3. Compare stdout, stderr, and exit status across all three.
4. Apply normalization only for divergences listed in `SEMANTICS.md` §Known
   divergences. Example from Phase 4: for panicking programs, Aether's stderr
   must equal gc's up to and including the first line that begins with
   `panic: `, and both must exit with status 2 (`ROADMAP.md` Phase 4).

The S1 suite starts with the verified cases S1-01 to S1-10 and S1-N01 to
S1-N05 (`SEMANTICS.md` §Subset S1). The expected outputs recorded there were
observed from gc during Phase 0. The tests still obtain the expected result
from gc at test time, and the recorded values serve only as a cross-check that
the oracle has not changed.

The gc version used is recorded in test logs. Programs that use unspecified
behavior are marked `// aether:unspecified` and either excluded or checked
against a list of every allowed outcome.

**Generated programs (Phase 2).** `generate` (`cmd/aether/gen_test.go`)
derives a random S1 program from a seed. It uses every S1 construct and
literals that make arithmetic wrap. `TestDifferentialAgainstGc` runs the
first 200 generated programs that type-check. A generated constant expression
may overflow `int`: Aether must classify such a program `invalid` with an
overflow message, and one `go build` of all of them must show gc rejecting
each for the same reason ("overflows", or "constant multiplication overflow"
for a constant beyond gc's untyped precision limit). Any other rejection fails the test. Negative
controls confirmed that it detects a wrong `neg` of the minimum in the
interpreter, tuple assignments lowered with reversed or non-simultaneous
stores, and a backend multiply truncated to 32 bits.

Since increment 4.1 the generator also emits `bool` variables and
constants, comparisons with negated and boundary operands, `!`, `&&`, `||`,
`if`/`else` chains, and loops in all three `for` forms with `break` and
`continue`. Every loop runs at most 3 times: its counter is never assigned,
and it advances before the body or in the post statement. About 40% of
seeds give a program with an overflowing constant (20% before 4.1, since
programs are now longer); the test tries at most 600 seeds. Negative
controls confirmed that it detects `||` lowered as `&&`, and a `continue`
that skips the post statement, which the interpreter's step budget stops
instead of hanging.

Since increment 4.4 it also declares variables of every integer type and
uses conversions between them, `&`, `|`, `^`, `&^`, unary `^`, their
compound assignments, and `print`. Each integer expression is generated
for a type and knows whether it is constant: a constant is never negated or
complemented in an unsigned type, only variables are converted, and two
constant operands in a type other than `int` are combined only bitwise,
since anything else would be invalid for a reason other than overflow or
would mostly overflow. About 36% of seeds now give an invalid program.
Negative controls confirmed that it detects conversions lowered as no-ops
and `&^` lowered as `&`.

Since increment 4.5 it also uses `/`, `%`, `<<`, `>>`, and their compound
assignments. A constant divisor is a nonzero literal; a variable one is
usually or-ed with 1 and otherwise any expression, which may be zero. A
constant shift count is a nonnegative literal; a variable one is an
expression of any integer type, which may be negative or at least the
width. A constant is shifted only by a constant, since with a variable
count its type would come from the context. So some programs panic and are
compared by the panic rule: of seeds 1 to 600, 409 were valid, 15 of them
panicking (13 dividing by zero, 2 with a negative shift count), and about
32% of seeds gave an invalid program (38% before). Negative controls
confirmed that generated programs detect `shrq` for signed `>>` in the
backend, a missing negative-count check in lowering, and unsigned values
divided as signed in the interpreter.

Since increment 4.6 a program may also begin with up to four package-level
`var` and `const` declarations, generated like local ones. Each initializer
may use the declarations generated before it, so there is no cycle, and the
declarations are then printed in shuffled order, so that the
initialization order (`InitOrder`) can differ from the declaration order.
`main` assigns globals like locals, local declarations may shadow them, and
`main` prints every global at its end. There is no other function, so no
call assigns a global that an expression is reading (V-4). Of seeds 1 to
600, 379 were valid (37% invalid, 32% before), 257 of them with globals
and 27 initializing in an order other than the declaration order; 35
panicked. Negative controls confirmed that generated programs detect an
initialization order reversed in lowering (11 programs) and globals that
share one word in the backend (85 programs).

Since increment 4.7 a statement may be a `switch`: on a `bool` or integer
tag, on a variable declared by its init statement, or without a tag, with
up to three case clauses of one to three values, an optional `default` in
any position, and short bodies of zero to three statements, which may
begin with a conditional `break`. go/types rejects duplicate integer
constants among case values, so each constant integer case value is a
literal not used before in that switch, chosen to fit every integer type.
Case values may divide by zero, so a panic can depend on which values are
evaluated. Of seeds 1 to 600, 345 were valid (43% invalid, 37% before, all
from overflowing constants); 159 of the valid ones contain a switch, 24 of
those panic. Negative controls in lowering, run by hand and reverted:
`default` taken in source order (20 generated programs fail), only the
first value of each case tested (10), and `break` leaving the enclosing
`for` (2 generated programs give wrong output, and 59 fail lowering with an
internal error because no loop encloses the switch).

Since Phase 6 increment 6.3, a fourth random stream (`gen_agg_test.go`)
declares up to three array or struct types, of scalars and the types before
them, with blank fields and lengths of 0 to 33 (17 and 33 exceed the
lowering's unrolling, so copies and comparisons loop); a function per type
that changes its parameter's copy and returns it; and up to two globals.
Statements on them, at most `maxAggStmts` (30) per program so that `main`
stays within the limits on promotion, are declarations, whole and tuple
assignments, stores to and arithmetic on fields and elements, comparisons,
calls whose argument is printed beside the result, and prints. An element is
selected by constant indices, or by a variable index guarded by a test of the
length, or unguarded: rarely by any variable, and often, inside a loop, by
`c + n-2` for the loop counter `c` and the array's length `n`, which reaches
the first index out of range on the loop's last iteration. On 2026-11-13, of
seeds 1001 to 3000, 1,162 were valid, 878 of them with aggregates; 126 of
those panic, 60 with an index out of range. All 878 gave gc's output in the
interpreter, `aether build`, and `-O0`. Negative controls in lowering, run by
hand and reverted, against `TestDifferentialAgainstGc`: a whole variable read
without a copy (11 of its 200 generated programs and 2 corpus programs fail)
and a bounds check off by one (3 generated programs and 1 corpus program).

`TestLayoutAgainstGc` checks the layout of arrays and structs (D-034): a
gc-built program prints `unsafe.Sizeof`, `Alignof`, and `Offsetof` of 18
types chosen for padding, nesting, embedding, blank and zero-size fields,
and an array element's offset, and each must equal what Aether's emitted IR
uses: a global's memory object, and the lowest offset at which a function
storing one field writes into it. Lowering the 386 layout instead fails it.

#### The S2 suite

The S2 suite is the evidence for Phase 4's acceptance criteria
(`ROADMAP.md`). It is the full test run, `go test -count=1 ./...`, without
`-short`, so that no gc-building test is skipped. Its parts:
- **Corpus against gc:** every `run` program of the corpus, the 22 `s2_*`
  programs among them, built by gc and compared with the Aether-built
  executable and the interpreter (`TestDifferentialAgainstGc`); exit 0
  programs compare stdout, stderr, and status exactly, and panicking ones by
  the panic rule (V-3). The `s2_*` programs cover each increment's
  features, and `s2_a01_algorithms` and `s2_a02_state_machines` (a
  bytecode interpreter and automata driven by `switch`) combine them in
  ordinary programs.
- **Generated programs:** 200 programs from `generate`, compared the same
  way.
- **Stack overflow:** `TestStackOverflowAgainstGc`, by the overflow rule
  (V-2), on five programs, one of them overflowing during package
  initialization.
- **Negative controls** (`TestNegativeControls`, `cmd/aether/controls_test.go`):
  the Phase 4 controls first run by hand, 29 of them, each applied as a
  text replacement to its own copy of the module, never to the working
  tree. The tests named for each control must fail, with output matching
  the control's patterns; a copy that does not build fails the control
  instead of counting as detected. An unmodified copy must pass the same
  tests. The control text was not recorded when the controls were run by
  hand; each was rebuilt from its description. Two detect differently than
  recorded: unsigned comparison as signed in the interpreter is caught by
  `TestIntegerTypes` in `internal/amd64`, not `TestComparisons`; and
  unsigned division as signed is the variant that sign-extends each
  operand at its width (a plain signed division of zero-extended values
  differs only for 64-bit values with the high bit set, which no generated
  program divides). The owner accepted both, since the intended defects
  are detected (2026-11-03). Skipped under `-short`; about 2 minutes.
  Each control and what detects it:

  | Control | Detected by |
  |---|---|
  | 4.1 branch targets swapped; `setb` for signed `<`; `printbool` printing `1` | `TestNativeControlFlow` |
  | 4.1 definite assignment on any path | `TestVerifyControlFlow` (store on one path only; store only inside the loop body) |
  | 4.1 `\|\|` lowered as `&&` | `TestDifferentialAgainstGc`, generated programs, interpreter and native |
  | 4.1 `continue` skips the post statement | `TestDifferentialAgainstGc` with GNU `as`/`ld` hidden: the interpreter's step budget |
  | 4.2 prologue without `jb`; `L` never stored | `TestNativeStackOverflow`, `TestStackBoundary`, `TestFrameLargerThanStack` (killed by a signal) |
  | 4.2 stack mapped with `MAP_NORESERVE` | `TestStackMapping` |
  | 4.2 overflow message changed, native; in the interpreter | `TestStackOverflowAgainstGc`, native; interpreter |
  | 4.3 `extract` reads the outgoing area | `TestNativeTuples` |
  | 4.4 no canonicalization; signed condition codes for unsigned types; `printint` for unsigned values | `TestIntegerTypes` |
  | 4.4 interpreter compares unsigned values as signed | `TestIntegerTypes` (`internal/amd64`), interpreter side |
  | 4.4 conversion executed as a no-op (interpreter) | `TestDifferentialAgainstGc`, generated programs |
  | 4.4 `&^` lowered as `&` | `TestDifferentialAgainstGc`, generated programs, interpreter and native |
  | 4.5 `idivq` for every divisor; no width check on shifts; zero instead of the sign fill | `TestIntegerTypes` |
  | 4.5 interpreter divides unsigned values as signed (at their width) | `TestDifferentialAgainstGc`: `s2_d01_division_shifts` and generated programs |
  | 4.5 `shrq` for signed `>>` | `TestDifferentialAgainstGc`, generated programs, native |
  | 4.5 no negative-count check | `TestDifferentialAgainstGc`, generated programs, interpreter and native |
  | 4.6 initialization order reversed | `TestDifferentialAgainstGc`: `s2_g02_init_order` and generated programs |
  | 4.6 globals share one word | `TestDifferentialAgainstGc`, generated programs, native |
  | 4.7 `break` leaves the enclosing `for` | `TestDifferentialAgainstGc`: `s2_s01_switch` and generated programs |
  | 4.7 tag evaluated for each case value | `TestDifferentialAgainstGc`: `s2_s01_switch` |
  | 4.7 `default` taken in source order | `TestDifferentialAgainstGc`: `s2_s01_switch` and generated programs |
  | 4.7 only the first value of each case tested | `TestDifferentialAgainstGc`, generated programs |
- **Goldens and the gate:** the IR goldens of the `run` programs
  (`TestLowerCorpus`), the classification corpus (`TestCheckCorpus`), and
  gc's agreement with each file's `valid` or `invalid` header
  (`TestCorpusAgainstGc`).
- **Hand-built IR:** the verifier, interpreter, and backend tests in
  `internal/ir`, `internal/interp`, and `internal/amd64`, including the
  stack mapping, boundary, and reserve tests (D-009, D-011).
- **Milestone 1:** P1–P6 (`TestMilestone1`, `TestMilestone1Rejections`,
  `TestMilestone1Determinism`), rerun because the runtime and the generated
  code changed.

Fuzzing (`FuzzCheck`, `FuzzGenerated`) is run separately after changes, as
described below.

Run on 2026-11-03 (go1.26.1, GNU as and ld 2.42, Linux x86-64 under WSL2):
exit 0 in 2 min 49 s, 880 passing tests and subtests, none failing or
skipped; 34 corpus programs and 200 generated programs (148 seeds skipped
as invalid Go) agreed with gc natively and in the interpreter, and every
negative control was detected.

From Phase 4 on, **generated differential tests** cover operator × type ×
edge-value tables (0, ±1, min, max, powers of two), and their expected outputs
come from running gc.

### Fuzz testing

Native Go fuzzing (`testing.F`):
- **Frontend and gate** (Phase 1): arbitrary bytes produce no panic and no
  `internal` diagnostic.
- **Lowering and verifier** (Phase 2): since Phase 2, `aether check` lowers
  and verifies every accepted program, and `FuzzCheck` also interprets it.
  Every gate-accepted input must lower to verifier-clean IR that the
  interpreter runs. Most `FuzzCheck` inputs are rejected before lowering, so
  `FuzzGenerated` fuzzes the generator's seed instead: nearly every input
  reaches lowering, the verifier, and the interpreter. Since Phase 5 (D-006
  part 3), it also promotes each program to SSA form and requires the same
  interpreter results.
- **Program generation** (Phase 2+): a generator of random *valid* subset
  programs, cross-checked between gc, the interpreter, and native code. In
  Phase 2 it is cross-checked between gc and the interpreter. Any
  disagreement becomes a regression test.
- **IR transformations** (Phase 5): random IR or random programs, where each
  pass must preserve both verifier validity and interpreter-observed behavior.

The fuzz corpus is committed when it finds a bug. Phase 1's `FuzzCheck`
(`cmd/aether/fuzz_test.go`) found one: `func main()` without a body (E12). Its
input is kept in `cmd/aether/testdata/fuzz/FuzzCheck/`, so plain `go test`
replays it.

**Fuzz output must be captured in full** to a log file, with the exit status
recorded. It must never be piped through `head` or `tail` alone. The incident
below was left unexplained because its output was truncated.

#### Fuzz incident FI-1 (closed 2026-11-03: unexplained, not reproduced)

**What is known:**
- **The run.** On 2026-10-24 a 180 s run of
  `go test ./cmd/aether -run '^$' -fuzz '^FuzzCheck$' -fuzztime 180s`
  (16 workers, go1.26.1) ended with `FAIL`, `exit status 1`, and
  `FAIL github.com/stevenstank/aether/cmd/aether 181.327s`.
- **Lost output.** Its output was piped through `tail -3`, so the failure
  message is lost. No log was saved.
- **No failing input.** No input was written to `testdata/fuzz/FuzzCheck/`.
- **It failed at the end, not mid-run.** File timestamps in the fuzz cache
  (`$GOCACHE/fuzz/.../FuzzCheck`) show the run adding new interesting inputs
  steadily until about 181 s after it started, then nothing until the next
  run.
- **No out-of-memory evidence.** `dmesg` shows no OOM-killer events; about
  9 GB was available.

**What the Go fuzzing engine allows** (`internal/fuzz/worker.go`, go1.26.1):
- **An assertion failure or panic** in the fuzz function on some input is
  returned as a crasher. The engine stops at once and writes the input to
  `testdata/fuzz`. Neither happened, so FI-1 was **not** an ordinary property
  failure found mid-run.
- **A slow or hung input** at the end of a run gets SIGINT, then SIGKILL, and
  the resulting error is suppressed. This does not explain FI-1.
- **Two paths fail without recording an input:**
  - At the end of a run (`ctx.Done`), a worker that exits with a non-zero
    status within 1 s of being told to stop, without being interrupted, makes
    the run fail. FI-1's timing matches this path.
  - A worker killed by a non-crash signal such as SIGKILL also fails the run
    without recording an input.

**Reproduction attempts, all with full output capture:**

| Runs | Executions | Shutdowns | Failures |
|---|---|---|---|
| 120 s, 180 s, and 420 s runs, after the E12 fix | about 54 M | 3 | 0 |
| 24 runs of 8 s (closure audit) | 19.3 M | 24 | 0 |

The 8 s runs targeted the end-of-run shutdown phase specifically.

**Remaining uncertainty.** The cause is unknown.
- **Most likely:** a fuzz worker exited non-zero during end-of-run shutdown.
  That could be environmental, such as a worker or pipe failure under WSL2,
  or an artifact of the engine.
- **Not excluded:** a genuine property failure on the one input in flight
  during the 1 s shutdown window. In that case the input could not be
  recorded.
  - This is improbable: the same property, no `internal` diagnostic and no
    panic, held for every one of the roughly 73 M executions run since the
    E12 fix, before and after FI-1.
- **Assessment.** The incident does not block later phases. The passing runs
  show the current implementation holds the property under new fuzzing. They
  do **not** explain FI-1.
- **Closing it requires** a recurrence with full output captured.

**Phase 4 closure investigation (2026-11-03):**
- **The exact conditions are recoverable.** The session transcript holds
  the command: `go test ./... -count=3 2>&1 | tail -4 && go test
  ./cmd/aether/ -run '^$' -fuzz '^FuzzCheck$' -fuzztime 180s 2>&1 | tail
  -3`, started 2026-10-24 13:02:13 UTC. Its full surviving output is the
  four `ok` lines of the first command, then `FAIL`, `exit status 1`, and
  `FAIL github.com/stevenstank/aether/cmd/aether 181.327s`. No Go file
  changed between the run and commit `83461ef` (only documentation did),
  so that commit holds the code that was fuzzed.
- **Replay.** All 2,976 inputs in the fuzz cache, 2,630 of them from
  2026-10-24 and 433 added during FI-1's own run, passed as seeds on
  `83461ef` (3,073 inputs with the corpus) and on the current code
  (3,076).
- **Rerun under the original conditions:** code of `83461ef`, the same
  compound command, and a fuzz cache restored to the 494 entries that
  existed before FI-1 started, with full output. Three runs, each exit 0:
  10.7 M, 11.1 M, and 9.3 M executions, no failing input written.
- **Not recoverable:** the failure message, which `tail -3` discarded; the
  input in flight, if any; and the host state at the time.
- **Disposition (owner decision, 2026-11-03): closed as investigated,
  unexplained, and not reproduced.** It is not fixed: the original failure
  was not recovered. All 2,976 cached inputs passed, and three reruns of
  the recovered original command exited successfully. The evidence is
  consistent with the end-of-run worker failure described above but
  cannot exclude a property failure on the input in flight. FI-1 is
  reopened if it recurs, and fuzz output must still be captured in full.

### Regression tests

Each discovered bug adds a minimized program under a regression directory,
named after the issue or a short description, and run through the
differential harness.

### Invalid programs and edge cases

- **Invalid programs:** expected to be classified `invalid` with the parser or
  checker message. Aether does not compare its wording with `go build`'s,
  because the classification is what matters.
- **Edge cases:**
  - integer extremes and wraparound
  - `-MinInt`
  - `MinInt / -1`, division by zero (Phase 4)
  - shift counts ≥ width and negative (Phase 4)
  - shadowing and short-variable redeclaration
  - empty `main`
  - `println()` with no arguments
  - valid Go that `go/types` accepts but the subset rejects (`var x int = 1.0`,
    `println('a')`)
  - `package main` without `func main`, which `go/types` does not report
- Go has no undefined behavior in the C sense for these cases. Tests assert the
  spec-defined result. Any optimization that assumes otherwise is a bug.

### Runtime correctness tests

- Runtime routines are tested in isolation where possible, for example integer
  formatting of every edge value through a tiny Aether-built program compared
  against gc.
- Panic exit codes, output streams (stderr vs. stdout), and the process exit
  path are tested explicitly.
- Later phases add allocator, GC, and scheduler stress tests (`ROADMAP.md`).

### Optimization cross-checks (Phase 5+)

- D-006 experiment (part 4): `TestNativeSSASuite` builds every S2-suite
  program both as lowered and with every function promoted to SSA. Both
  executables must match the interpreter. The test also logs the static
  comparison of the two (instructions, code bytes, frame sizes);
  `internal/amd64/ssa_test.go` covers φ copies along every kind of edge
  natively.
- Instruction selection (D-029): `TestSelectDifferential` runs every selected
  form on boundary values of all ten integer types, with values in registers and
  in cells, built by gc, by `aether build`, and with `-O0`, and in the
  interpreter. `checkSelection` runs on every function emitted with selection.
  The `isel` entries of `TestNegativeControls` plant bugs in `select.go`.
- Every end-to-end and differential test runs at every optimization level.
- Results at every level must be identical to each other and to the oracle.
- In tests, the verifier runs after every pass.
- A pass-bisection mode (disable passes one at a time) is used to locate the
  failing pass when a cross-check fails.

### Reproducibility and deterministic output

- Two builds must produce byte-identical diagnostics, IR text, `.s` files, and
  executables under the documented conditions in `ROADMAP.md` §Milestone 1,
  check P6:
  - **Controlled:** the Aether binary, source bytes, flags, and resolved
    `as`/`ld` paths and versions. These are identical in both builds, and the
    test records them and asserts they are equal.
  - **Fixed by the driver:** the tool environment (`LC_ALL=C` only) and the
    relative file names.
  - **Deliberately varied:** the work-directory location, the current
    directory, the output path, and an unrelated environment variable.
- This is tested from Phase 3 on (`TestMilestone1Determinism`).
  Reproducibility across binutils versions, Aether builds, or hosts is not
  claimed.
- Phase 0 experiments support feasibility, but they are not a guarantee:
  - E2: identical rebuilds.
  - E6: identical executables across different work-directory paths and input
    file names, with no `.file` directive and no build-id.
  - E9: identical executables across output file names; the tools work under
    `env -i LC_ALL=C`.
- Output must not embed absolute or temporary paths, timestamps, or build-ids
  (D-008).

## Provenance tests (Milestone 1 onward)

These tests show that an executable's program logic came from Aether's backend
and was turned into machine code only by GNU `as` and `ld`. The full procedure
and the mapping to requirements are in `ROADMAP.md` §Milestone 1 acceptance
test.

**Primary checks**, all required:
- **P1:** Aether accepts, type-checks, and lowers the program. The verified IR
  matches its golden file, and the interpreter reproduces gc's output.
- **P2:** Aether's backend, run in-process on that IR, produces assembly
  byte-identical to the program `.s` used in the build. The runtime `.s` files
  match the repository sources.
- **P3:** The test harness itself reconstructs the executable from that
  assembly and the runtime sources, using GNU `as`/`ld` with the documented
  commands and environment. The result is byte-identical to Aether's output.
  This is what establishes that the executable is exactly GNU `as`/`ld` applied
  to Aether's assembly, with no Go compiler involved. It does not rely on
  Aether's own logging.
- **P4:** The executable's behavior matches gc and the interpreter.
- **P5:** Unsupported, invalid, and tool-failure builds exit with the
  documented code. They create nothing at the output path and leave any
  pre-existing file there unchanged.
- **P6:** Rebuilds are reproducible under the documented conditions above.

**Implementation** (`cmd/aether/m1_test.go`, Phase 3): `TestMilestone1`
(P1–P4 and the supporting evidence), `TestMilestone1Rejections` (P5), and
`TestMilestone1Determinism` (P6). They run the `aether` binary, built by the
test. Negative controls confirmed that P2 and P3 fail when the driver
assembles text other than the backend's output, and that P3 and the command
log check fail when an extra `ld` option is passed.

**Supporting evidence**, recorded but not sufficient on its own:
- Aether's `-x` command log
- the `os/exec` dependency test
- a restricted-`PATH` rebuild (a failure there is investigated as a driver
  bug, not read as provenance evidence)
- `objdump` correspondence between `.s`, `.o`, and the executable
- the absence of gc ELF sections (`.go.buildinfo`, `.note.go.buildid`,
  `.gopclntab`) and of `runtime.*` symbols

A log can be incomplete, and a binary can lack gc sections without having been
built by Aether. Only the reconstruction is treated as proof.

## Test-only use of the official toolchain

The gc toolchain may build **reference** binaries and run `go test`. It must
never build the program under test.

Test code may start processes (`go list`, `go build` of reference programs and
of the `aether` binary). The `os/exec` rule in `ARCHITECTURE.md` governs
compiler packages only, and `go list -deps` checks non-test code only.

Tests that build with gc or scan the GOROOT corpus are skipped under
`go test -short`. Provenance tests (above, and `ROADMAP.md`
§Milestone 1) enforce this.

## Conformance corpus

`/usr/local/go/test` contains gc's black-box tests. Its README notes that
tests there may be "applicable to gccgo and other Go tool chains". Phase 0
counted 141 top-level files containing a `// run` line.
- From Phase 1 the corpus is used for gate-classification sweeps.
- From Phase 11 it is used for pass-rate reporting.
- It is read in place from GOROOT, not copied into the repository. Copying would
  require handling the BSD license.
