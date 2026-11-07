# aether

Aether is an independent compiler for the Go programming language, written in
Go, targeting native Linux x86-64 executables through its own intermediate
representation and backend.

> **Status: Phase 5 is in progress; Phase 4 was accepted by the owner on
> 2026-11-03.** Aether compiles programs in its supported subset, **S2**, into
> static Linux x86-64 executables. Its own backend selects every instruction and
> its own runtime provides process start, printing, panics, and stack-overflow
> detection; GNU `as` and `ld` only assemble and link. S2 is S1, the first
> milestone's subset (one `main` function with `int` variables, `+ - *`, nested
> blocks, and `println`), plus functions with `int` parameters and results and
> stack-overflow detection (increment 4.2), `bool`, `if`, and `for` (increment
> 4.1), multiple results (4.3), every integer type with conversions and bit
> operators (4.4), `/ % << >>` with run-time panics (4.5), package-level `var`
> and `const` (4.6), and `switch` (4.7).
>
> Phase 5 has started. `aether build` now promotes functions to SSA form
> (D-006), keeps values in registers where it can (D-026, D-027), and selects
> shorter forms for comparisons, branches, and constants (D-029), and passes
> arguments and results in registers (ABI-R1, D-030, D-031; `-O0` keeps the
> stack convention ABI-S0). It computes arithmetic in its result's register and
> folds constants. Copy propagation and dead code elimination have not started.

## What works today

```
go build -o aether ./cmd/aether
./aether check prog.go
./aether build -o prog prog.go
./prog
```

`aether check` parses and type-checks one file with the standard library's
`go/parser` and `go/types`; Aether reuses these, it does not implement them.
It then runs Aether's own subset gate and reports one of the following:

| Exit | Meaning |
|---|---|
| 0 | the program is inside S2 (no output) |
| 1 | not valid Go (`invalid`) |
| 3 | valid Go outside S2 (`unsupported`, with a feature name) |
| 2 | usage error |
| 4 | internal compiler error |
| 5 | GNU `as` or `ld` missing, not identified, or failed (`build`) |

Diagnostics go to stderr as `file:line:col: class: message`. See D-018 in
[docs/DECISIONS.md](docs/DECISIONS.md).

For a program inside S2, `aether check` also lowers it to Aether's IR and
verifies the IR; a failure there is an internal error (exit 4).

`aether build -o OUT prog.go` uses the same codes. It emits Aether's
assembly, assembles and links it with GNU `as`/`ld` together with Aether's
runtime, and writes `OUT` only if every step succeeds; an existing `OUT` is
replaced only then. `-x` prints the commands run and the tool versions,
`-work` keeps the work directory, and `-O0` builds without SSA promotion or
registers, with every value in a stack cell. See D-022 and D-028.

The IR interpreter is a test oracle and has no command. Tests compare the
interpreter and the built executables with the gc-built binary.

Run the tests with `go test ./...`. `go test -short ./...` skips the gc
cross-check, the gc differential test, the binary test, and the GOROOT corpus
sweep.

## What Aether aims to become

The goal is a compiler for progressively broader portions of the real Go
language that generates machine code itself, rather than delegating to the
official Go toolchain, gcc, or LLVM. It will support a **declared subset** of
Go for a long time. Full Go compatibility is a long-term ambition, not a
current or near-term property. See [docs/VISION.md](docs/VISION.md).

| | |
|---|---|
| Implementation language | Go 1.26 |
| Source language | Go (language version go1.26), as a documented subset |
| Initial target | Linux x86-64, static, no libc |

## Planned pipeline

```
Go source
  → go/parser + go/types      (reused standard-library frontend)
  → subset gate               (rejects anything not yet supported)
  → Aether IR                 (+ reference interpreter used as a test oracle)
  → SSA (slot promotion) + constant folding [more passes later]
  → amd64 backend             (register allocation, instruction selection, frames)
  → GNU assembly → as → ld    (GNU binutils: translate and link only)
  → static ELF executable
```

The frontend (`go/parser`, `go/types`) is reused.

**Aether owns:**
- the IR, lowering, and optimizer
- instruction selection, register allocation, and stack layout
- assembly emission
- the runtime
- the semantics of the generated program

**GNU `as`** translates Aether's assembly into object code, and **GNU `ld`**
links the objects into the executable. Both are accepted as initial and
potentially long-term dependencies. The exact ownership boundaries are in
[docs/VISION.md](docs/VISION.md) and [docs/DECISIONS.md](docs/DECISIONS.md)
(D-007, D-008).

## Prerequisites

- Go 1.26, to build Aether (module `github.com/stevenstank/aether`, standard
  library only).
- The official Go toolchain, to run the tests. They use it as an oracle and
  for `go list`.
- GNU binutils (`as`, `ld`) targeting x86-64 ELF, for `aether build`, and a
  Linux x86-64 host to run the executables. Verified with binutils 2.42. Tests
  that need them skip with a message when they are missing.

## Current limitations

- Only S2 is supported: no strings, floats, arrays, structs, pointers, slices,
  maps, methods, interfaces, closures, `defer`, `panic`, or goroutines; no
  `fallthrough`, type switches, labels, `goto`, or `for range`; no `type`
  declarations or `init` functions. Panics print no traceback.
- Only one source file, `package main`, with no imports, is ever accepted.
  Everything outside S2 is **rejected with an explicit diagnostic**. The
  diagnostic's text still says "subset S1" (`SEMANTICS.md` §Subset S2).
- The only optimization pass is constant folding. By default, values are kept
  in registers where possible and arguments and results are passed in
  registers; values live across a call stay in stack cells. `-O0` folds
  nothing and keeps every value in a stack cell.

## First milestone

Compile a single-file, import-free `package main` program into a static
executable whose instructions Aether's backend generated. The program uses
`int` local variables, `+ - *` with Go's wraparound semantics, nested blocks,
and the `println` builtin. This is subset S1. It was reached in Phase 3; see
[docs/SEMANTICS.md](docs/SEMANTICS.md#subset-s1-accepted-d-012).

The acceptance test checks:
- that Aether accepted, type-checked, and lowered the program to its own IR
- that Aether's backend emitted the assembly
- that rebuilding the executable from that assembly with GNU `as` and `ld`,
  independently of Aether, gives byte-identical output, so no Go compiler was
  involved
- that the program's output matches the official Go toolchain's
- that invalid and unsupported programs fail clearly and leave no output file

See [docs/ROADMAP.md](docs/ROADMAP.md#milestone-1-acceptance-test).

## Documentation

- [Vision](docs/VISION.md): goals, non-goals, what "independent" means
- [Architecture](docs/ARCHITECTURE.md): pipeline, stage boundaries, diagnostics, runtime boundary
- [Semantics](docs/SEMANTICS.md): compatibility policy, subsets S1 and S2, support matrix
- [Roadmap](docs/ROADMAP.md): phased milestones with acceptance criteria
- [Decisions](docs/DECISIONS.md): decision register (accepted and proposed)
- [Testing](docs/TESTING.md): testing strategy
- [Benchmarks](docs/BENCHMARKS.md): measurement strategy, harness, and
  baselines
- [AGENTS.md](AGENTS.md): working rules for contributors and coding agents
