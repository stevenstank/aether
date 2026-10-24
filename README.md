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
  → GNU assembly → as → ld    (host binutils, for now)
  → static ELF executable
```

The frontend and the assembler/linker are reused. The IR, lowering, optimizer,
code generation, and runtime belong to Aether. The exact ownership boundaries
are in [docs/VISION.md](docs/VISION.md).

## Current limitations

All of them: nothing is implemented. When implementation begins, programs
outside the supported subset will be **rejected with an explicit diagnostic**,
never compiled incorrectly.

## First milestone

Compile a single-file `package main` program that uses `int` local variables,
`+ - *`, and `println` (subset S1, **proposed**) into a static executable that
Aether's backend generated. The output is checked against the official Go
toolchain, along with tests showing that gc took no part in building it. See
[docs/ROADMAP.md](docs/ROADMAP.md#milestone-1-acceptance-test).

## Documentation

- [Vision](docs/VISION.md): goals, non-goals, what "independent" means
- [Architecture](docs/ARCHITECTURE.md): pipeline, stage boundaries, diagnostics, runtime boundary
- [Semantics](docs/SEMANTICS.md): compatibility policy, subset S1, support matrix
- [Roadmap](docs/ROADMAP.md): phased milestones with acceptance criteria
- [Decisions](docs/DECISIONS.md): decision register (accepted and proposed)
- [Testing](docs/TESTING.md): testing strategy
- [Benchmarks](docs/BENCHMARKS.md): measurement strategy
- [AGENTS.md](AGENTS.md): working rules for contributors and coding agents
