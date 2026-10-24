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
proposed format, finalized in Phase 1, is:

```go
// aether:expect run
// aether:expect unsupported <feature>
// aether:expect invalid
```

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

The full pipeline runs: source → `aether build` → execute → compare. These
tests run only when `as` and `ld` are found. If the tools are missing, the
tests are **skipped with a visible message**, never silently passed.

### Differential testing against gc

For each `// aether:expect run` program:
1. `go build -o ref prog.go`, then execute `ref`. Never use `go run`: it
   rewrites the exit status (E1).
2. Run the Aether interpreter (from Phase 2) and the Aether-built executable
   (from Phase 3).
3. Compare stdout, stderr, and exit status across all three.
4. Apply normalization only for divergences listed in `SEMANTICS.md` §Known
   divergences. Example from Phase 4: for panicking programs, compare only the
   first stderr line and the exit status.

The gc version used is recorded in test logs. Programs that use unspecified
behavior are marked `// aether:unspecified` and either excluded or checked
against a list of every allowed outcome.

From Phase 4 on, **generated differential tests** cover operator × type ×
edge-value tables (0, ±1, min, max, powers of two), and their expected outputs
come from running gc.

### Fuzz testing

Native Go fuzzing (`testing.F`):
- **Frontend and gate** (Phase 1): arbitrary bytes produce no panic and no
  `internal` diagnostic.
- **Lowering and verifier** (Phase 2): mutate valid S-subset programs. Every
  gate-accepted program must lower to verifier-clean IR.
- **Program generation** (Phase 2+): a generator of random *valid* subset
  programs, cross-checked between gc, the interpreter, and native code. Any
  disagreement becomes a regression test.
- **IR transformations** (Phase 5): random IR or random programs, where each
  pass must preserve both verifier validity and interpreter-observed behavior.

The fuzz corpus is committed when it finds a bug.

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
  - shadowing
  - empty `main`
  - `println()` with no arguments
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

- Every end-to-end and differential test runs at every optimization level.
- Results at every level must be identical to each other and to the oracle.
- In tests, the verifier runs after every pass.
- A pass-bisection mode (disable passes one at a time) is used to locate the
  failing pass when a cross-check fails.

### Reproducibility and deterministic output

- Building the same input twice with the same Aether binary must produce
  byte-identical diagnostics, IR text, `.s` files, and executables.
- This is tested in Phase 3 and later. One Phase 0 trial (E2) showed
  `as`/`ld` producing identical output on rebuild. That is not yet a guarantee.
- Output must not embed absolute temporary paths or timestamps.

## Test-only use of the official toolchain

The gc toolchain may build **reference** binaries and run `go test`. It must
never build the program under test. Provenance tests (`ROADMAP.md` §Milestone 1)
enforce this.

## Conformance corpus

`/usr/local/go/test` contains gc's black-box tests. Its README notes that
tests there may be "applicable to gccgo and other Go tool chains". Phase 0
counted 141 top-level files containing a `// run` line.
- From Phase 1 the corpus is used for gate-classification sweeps.
- From Phase 11 it is used for pass-rate reporting.
- It is read in place from GOROOT, not copied into the repository. Copying would
  require handling the BSD license.
