# aether

Aether is an independent compiler for the Go programming language, written in
Go, targeting native Linux x86-64 executables through its own intermediate
representation and backend.

> **Status: Phase 1 is complete (pending owner review).** Aether can
> *classify* a Go source file against its first supported subset, S1. It
> cannot compile anything yet: there is no IR, code generation, or runtime.

## What works today

```
go build -o aether ./cmd/aether
./aether check prog.go
```

`aether check` parses and type-checks one file with the standard library's
`go/parser` and `go/types`; Aether reuses these, it does not implement them.
It then runs Aether's own subset gate and reports one of the following:

| Exit | Meaning |
|---|---|
| 0 | the program is inside S1 (no output) |
| 1 | not valid Go (`invalid`) |
| 3 | valid Go outside S1 (`unsupported`, with a feature name) |
| 2 | usage error |
| 4 | internal compiler error |

Diagnostics go to stderr as `file:line:col: class: message`. See D-018 in
[docs/DECISIONS.md](docs/DECISIONS.md).

Run the tests with `go test ./...`. `go test -short ./...` skips the gc
cross-check, the binary test, and the GOROOT corpus sweep.

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
  → [later] SSA + optimizations
  → amd64 backend             (instruction selection, frames, later register allocation)
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
- Later phases: GNU binutils (`as`, `ld`) targeting x86-64 ELF for
  `aether build`, and a Linux x86-64 host to run the executables. Phase 1 does
  not need them.

## Current limitations

- Only `aether check` exists. There is no `aether build`, no IR, no code
  generation, and no runtime.
- Only one source file, `package main`, with no imports, is ever accepted.
  Everything outside S1 is **rejected with an explicit diagnostic**.

## First milestone

Compile a single-file, import-free `package main` program into a static
executable whose instructions Aether's backend generated. The program uses
`int` local variables, `+ - *` with Go's wraparound semantics, nested blocks,
and the `println` builtin. This is subset S1. `aether check` can recognize S1
programs, but **compiling them is not yet implemented** (Phases 2–3); see
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
- [Semantics](docs/SEMANTICS.md): compatibility policy, subset S1, support matrix
- [Roadmap](docs/ROADMAP.md): phased milestones with acceptance criteria
- [Decisions](docs/DECISIONS.md): decision register (accepted and proposed)
- [Testing](docs/TESTING.md): testing strategy
- [Benchmarks](docs/BENCHMARKS.md): measurement strategy
- [AGENTS.md](AGENTS.md): working rules for contributors and coding agents
