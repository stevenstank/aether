# Aether Vision

## Long-term objective

Aether is meant to become an independent compiler, written in Go, for
progressively larger portions of the actual Go language, producing native
executables through its own intermediate representation, optimizer, and
backend. The first target is Linux x86-64.

Full Go compatibility, including the standard library, goroutines, garbage
collection, and reflection, is a **long-term ambition, not a commitment**.
Aether will spend most of its life as a compiler for a declared subset of Go.
It must always say exactly which subset (`SEMANTICS.md`).

## Why build it

Aether is a vehicle for building deep, demonstrable expertise in compiler
engineering:
- language semantics and type systems
- IR design and SSA
- dataflow optimization
- instruction selection, register allocation, and calling conventions
- object files and linking
- runtime systems
- correctness engineering (differential testing and fuzzing)

Go is a good source language for this. It has a precise specification, a
reference implementation to test against, and a runtime model (GC, goroutines,
interfaces) that forces real systems decisions.

## Independent compiler vs. wrapper

A **wrapper** hands the input program to the official toolchain (or to gcc or
LLVM) and lets that toolchain decide how the program becomes machine code.
Aether must never do this for the program it compiles.

**Independent** means Aether itself decides every aspect of the generated
program:
- its control flow
- the instructions executed
- register and stack use
- data layout
- the runtime it calls

What Aether owns, and what it reuses, at the start:

| Area | Owned by Aether? | Notes |
|---|---|---|
| Lexing and parsing | **No** (`go/parser`, D-001) | Revisitable |
| Type checking, constant evaluation | **No** (`go/types`, `go/constant`, D-002) | Revisitable |
| Support boundary (subset gate) | Yes | D-003 |
| IR, lowering, evaluation order | Yes | D-004 |
| Optimizer | Yes | Phase 5+ |
| Instruction selection, register allocation, frame layout, calling convention | Yes | Phases 3–5 |
| Machine-code encoding and ELF writing | **No** (GNU `as`) | D-007; ownership evaluated in D-008 |
| Linking | **No** (GNU `ld`) | D-007; D-008 |
| Runtime | Yes (tiny at first) | D-009 |

"Owning the backend" therefore means owning every decision up to fully specified
assembly. Delegating encoding to `as` does not delegate any of those decisions.
Delegating the frontend means Aether relies on `go/types` for the rules about
which programs are valid Go. Project documentation must state this honestly
rather than claim full ownership.

## Initial platform

- **Target:** Linux, x86-64, statically linked, no libc.
- **Host:** the same platform. Phase 0 verified Ubuntu 24.04 (WSL2), go1.26.1,
  GNU binutils 2.42.
- **Source language version:** go1.26.

## Non-goals for early stages

- Compatibility with gc's ABI, object format, runtime, or linker.
- Compiling the standard library, or any program with imports, before Phase 11.
- Cross-compilation and other targets (ARM64, Windows, macOS).
- Performance competitive with gc.
- Matching gc's diagnostic wording, panic tracebacks, or debug info.
- cgo, plugins, race detector, coverage instrumentation, PGO.
- Self-hosting.

## Evidence of meaningful progress

Progress is only what can be demonstrated by reproducible tests:
- the size of the subset in the support matrix that is *Supported (native)*,
  each feature with tests
- differential agreement with gc across the test suite, including generated and
  fuzzed programs
- zero known miscompilations outstanding, with each past one covered by a
  regression test
- measured, not asserted, effects of optimization passes
- the pass rate on the GOROOT `test/` corpus (late phases)

Lines of code, number of packages, and document count are **not** progress.

## Maturity levels

| Level | Meaning | Roadmap |
|---|---|---|
| 0 | Researched architecture, no code | Phase 0 |
| 1 | Classifies programs; interprets S1 via its own IR | Phases 1–2 |
| 2 | First native executable from an Aether backend (S1) | Phase 3 |
| 3 | Structured programs with functions; optimizing backend with register allocation | Phases 4–5 |
| 4 | Realistic single-package programs: composite types, heap, interfaces, closures, defer | Phases 6–8 |
| 5 | Managed memory and concurrency | Phases 9–10 |
| 6 | Multi-package programs; meaningful standard-library subset; published conformance rate | Phase 11 |

**Near-term commitments:** levels 0–2. Levels 3 and above are direction, and
are subject to revision as decisions in `DECISIONS.md` are resolved.
