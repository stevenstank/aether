# aether

Aether is an independent compiler for the Go programming language, written in
Go, targeting native Linux x86-64 executables through its own intermediate
representation and backend.

> **Status: architectural preparation (Phase 0).** There is no compiler yet.
> No Go code exists in this repository, and nothing here can compile a program.

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

## Planned prerequisites

None of these are needed yet, because there is nothing to build:
- Go 1.26, to build Aether. The module will be `github.com/stevenstank/aether`,
  and `go.mod` is created in Phase 1.
- GNU binutils (`as`, `ld`) targeting x86-64 ELF, for `aether build`. Aether
  will check for them and report a clear error if they are missing.
- A Linux x86-64 host to run the produced executables.
- The official Go toolchain, only for running Aether's differential tests.

## Current limitations

All of them: nothing is implemented. When implementation begins, programs
outside the supported subset will be **rejected with an explicit diagnostic**,
never compiled incorrectly.

## First milestone

Compile a single-file, import-free `package main` program into a static
executable whose instructions Aether's backend generated. The program uses
`int` local variables, `+ - *` with Go's wraparound semantics, nested blocks,
and the `println` builtin. This is subset S1, accepted but **not yet
implemented**; see [docs/SEMANTICS.md](docs/SEMANTICS.md#subset-s1-accepted-d-012).

The acceptance test checks:
- that Aether emitted the assembly
- that GNU `as` and `ld` assembled and linked it
- that the program's output matches the official Go toolchain's
- that no Go compiler was involved
- that unsupported programs fail clearly with no output file

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
