# Aether Language Semantics and Compatibility

This document defines how Aether relates to the Go language. It does not
restate the specification. It defines how Aether declares, implements, and
validates support for parts of it.

## Semantic reference

- **Normative reference:** *The Go Programming Language Specification*,
  language version **go1.26** (Jan 12, 2026), as shipped at
  `/usr/local/go/doc/go_spec.html` and published at <https://go.dev/ref/spec>.
- **Memory model:** *The Go Memory Model*, version of June 6, 2022
  (`/usr/local/go/doc/go_mem.html`).
- **Reference implementation (oracle, not authority):** the official Go
  toolchain ("gc") at a recorded version, currently go1.26.1. When gc and the
  spec disagree, the spec wins, and the disagreement is recorded here.

Aether's support for a feature is defined by this document, never by the fact
that `go/parser` or `go/types` accepts it (D-001, D-002, D-003).

## Levels of acceptance

A construct can be in one of these states. Only the last two count as
"supported".

| State | Meaning |
|---|---|
| **Parsed** | `go/parser` builds an AST for it. This implies nothing about support. |
| **Type-checked** | `go/types` accepts it. This implies nothing about support. |
| **Unsupported** | The subset gate rejects it with an `unsupported` diagnostic. This is the default for every construct. |
| **Supported (interp)** | Lowered to IR, executed correctly by the reference interpreter, and covered by tests. |
| **Supported (native)** | All of the above, plus compiled by the amd64 backend and covered by end-to-end and differential tests. |

Any command that executes or generates code (`build`, interpretation) must
reject constructs that are not at least **Supported (interp)**. The only
exception is `aether check` in Phase 1. It classifies programs against the
*target* subset before lowering exists, and that is classification, not support.
A program that passes the gate must either compile correctly or fail with an
`internal` error. It must never be silently miscompiled.

## Compile-time versus run-time semantics

**Compile-time semantics** are owned by `go/types` in early phases (D-002):
- validity
- name resolution
- typing
- untyped-constant arithmetic and representability
- "declared and not used"
- constant division by zero

Aether does not re-derive these. For a constant expression it takes the value
`go/types` recorded (`types.Info.Types[e].Value`), converts it to the
expression's type, and emits it as an IR constant.

**Run-time semantics** are entirely Aether's responsibility:
- evaluation order of non-constant expressions
- integer wraparound
- run-time panics
- memory layout and aliasing
- `defer`, `panic`, and `recover`
- goroutines
- everything else performed by generated code or Aether's runtime

## How features are declared

The **support matrix** at the end of this document is the authoritative list.
Each supported row names:
1. the spec section,
2. the subset (S1, S2, …) and roadmap phase where it became supported,
3. the support state, and
4. the test files that prove it.

Adding a feature means updating the matrix, the subset gate, lowering, the
interpreter, the backend, and tests **in the same change**. A matrix row without
tests is not supported.

## Semantic areas and Aether's policy

### Integer overflow and constants

- Constant expressions are exact (arbitrary precision) and checked by
  `go/types`. Experiment E3 confirmed that `var x int8 = 200` is rejected.
- Non-constant integer arithmetic wraps. Unsigned operations wrap modulo 2ⁿ.
  Signed `+ - * / <<` overflow is "deterministically defined" and "does not
  cause a run-time panic" (spec §Integer overflow).
- **Optimizations must not assume overflow cannot happen.** The spec's own
  example: a compiler "may not assume that `x < x + 1` is always true".
- Division: `MinInt / -1 == MinInt` and `MinInt % -1 == 0` (spec §Arithmetic
  operators). x86 `IDIV` raises a fault in this case, so the backend must
  special-case it. Division by a zero variable must panic. Both are Phase 4
  items.
- Shifts: a negative non-constant shift count must panic at run time. Shift
  counts at or above the operand width are well defined and must not be
  passed raw to x86 shift instructions, which mask the count.
- `int` and `uint` are 64-bit on amd64 (`types.SizesFor("gc", "amd64")`).

### Evaluation order

- Spec §Order of evaluation: function calls, method calls, receive operations,
  and binary logical operations are evaluated in lexical left-to-right order.
  `&&` and `||` short-circuit.
- The order of those events relative to plain variable evaluation and indexing
  is **unspecified**. The spec's example is `x := []int{a, f()}`, which may be
  `[1 2]` or `[2 2]`.
- **Aether policy:**
  - Lowering fixes one deterministic order: operands left to right, with
    side effects serialized into IR instruction order.
  - That order is recorded in the IR, and later passes may not change it.
  - Differential tests must not depend on unspecified orderings unless the test
    is explicitly marked as such and accepts every allowed outcome
    (`docs/TESTING.md`).
- Package-level initialization follows `types.Info.InitOrder`.

### Scoping and name resolution

Name resolution comes from `go/types` (`Info.Defs`, `Info.Uses`,
`Info.Implicits`, `Info.Scopes`). Lowering keys variables by `types.Object`
identity, **never by name**, so shadowing is correct by construction. S1
includes nested blocks specifically to test this.

### Types, conversions, and interfaces

- IR types are Aether's own (D-004), derived from `go/types`. Named types lower
  to their underlying representation, with identity kept for interface and
  reflection data later.
- Conversions between integer types truncate or extend by sign or zero
  according to the source type (spec §Conversions). These are Phase 4 items.
- Interfaces need runtime type descriptors, method tables, dynamic type
  assertions, and comparison (which can panic for uncomparable dynamic types).
  These are Phase 8 items. The representation is not yet decided.

### Memory and pointer semantics

- S1 and S2 have no addressable memory visible to the program: no `&`, no
  pointers, and no heap.
- The spec has no stack/heap distinction. A variable whose address escapes must
  outlive its frame. Until Aether has escape analysis, the planned rule is that
  every address-taken variable is heap-allocated (Phase 7, D-010).
- Nil dereference, out-of-range index, and slice-bounds violations are run-time
  panics, and the generated code must check for them. Aether will use explicit
  checks rather than relying on hardware faults unless a later decision says
  otherwise.

### Panic, defer, and recover

- Phase 4 introduces a **minimal panic**: the runtime prints
  `panic: runtime error: <message>` and exits with status **2**. It prints no
  goroutine traceback, which is a documented divergence from gc (E1 confirmed
  gc's exit status 2 and message prefix).
- `defer`, `recover`, unwinding, and `panic(v)` with arbitrary values are
  Phase 8 items. They need a design for defer records and frame unwinding,
  which does not exist yet.

### Concurrency and the memory model

- Goroutines, channels, `select`, and `sync`/`sync/atomic` are Phase 10 items.
- Until then the memory model is trivially satisfied, because programs are
  single-threaded.
- Once concurrency exists, optimizations must respect happens-before edges
  created by channel operations and synchronization primitives. That
  constraint will be added to the optimizer invariants in Phase 10.
- Data-race behavior follows the memory model's restrictions on "programs with
  races". Aether will not try to match gc's behavior for racy programs.

### Builtins `print` and `println`

These are bootstrapping builtins, and the spec says they are "not guaranteed to
stay in the language", with formatting that is "implementation-specific".
Aether matches gc's observed format for the types it supports (E1):

- written to **stderr**
- decimal integers
- single spaces between arguments
- a newline at the end (`println`)

## Compatibility testing against gc

Every supported feature is tested differentially (D-014, `docs/TESTING.md`):
the same program is built with gc and with Aether, both binaries are run, and
stdout, stderr, and exit status are compared. Known divergences, such as the
missing panic traceback, are listed in the table below. Each listed divergence
has a defined normalization in the test harness.

### Known divergences from gc

| ID | Area | Divergence | Since | Normalization in tests |
|---|---|---|---|---|
| — | — | None yet (no implementation) | — | — |

Planned (Phase 4): the panic output has no goroutine traceback. Tests compare
only the first stderr line and the exit status for panicking programs.

---

## Subset S1 (proposed, D-012)

S1 is the language accepted by the first native milestone (Phase 3). Status:
**Proposed**. Nothing is implemented.

**Program structure**
- Exactly one source file. Package clause `package main`.
- **No** import declarations.
- Exactly one top-level declaration: `func main() { … }`, with no parameters,
  no results, no receiver, and no type parameters.
- No package-level `var`, `const`, `type`, or other functions.

**Statements** (inside `main`)
- `var` declarations of type `int`, with or without an initializer, e.g.
  `var x int`, `var x int = e`, `var x = e`, `var a, b int = e1, e2`.
- Short variable declarations `x := e`, `a, b := e1, e2`, where every
  resulting type is `int`.
- Local `const` declarations. Constants may be untyped. Uses must be of type
  `int`.
- Assignment `x = e`, tuple assignment `a, b = e1, e2` (all right-hand sides
  are evaluated before any assignment), and `x += e`, `x -= e`, `x *= e`.
- `x++`, `x--`.
- Explicit nested blocks `{ … }`, including shadowing declarations.
- Expression statement: a call to the builtin `println` with zero or more
  arguments of type `int`.
- Empty statements.

**Expressions** (all of type `int`)
- Integer literals and constant expressions that are representable as `int`.
  `go/types` enforces this.
- Variables and constants declared as above.
- Binary `+`, `-`, `*` with 64-bit two's-complement wraparound.
- Unary `+`, `-`. `-MinInt64 == MinInt64`.
- Parentheses.

**Explicitly unsupported in S1** (rejected as `unsupported` with a source
position and a feature name):
- `/`, `%`, `<<`, `>>`, `&`, `|`, `^`, `&^`, unary `^`
- comparisons, `bool`, `&&`, `||`, `!`
- `if`, `for`, `switch`, `select`, `goto`, labels, `break`, `continue`,
  `return`, `defer`, `go`
- other functions, function literals, and calls other than `println`
- all types other than `int`, including the other integer types, floats,
  strings, `bool`, arrays, slices, maps, structs, pointers, interfaces,
  channels, and generics
- `print` (deferred to S2; its gc output format for multiple arguments differs
  from `println` and needs its own tests)
- imports, package-level declarations, `init` functions
- blank identifier `_` as an assignment target
- multiple files, cgo, and `//go:` directives that affect code generation

**Run-time behavior of S1 programs**

S1 programs have no run-time panics. Output goes only to stderr through
`println`. The process exits with status 0 when `main` returns.

## Support matrix

| Feature | Spec section | Subset / phase | State | Tests |
|---|---|---|---|---|
| (all features) | — | — | Unsupported | — |

S1 rows are added here as Phase 2 and Phase 3 implement them.
