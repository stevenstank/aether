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
exception was `aether check` in Phase 1, which classified programs against the
*target* subset before lowering existed. Since Phase 4 the gate's subset, S2,
is Supported (native) in full, so the gate accepts nothing that is not.
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
`go/types` recorded (`types.Info.Types[e].Value`). That value is already
converted to the expression's type (E8). Aether extracts it exactly with
`constant.Int64Val`, where a non-exact result is an `internal` error, and emits
it as an IR constant. Constant expressions are never evaluated with machine
arithmetic.

`go/types` does not perform every compile-time check gc performs. Aether's
frontend adds the program-level checks that gc enforces outside its type
checker, and classifies violations as `invalid` (D-002). The known case so far:
`package main` must declare `func main`. gc reports a missing one at link
time, while `go/types` accepts it (E8, E9).

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
- **Division and remainder** (spec §Arithmetic operators; gc behavior
  confirmed in E7). These are Phase 4 items.
  - Integer division truncates toward zero, and the remainder has the sign of
    the dividend.
  - `MinInt / -1 == MinInt` and `MinInt % -1 == 0`. This is **not** a panic.
    x86 `IDIV` faults on this case (SIGFPE on Linux, E10), so the backend must
    detect a `-1` divisor and handle it on a separate path.
  - A zero divisor at run time is a run-time panic. A constant zero divisor is
    a compile-time error (E3).
- **Shifts.** A negative non-constant shift count must panic at run time.
  Shift counts at or above the operand width are well defined. They must not
  be passed raw to x86 shift instructions, which mask the count (E10).
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
  - That order is recorded in the IR, and later passes may not change it
    (D-024).
  - Differential tests must not depend on unspecified orderings unless the test
    is explicitly marked as such and accepts every allowed outcome
    (`docs/TESTING.md`).
- Package-level initialization follows `types.Info.InitOrder`.
- With package-level variables (increment 4.6) the unspecified order is
  observable: in `println(x, f())`, `x + f()`, or `x += f()`, where `f`
  assigns the global `x`, Aether reads `x` first, left to right. gc
  (go1.26.1) reads it after the call (observed 2026-10-30). Both are
  allowed; this is divergence V-4.
- The divergence is broader than globals. gc (go1.26.1, observed
  2026-11-01 and 2026-11-03) evaluates every call in a statement first and
  the other operands after them, even `-x` or `x + 0`; `&&` and `||` keep
  their order. Two observable effects follow:
  - A global read before a call that assigns it gets the value from after
    the call: `x + f()`, `g(x, f())`, `a, b := x, f()`, `return x, f()`.
  - A division or shift that panics before a call runs after the call:
    `println(1/z, f())` with `z == 0` calls `f` and then panics in gc, and
    panics without calling `f` in Aether. This holds for a local `z` too.
- Aether keeps strict left-to-right order and does not match gc (D-024,
  owner decision 2026-11-03).

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
  pointers, and no heap. Pointers come in Phase 7 increment 7.1 (§Pointers).
- The spec has no stack/heap distinction. A variable whose address escapes must
  outlive its frame. Aether has no escape analysis, so every address-taken
  variable is heap-allocated, and heap memory is never freed (D-010, D-035).
- Nil dereference, out-of-range index, and slice-bounds violations are run-time
  panics, and the generated code must check for them. Aether will use explicit
  checks rather than relying on hardware faults unless a later decision says
  otherwise.

### Panic, defer, and recover

- Phase 4 introduces a **minimal panic**: the runtime prints
  `panic: runtime error: <message>` and exits with status **2**. It prints no
  goroutine traceback, which is a documented divergence from gc (E1 confirmed
  gc's exit status 2 and message prefix).
- `defer`, unwinding, and `panic(v)` are supported since Phase 8
  increment 8.6a, and `recover` since 8.6b (§Defer and panic, D-043);
  named results, which deferred calls may set, since 8.7 (D-044).

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

Aether supports `println` from S1 onward as an **explicitly supported compiler
builtin** (D-012). This is not a guarantee of permanent Go language
compatibility: if Go removes `println`, Aether's support for it becomes an
Aether extension.

**Behavior Aether reproduces in S1** (verified against go1.26.1; E1, E4, E7):

| Aspect | Behavior |
|---|---|
| Argument types | Each argument's type in `types.Info` must be `int`. Untyped integer constants are recorded as `int` (E8) and accepted. Every other argument type is `unsupported` in S1. |
| Stream | All output goes to **stderr** (file descriptor 2). Nothing is written to stdout. |
| Format | Each argument in signed decimal (a leading `-` for negatives only, full `int64` range), separated by a single space, followed by one newline. `println()` prints just a newline. |
| Evaluation | All arguments are evaluated, left to right, **before** any output is produced (E7: a panic in the second argument suppressed all output). This is not observable in S1, which has no panics, but lowering must follow it so S2 is correct. |
| Write errors | Ignored. Execution continues and the exit status is unaffected (E7). Short writes are continued, and `EINTR` is retried (D-009). |

**Not specified and not reproduced:** how output is split into `write` system
calls, and whether gc retries short writes. Neither was verified, and neither
is observable in S1 tests, which compare complete stderr contents. If a later
phase needs either (for example, interleaving with stdout), it becomes an open
question at that point.

`print` has a different format (no separators, no newline, per E1) and is not
in S1. From increment 4.4, `print` and `println` take every integer type and
`bool`: signed types in signed decimal, unsigned types in unsigned decimal,
and `bool` as `true` or `false`, as gc prints them (`s2_i02_conversions_print`,
D-023).

## Compatibility testing against gc

Every supported feature is tested differentially (D-014, `docs/TESTING.md`):
the same program is built with gc and with Aether, both binaries are run, and
stdout, stderr, and exit status are compared. Known divergences, such as the
missing panic traceback, are listed in the table below. Each listed divergence
has a defined normalization in the test harness.

### Known divergences from gc

| ID | Area | Divergence | Since | Normalization in tests |
|---|---|---|---|---|
| V-1 | Startup resources | If the runtime cannot map `main`'s stack (for example under a small `ulimit -v`), the program prints `fatal error: cannot allocate the main stack` and exits with status 2. gc's fatal errors also exit 2, with different messages. | Phase 3 (F1 fix) | None: no differential test runs under such limits. `TestRuntimeStack` checks this path directly. |
| V-3 | Panics | A run-time panic prints only `panic: runtime error: <message>`, or for a failed type assertion `panic: interface conversion: <message>`, with no goroutine traceback, nor for a nil dereference gc's `[signal SIGSEGV …]` line; both exit 2. | Phase 4 increment 4.5 | The panic comparison rule below (`panicPrefix` in `TestDifferentialAgainstGc`). |
| V-4 | Evaluation order | Aether evaluates operands strictly left to right (D-024); gc evaluates a statement's calls before its other operands. A global read before a call that assigns it, a `string(b)` of a `[]byte` before a call that changes its elements, a method's value receiver copied before a call among its arguments that changes it, or a division, shift, index, or nil check that panics before a call, behaves differently (§Evaluation order, §Methods). Both orders are allowed by the spec. | Phase 4 increment 4.6 | None: differential test programs never combine, in one statement, a call with a global it may assign, with a `string(b)` of bytes it may change, with a method's value receiver it may change, or with a panicking operation before it. `TestGlobalReadOrder` pins Aether's order for globals without gc, `TestABIR1PanicOrder` pins the panic case without gc, under ABI-R1 and in the interpreter, and `TestMethodReceiverOrder` pins it for receivers in the interpreter, both backends, and ABI-R1. |
| V-6 | Slices | When `append` grows a slice, its new capacity is the largest of the needed length, twice the old capacity, and 4 elements (D-036), where gc's depends on its size classes and allocation; the capacity, and whether a later `append` shares the array, can differ. The capacity of `[]byte(s)` and of `[]rune(s)` is its length, where gc's may be larger (increment 7.3; measured 2026-11-16: `cap([]rune(s))` of a 3-byte string made at run time is 32 with gc, 3 with Aether). | Phase 7 increment 7.2 | None: differential test programs never print a capacity after growth or of `[]byte(s)` or `[]rune(s)`, or depend on sharing after a growing `append`. |
| V-5 | Memory | Heap memory is never freed (D-010): a program's resident set grows by every byte it allocates, garbage included, where gc's levels off at its live data; after 64 GiB of allocations in all, the program writes `fatal error: runtime: out of memory` and exits 2. Measured in `docs/benchmarks/2026-11-15-heap/README.md`. | Phase 7 increment 7.1 | None: no test program allocates enough to run out. |
| V-7 | Method expressions | The method expression `(*T).M` of a method with a value receiver, called with a nil `*T`, panics with `panic: runtime error: invalid memory address or nil pointer dereference`, where gc's generated wrapper panics with `panic: value method main.T.M called using nil *T pointer`; both exit 2 (D-039). | Phase 8 increment 8.2 | None: differential test programs never call such a method expression with nil. `TestNilMethodExpression` pins Aether's behavior in the interpreter, both backends, and ABI-R1. |
| V-8 | Map iteration order | A range over a map visits its entries in insertion order, the same in every run, where gc's order is randomized and differs from run to run; both are allowed by the spec (D-042). Whether an entry added during a range is visited, which the spec leaves open, follows Aether's rule in D-042. | Phase 8 increment 8.5b | None: differential test programs only print what does not depend on the order (sums, counts, lookups), and add no entries during a range. `TestMapRangeOrder` pins Aether's order in the interpreter, both backends, and ABI-R1. |
| V-9 | Panic values | A panic value of a type other than a basic type, `error`, or `fmt.Stringer`-like (`String() string`) is written as `(T) ` and its data word in hex, which is an address of Aether's (or the interpreter's), not gc's, except for nil (`0x0`). If an `Error` or `String` method panics while the panics are written at exit, gc ends with `panic while printing panic value`; Aether writes that value as empty (D-043). | Phase 8 increment 8.6a | None: differential test programs panic with such values only when nil, and with no method that panics. |
| V-2 | Stack overflow | Detailed below: Aether overflows at a different depth and prints only `fatal error: stack overflow`; both exit 2. | Phase 4 increment 4.2 | The overflow comparison rule below (`TestStackOverflowAgainstGc`). |

Implemented (Phase 4 increment 4.5):
- **V-3, panics.** The run-time errors are `integer divide by zero` (for
  `/` and `%`) and `negative shift amount`. Panic comparison rule:
  For a program that panics with a run-time error, let Q be the exact
  bytes of the gc-built binary's stderr up to and including the first
  newline after the first occurrence of the text `panic: `; the line from
  that occurrence must be `panic: runtime error: ` and a message. The text is
  found anywhere, not at a line start, because a program that last used
  `print` leaves no newline before it. The stderr of the Aether-built
  executable and of the interpreter must each be exactly Q, with nothing
  after it, and the gc-built binary, the Aether-built executable, and the
  interpreter must each write nothing to stdout and exit with status 2.
  gc's traceback after Q is not compared. Since D-043 the line may be any
  `panic: ` and a message, and Q also takes in each following line that
  begins with a tab: later panics, and the lines of a value with newlines.

Implemented (Phase 4 increment 4.2):
- **V-2, stack overflow** (increment 4.2, D-009). Aether's stack is at least
  256 MiB, larger only when `main`'s own frame needs more
  (`aether.mainstack`), and its frames are larger than gc's, so recursion
  usually overflows at a smaller depth than under gc's 1e9-byte limit.
  Aether prints only `fatal error: stack overflow` and exits 2; gc prints a
  `runtime: goroutine stack exceeds …` line, a `runtime: sp=…` line, the same
  `fatal error` line, and a traceback, and also exits 2 (E14).
  Overflow comparison rule:
  The rule applies to test programs whose recursion never ends, so that the
  gc-built binary, the Aether-built executable, and the interpreter all
  overflow. The program prints nothing during its recursion. Let D be the
  text `runtime: goroutine stack exceeds`, which begins gc's stack-overflow
  diagnostic. Let P be the exact byte prefix of the gc-built binary's stderr
  before the first occurrence of D. P holds everything the program printed
  before the recursion and is compared exactly; it need not end in a newline,
  because `print` writes none. The gc-built binary's stderr must contain D.
  Its bytes from that occurrence on must begin with the stack-limit line: D,
  one space, one or more ASCII digits, the text `-byte limit`, and a newline.
  After the stack-limit line, the first newline-terminated line that begins
  with `fatal error: ` must exist and must be exactly
  `fatal error: stack overflow`. Beyond P and these checks, gc's stderr is
  not compared: the `runtime: sp=` line, the traceback, and any other lines
  are ignored. The stderr of the Aether-built executable and the stderr of the
  interpreter must each be exactly P followed by `fatal error: stack overflow`
  and one newline, with nothing after it. The gc-built binary, the Aether-built
  executable, and the interpreter must each write nothing to stdout and exit
  with status 2. If any of these checks fails, the test fails. A program that
  overflows in Aether but not under gc shows the V-2 depth difference and is not
  compared with gc by this rule.
- **Stack limitations** (D-009): running out of memory while the stack is in
  use can end the program by a signal with no message, and a small
  `ulimit -v` makes every program fail at startup with V-1.

---

## Subset S1 (accepted, D-012)

S1 is the language accepted by the first native milestone (Phase 3). The
definition was accepted at the owner's Phase 0 review on 2026-10-24.

**Status.** S1 is **Supported (native)** (Phase 3). The subset gate enforces
every rule in this section (Phase 1). Every construct is lowered to IR
(Phase 2), executed by the reference interpreter, and compiled by
`aether build` into a static Linux x86-64 executable (Phase 3). On the corpus
and on generated programs, the interpreter's and the executable's output equal
gc's (support matrix).

**Program structure**
- Exactly one source file.
- Package clause `package main`. Any other package name is `unsupported`:
  Aether builds programs, not libraries.
- **No** import declarations.
- Exactly one top-level declaration: `func main() { … }`, with no parameters,
  no results, no receiver, and no type parameters.
  - A `package main` with no `func main` is `invalid`. Aether checks this
    itself, because `go/types` does not (E8).
  - `main` declared with parameters or results, or as a non-function, is
    `invalid` (reported by `go/types`, E8).
- No package-level `var`, `const`, `type`, or other functions.

**Statements** (inside `main`)
- `var` declarations of type `int`, with or without an initializer, e.g.
  `var x int`, `var x int = e`, `var x = e`, `var a, b int = e1, e2`.
- Short variable declarations `x := e`, `a, b := e1, e2`, where every
  resulting type is `int`. This includes **redeclaration**: in
  `a, c := 3, 4` with `a` already declared in the same scope, `a` is assigned,
  not declared. `go/types` records it in `Info.Uses`, not `Info.Defs` (E8).
  Lowering must treat it as an assignment to the existing variable (case
  S1-10).
- Local `const` declarations. Constants may be untyped or of type `int`. Uses
  must be of type `int`.
- Assignment `x = e`, tuple assignment `a, b = e1, e2` (all right-hand sides
  are evaluated before any assignment), and `x += e`, `x -= e`, `x *= e`.
- `x++`, `x--`.
- Explicit nested blocks `{ … }`, including shadowing declarations.
- Expression statement: a call to the builtin `println` with zero or more
  arguments of type `int`.
- Empty statements.

**Expressions** (all of type `int`)
- Integer literals, in any integer literal form: decimal, `0x`, `0o`/`0`,
  `0b`, with or without `_` separators.
- Constant expressions representable as `int` (`go/types` enforces
  representability). A constant expression is subject to the **same**
  restrictions as any other S1 expression: it may contain only integer
  literals, S1 constants, the operators below, and parentheses.
  - Float, imaginary, rune, and string literals are `unsupported` even when the
    resulting constant has type `int`. For example, `var x int = 1.0` is valid
    Go and `go/types` records it as the `int` value 1 (E8), but S1 rejects it.
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
- `println` arguments of any type other than `int`, for example
  `println('a')` (type `rune`), `println(1.5)`, or `println("hi")`
- float, imaginary, rune, and string literals anywhere, including inside
  constant expressions
- imports, package-level declarations, `init` functions, package names other
  than `main`
- the blank identifier `_` in any position (declaration or assignment)
- more than one source file
- cgo
- every `//go:` directive comment (including `//go:build`), `// +build` lines,
  and `//line` directives

The following forms are also outside S1. Each follows from the definition
above and is listed explicitly because Phase 1 testing showed it needs a
dedicated rule (E12):
- the predeclared identifier `iota`, and `true`, `false`, `nil`
- a constant declaration without a value (implicit repetition in a `const`
  group)
- a parenthesized assignment target, as in `(x) = 1` or `(x)++`
- uses of package-level variables and constants (their declarations are
  rejected too)
- an empty result list, `func main() ()` or `func init() ()`. This is valid
  Go, but `go/types` wrongly reports it as an error, so the gate rejects it
  before type checking (D-019).
- `func main()` without a body. This is valid Go per the spec (a function
  implemented outside Go); gc rejects it only because no assembly is supplied
  (D-020). A body-less `func init()` is instead invalid Go, and is classified
  `invalid`.

**Feature names.** Unsupported diagnostics end with a feature name in
brackets. The names are a stable contract (D-018), used by tests and the
`// aether:expect unsupported <feature>` header (`TESTING.md`):

| Area | Feature names |
|---|---|
| Files and packages | `multiple-files`, `package-name`, `import`, `cgo`, `directive`, `empty-result-list` |
| Top-level declarations | `package-level-declaration`, `function-declaration`, `init-function`, `function-without-body` |
| Generics (D-045) | `generic-instance`, `generic-uninstantiated` |
| Statements | `for-range`, `select`, `go`, `defer`, `goto`, `fallthrough`, `label`, `send`, `statement` |
| Declarations and assignment | `local-type-declaration`, `const-implicit-value`, `blank-identifier`, `assignment-target`, `multi-value-assignment` |
| Types | `non-int-type`, `type-expression`, `frame-size` |
| Literals and identifiers | `imaginary-literal`, `predeclared-identifier` |
| Operators | `operator-receive`, `operator-other`, `loop-variable-address` |
| Calls | `call`, `conversion`, `builtin`, `println-argument-type`, `variadic-call` |
| Other expressions | `composite-literal`, `selector`, `index-expression`, `slice-expression`, `expression` |

`statement` and `expression` are catch-alls. The gate is default-deny, so a
construct with no specific rule is still rejected.

The gate keeps these names in a single registry (`gate.Features()`).
`TestFeatureTableMatchesGate` fails if this table and the registry differ in
either direction, and a rejection with an unregistered name is an `internal`
error. Update the table and the registry together.

**Constant expressions versus run-time arithmetic in S1**

- A constant expression has an exact, arbitrary-precision value computed by
  `go/types` (D-002). Aether never computes or infers it by running `int64`
  arithmetic.
- Intermediate values may exceed `int64`, provided the final value is
  representable where it is used (case S1-05).
- A constant that is not representable as `int` is a compile-time error of
  class `invalid` (cases S1-N01, S1-N02, S1-N04). It does not wrap.
- Non-constant `+ - *` and `++`/`--` wrap modulo 2⁶⁴ in two's complement
  (cases S1-03, S1-04).
- Lowering emits constant expressions as IR constants holding the value from
  `types.Info`, and emits non-constant expressions as IR arithmetic.

**Evaluation order in S1**

S1 has no function calls other than `println` and no expressions with side
effects. Evaluation order is therefore observable only through:
- statement order, including the order of `println` calls;
- tuple assignment, where all right-hand operands are evaluated before any
  variable is assigned (spec §Assignment statements; case S1-06);
- left-to-right evaluation of `println` arguments, which in S1 have no side
  effects.

Lowering must nevertheless evaluate all `println` arguments before emitting any
output (E7), so that the rule is already right when panicking operators arrive
in S2.

No S1 program depends on behavior the spec leaves unspecified.

**Run-time behavior of S1 programs**

S1 programs have no run-time panics. Output goes only to stderr through
`println`. The process exits with status 0 when `main` returns. The spec says
only that the program exits; status 0 is gc's observed behavior (E1, E4), and
Aether matches it. `main` runs on a stack the runtime maps to fit its frame,
so the result does not depend on `ulimit -s`. The only other outcome is
divergence V-1: if that stack cannot be mapped, the program exits with
status 2 before `main` runs.

**Limitations of S1**

S1 cannot express:
- control flow, conditions, loops, or functions
- any type except `int`
- division, remainder, shifts, or bitwise operators
- strings
- non-zero exit status
- input of any kind (arguments, environment, stdin)

Every S1 program is therefore straight-line code with output determined
entirely by its source. S1 tests correctness of declarations, scoping,
assignment, wraparound arithmetic, constants, and output formatting, and
nothing else.

**Verified cases (E4, E5, E8, E9)**

The cases below were run against gc (go1.26.1, `go build` followed by
executing the binary): S1-01 to S1-09 during the Phase 0 review (E4), and
S1-10 and S1-N05 during the pre-Phase 1 audit (E8, E9). They form the minimum S1
differential suite. Phase 2 and Phase 3 tests must include each of them, with
the expected result re-derived from gc in the test itself (`TESTING.md`).

| ID | Exercises | Program body inside `func main() { … }` | gc stderr (exit 0, empty stdout) |
|---|---|---|---|
| S1-01 | empty `main` | *(empty)* | *(nothing)* |
| S1-02 | `println()` with no arguments | `println()` | `⏎` (one newline) |
| S1-03 | arithmetic, shadowing, `++` wrap | Milestone 1 program (`ROADMAP.md`) | `-6 42` / `6 -9223372036854775808` |
| S1-04 | `+ - *` and unary wraparound | `hi := 9223372036854775807; lo := -hi - 1; println(hi+1, lo-1, -lo, +lo); println(hi*2, lo*-1, hi*hi, lo*lo); x := 3037000500; println(x * x)` | `-9223372036854775808 9223372036854775807 -9223372036854775808 -9223372036854775808` / `-2 -9223372036854775808 1 0` / `-9223372036709301616` |
| S1-05 | exact constants with oversized intermediates | `const big = 9223372036854775807 * 4 - 3*9223372036854775807; const neg = -9223372036854775807 - 1; x := big; y := neg; println(x, y)` | `9223372036854775807 -9223372036854775808` |
| S1-06 | tuple assignment | `a, b := 1, 2; a, b = b, a; println(a, b); a, b = a+b, a*b; println(a, b)` | `2 1` / `3 2` |
| S1-07 | zero value, compound assignment, `var` forms | `var x int; x += 5; x -= 7; x *= -3; x++; x--; x--; println(x); var p, q int = 10, -10; var r = p * q; println(p, q, r)` | `5` / `10 -10 -100` |
| S1-08 | nested shadowing, empty statement | `x := 1; { x := x + 10; { x := x * 2; println(x) }; x++; println(x) }; println(x); ;` | `22` / `12` / `1` |
| S1-09 | integer formatting edge values | `m := -9223372036854775807 - 1; println(m, 0, -0, -1, 1, 10, -10, 100, 999999999999)` | `-9223372036854775808 0 0 -1 1 10 -10 100 999999999999` |
| S1-10 | short variable redeclaration | `a, b := 1, 2; a, c := 3, 4; println(a, b, c)` | `3 2 4` |

In the table, `/` separates output lines and `;` separates statements written
on separate lines in the tested source.

**Invalid cases (must be classified `invalid`, exit 1; `go build` also fails):**

| ID | Program body | gc diagnostic (abridged) |
|---|---|---|
| S1-N01 | `x := 9223372036854775807 + 1; println(x)` | untyped int constant 9223372036854775808 … (overflows) |
| S1-N02 | `const c int = 9223372036854775807; x := c + 1; println(x)` | c + 1 (constant 9223372036854775808 of type int) overflows int |
| S1-N03 | `x := 1` | declared and not used: x |
| S1-N04 | `const c = -9223372036854775807 - 2; x := c; println(x)` | untyped int constant -9223372036854775809 … (overflows) |
| S1-N05 | whole file is `package main` (no `func main`) | function main is undeclared in the main package (reported by gc at link time; `go/types` reports nothing, so Aether's own check must report it) |

**Unsupported cases (must be classified `unsupported`, exit 3, no output
file).** At least one test per item in the "Explicitly unsupported in S1" list
above. Phase 1 has one corpus file per feature category in
`cmd/aether/testdata/check/`. These must include the valid-Go forms that
`go/types` accepts (E8):
- `var x int = 1.0`
- `println('a')`
- `package foo` with `func main() {}`
- `x := 1; _ = x` (the blank identifier as an assignment target)

The Milestone 1 acceptance test names a further required minimum set
(`ROADMAP.md`).

**Classification precedence** (D-003):
1. The gate's file-level pre-check: multiple files, a non-`main` package
   name, imports, and directive comments (`ARCHITECTURE.md` §2).
2. `invalid`.
3. The remaining `unsupported` checks.

A test program that contains an import is expected to be `unsupported` even if
it also has a type error.

## Subset S2 (Phase 4, accepted 2026-11-03)

S2 is the subset Aether supports today, and the one the gate enforces. It is S1
extended by the sections from here to the support matrix, each one a Phase 4
increment; a rule of S1 holds in S2 unless a later section relaxes it. Its
status is in the support matrix. Everything else is `unsupported`.

The text of `unsupported` diagnostics still says "subset S1". It was not
updated when S2 was added, so read it as "the supported subset".

## Functions (Phase 4 increment 4.2)

The gate accepts S1 extended with functions. Every rule of S1 applies
inside every function.
- Top-level `func` declarations other than `init`, with `int` parameters
  (named, blank, or unnamed) and either no result or one unnamed `int`
  result. A function named `_`, a generic function, a function without a
  body, and a function with named or multiple results remain
  `unsupported`.
- `return`, with a value in a function with a result, anywhere in a body.
  Statements after a `return` are not executed (and not lowered).
- Direct calls `f(args)` of these functions, including `main()`, as
  statements and, for functions with a result, in expressions. Arguments
  are evaluated left to right before the call.
- Recursion. Without `if` (increment 4.1), every recursive call recurses
  without end and overflows the stack (V-2).
- The `return` feature name no longer exists: every `return` is accepted.

## Control flow and bool (Phase 4 increment 4.1)

The gate also accepts, in every function:
- the type `bool`: variables, parameters, results, untyped and typed
  constants, `true` and `false`, and `println` arguments, printed as `true`
  or `false`;
- the comparisons `== != < <= > >=` of `int` operands and `== !=` of
  `bool` operands, unary `!`, and `&&` and `||`, which evaluate their right
  operand only when it decides the result;
- `if`, with an optional init statement, `else`, and `else if`;
- the three `for` forms (three-clause, condition-only, and infinite), and
  unlabeled `break` and `continue`. Each iteration of a three-clause loop
  shares one variable; Go 1.22 gives each its own copy, which no program in
  the subset can observe (it needs closures or pointers).

Still `unsupported`: labeled statements and `break` or `continue` with a
label (`label`), `goto`, `switch` (until 4.7), `for range`, and every type
and operator not listed. The feature names `if`, `for`, `break`, `continue`, `comparison`,
`operator-not`, `operator-logical-and`, and `operator-logical-or` no longer
exist.

## Multiple results (Phase 4 increment 4.3)

Functions may have two or more unnamed `int` or `bool` results. A call of
such a function may supply all the values of a `return`, of an assignment
or `var` declaration of as many variables (`a, b := f()`, `a, b = f()`,
`var a, b = f()`), of the arguments of a call (`g(f())`), and of `println`
(`println(f())`), or be a statement on its own. Still `unsupported`: named
results (`function-declaration`) and the blank identifier as a target
(`blank-identifier`).

## Integer types (Phase 4 increment 4.4)

The gate also accepts the predeclared integer types `int8`, `int16`,
`int32`, `int64`, `uint`, `uint8`, `uint16`, `uint32`, `uint64`,
`uintptr`, and the aliases `byte` and `rune`, in every position `int` is
accepted, with wraparound at each type's width; conversions between integer
types (and `bool(b)`); the operators `&`, `|`, `^`, `&^`, unary `^`, and
their compound assignments; and `print`. `int`, `uint`, and `uintptr` are
64 bits wide. Still `unsupported`: `/`, `%`, `<<`, `>>` (increment 4.5),
rune literals, and conversions to other types (`conversion`). The feature
names `operator-and`, `operator-or`, `operator-xor`, `operator-and-not`,
`operator-complement`, and `print` no longer exist.

## Division and shifts (Phase 4 increment 4.5)

The gate also accepts `/`, `%`, `<<`, `>>`, and their compound
assignments, on every integer type. Division truncates toward zero; the
minimum of a signed type divided by -1 is that minimum, with remainder 0.
A shift count may be of any integer type; a count at or above the width
gives 0, or the sign fill for `>>` of a signed value. A zero divisor
panics with `integer divide by zero` and a negative count with `negative
shift amount` (V-3). The feature names `operator-div`, `operator-rem`,
`operator-shl`, and `operator-shr` no longer exist.

## Package-level variables and constants (Phase 4 increment 4.6)

The gate also accepts package-level `var` and `const` declarations of
`bool` and the integer types, under the rules for local ones (no blank
names, no implicit constant values), and their use in every function.
Variables without an initializer start at zero. Initializers, which may
call functions, run before `main` in `types.Info.InitOrder` (spec §Package
initialization); a panic or stack overflow in one ends the program before
`main` runs. Package-level `type` declarations and `init` functions remain
`unsupported` (`package-level-declaration`, `init-function`).

## Switch statements (Phase 4 increment 4.7)

The gate also accepts expression `switch` statements, with an optional init
statement and an optional tag of `bool` or an integer type, and case clauses
with one or more values and at most one `default`. An unlabeled `break` in a
clause leaves the switch; `continue` continues the enclosing `for`. As the
spec requires, the tag is evaluated once, and the case values are evaluated
and compared with it left to right and top to bottom, only until one
matches; the `default` clause runs if none does, wherever it appears. A
switch without a tag tests each case value as a condition. Lowering builds
the tests from the IR's existing comparisons and branches; the IR is
unchanged. Still `unsupported`: `fallthrough`, labeled switches (`label`),
and switches on other types. Type switches came with interfaces (§Interfaces). The feature name
`switch` no longer exists.

## Floating point (Phase 6 increment 6.1)

The first part of S3 (`ROADMAP.md` Phase 6). The gate also accepts `float64`
and `float32` in every position the integer types are accepted (variables,
parameters, results, globals, constants, `switch` tags and case values), float
literals in every form, and untyped float constants, including those of integer
type such as `var i int = 1.0`. The IR is D-032's.
- **Arithmetic** `+ - * /`, unary `+ -`, their compound assignments, `++`, and
  `--` follow IEEE 754: each operation rounds to nearest, ties to even, at its
  type's precision, and none is fused with another (gc reference binaries are
  built with `GOAMD64=v1`, which has no FMA). Division by zero gives an
  infinity or NaN and never panics. Unary `-` flips the sign, so `-x` of `+0`
  is `-0`.
- **Comparisons** are IEEE 754's: NaN is unordered, so `x != x` holds only for
  NaN, and `-0 == +0`.
- **Conversions** between the float types, and between them and every integer
  type, round to nearest, ties to even, once, from the source value. A float
  converts to an integer by truncation toward zero; a value the type cannot
  represent, which the spec leaves implementation-specific, converts as gc does
  on amd64 (D-032).
- **Constants** are go/types' exact values, rounded to the type by go/types
  (D-002). Float constant arithmetic is exact, as the spec requires.
- **`print` and `println`** write a float as gc go1.26.1 does: the shortest
  decimal that converts back to the same value (`strconv` `'g'` format with
  precision -1), with `NaN`, `+Inf`, `-Inf`, and `-0`.

Still `unsupported`: complex types, imaginary literals (`imaginary-literal`),
and the builtins `real`, `imag`, `complex`, `min`, and `max` (`builtin`). `%`,
the bitwise operators, and shifts on floats are invalid Go. The feature name
`float-literal` no longer exists.

## String constants (Phase 6 increment 6.2)

The gate also accepts the type `string`, string literals (interpreted and raw),
and string constants, in every position the other types are accepted:
variables, `:=`, parameters, results, multiple results, globals and their
initializers, and `print` and `println` arguments. Every string value is a
constant's bytes or the empty string; no operation creates string data (D-033).
- **`len(s)`** is the number of bytes, not of code points. Of a constant it is a
  constant, as go/types computes it.
- **`s[i]`** is the byte at offset `i`, of type `byte` (`uint8`), for an index of
  any integer type or an untyped constant. An index out of range, negative
  included, panics with gc's message, `index out of range [i] with length n`, or
  `index out of range [i]` for a negative index of a signed type; an unsigned
  index is printed unsigned. Both follow the panic rule of V-3.
- **`print` and `println`** write a string's bytes exactly, NUL bytes and invalid
  UTF-8 included; the empty string writes nothing.

Still `unsupported`: concatenation, `+` and `+=` (`string-concatenation`);
comparison, `== != < <= > >=`, and `switch` on a string (`string-comparison`);
conversions to `string` and from it to slices (`conversion`); slicing
(`slice-expression`); `for range` over a string (`for-range`); rune literals
(`rune-literal`). The feature name `string-literal` no longer exists.

## Arrays and structs (Phase 6 increment 6.3)

The gate also accepts array and struct types whose elements and fields are of
supported types, nested to any depth, of at most 2²⁴ bytes: type literals, and
defined types declared by `type T [N]E` or `type T struct{…}`, at package level
or locally (an alias, a generic type, or a defined type of another underlying
type is still `local-type-declaration` or `package-level-declaration`). Their
values may be variables, globals and their initializers, parameters, results,
and multiple results, as the other types; each lives in memory laid out as
`types.SizesFor("gc", "amd64")` gives, which is what `unsafe.Sizeof` and
`unsafe.Offsetof` report for gc (D-034).
- **Values.** Assignment, passing, and returning copy the whole value; a
  callee's changes to its parameter never reach the caller. A variable without
  an initializer is the zero value.
- **Literals.** Array and struct composite literals, positional or keyed, `[...]T`,
  and nested literals with elided types; omitted elements are zero.
- **Selectors** `x.f` name a field, including one promoted through embedded
  struct fields. **Indexing** `a[i]` of an array takes an index of any integer
  type; an index out of range panics as for strings (§String constants, V-3),
  and a constant one is checked by the compiler. **`len(a)`** is the array's
  length, a constant unless `a` contains a call, which is then evaluated.
- **Targets.** Fields and elements, through any number of selectors and
  indices, may be assigned, with `op=`, and with `++` and `--`. The operands of
  a target's index expressions are evaluated with the right side, and its
  bounds are checked when it is assigned (spec §Assignment statements), so in
  `a[i()] = f()` and `a[i()] += f()` both calls come before an index panic, as
  with gc. An index read in an expression is checked where it is evaluated,
  left to right (V-4).
- **`==` and `!=`** compare corresponding elements and non-blank fields: floats
  by IEEE 754, so a NaN makes a value unequal to itself.
- **Conversions** between array or struct types of identical underlying types.
- **Frame size.** A function, or package initialization, whose array and
  struct variables and temporaries may need more than 512 MiB of stack, by a
  bound the gate computes from the source, is `unsupported` (`frame-size`): its
  frame would exceed the backend's 1 GiB limit (D-034). A smaller frame that does
  not fit the stack overflows it, as any frame does (V-2); gc instead moves large
  variables to the heap.

Still `unsupported`: `switch` on an array or struct value (`non-int-type`);
`==` and `!=` of ones that hold strings are supported since increment 7.3
(§Strings at run time); printing
one (`println-argument-type`); slicing an array (`slice-expression`); `for
range` (`for-range`); slice and map literals (`composite-literal`); `cap`
(`builtin`); methods; parenthesized assignment targets
(`assignment-target`).

## Pointers (Phase 7 increment 7.1)

The gate also accepts pointer types `*T` to every supported type `T` whose size
is at most 2²⁴ bytes, including pointers to pointers and recursive struct types
such as `type Node struct{ next *Node }`, as variables, globals, fields,
elements, parameters, and results (D-035).
- **Values.** A pointer is the address of a variable, or of a field or element
  of one, or `nil`. Taking the address of a local variable or parameter puts it
  on the heap for its whole life, from its declaration (a parameter from
  function entry), so a pointer may outlive the call that made it and each
  execution of the declaration makes a new variable. Heap memory is never
  freed (D-010).
- **`&x`** of a variable, `*p`, a field, or an array element, reached through
  any number of fields, elements, and pointers; **`&T{…}`** of a composite
  literal, a new variable holding its value; and **`new(T)`**, a new zero
  variable. In an array or struct literal whose elements are pointers, `{…}`
  stands for `&T{…}`.
- **Indirection.** `*p` may be read, assigned, used with `op=`, `++`, and
  `--`. `p.f` selects a field of the struct `p` points to, also through
  embedded pointers, and `p[i]` indexes the array `p` points to; both may be
  assigned. `len(p)` of a pointer to an array is the array's length, a
  constant unless `p` contains a call, which is then evaluated; `p` is not
  dereferenced, so a nil `p` does not panic, as with gc.
- **Nil checks.** Every access through a nil pointer, including `&*p`, `&p.f`,
  and `&p[i]`, panics with `panic: runtime error: invalid memory address or nil
  pointer dereference` and exits 2; gc's following `[signal SIGSEGV …]` line
  is not written (V-3). A check is made where the pointer is used, after the
  operands of the expression that uses it, so in `p.f = g()` and `*p += g()`
  the call comes first, as with gc.
- **`==` and `!=`** compare addresses, also of pointers in arrays and structs,
  and with `nil`; a `switch` may have a pointer tag. Conversions between
  pointer types whose base types have identical underlying types.

Still `unsupported`: printing a pointer (`println-argument-type`), whose
address gc prints and which varies; the address of a variable declared in a
`for` statement's init, which Go 1.22 copies for each iteration
(`loop-variable-address`); `new` of a value, which Go 1.26 added (`builtin`);
defined pointer types, as `type P *int` (`local-type-declaration`,
`package-level-declaration`); pointer arithmetic and `unsafe` (`import`). The
feature names `operator-address` and `pointer-dereference` no longer exist.
Pointers to distinct zero-size variables may or may not be equal (spec
§Comparison operators); no test depends on either.

## Slices (Phase 7 increment 7.2)

The gate also accepts slice types `[]T` of every supported `T` of at most 2²⁴
bytes, as variables, globals, fields, elements, parameters, and results
(D-036). A slice is a pointer, a length, and a capacity; its array is on the
heap, or is an array variable it was sliced from, which is then on the heap
(or global) as if its address were taken (§Pointers).
- **`make([]T, n)`** and **`make([]T, n, c)`**, zero; a negative length or one
  beyond the heap, or a capacity below the length, panics with gc's
  `makeslice` messages. **Slice literals**, positional or keyed, with elided
  element types.
- **`s[i]`** reads and assigns elements, also with `op=`, `++`, and `--`; an
  index of any integer type at or beyond the length panics as for arrays, and
  in an assignment after the right side is evaluated.
- **`x[i:j]` and `x[i:j:k]`** of slices, addressable arrays, and pointers to
  arrays: the bounds are evaluated, then checked as gc checks them, with its
  messages (`slice bounds out of range [:j] with capacity c` and the others);
  slicing through a nil pointer to an array panics. Slicing a nil slice with a
  low bound of 0 gives nil. The new slice shares the array.
- **`len`** and **`cap`** of slices, and `cap` of arrays and pointers to them.
- **`append(s, x…)`** and **`append(s, t...)`**: within the capacity it
  stores into the shared array, as the spec requires; beyond it, it allocates a
  new array by Aether's growth rule (V-6) and moves the elements. **`copy(dst,
  src)`** copies the shorter length's elements, overlapping or not, and
  returns that length.
- **`== nil`** and **`!= nil`**; conversions between slice types of identical
  underlying types.

Still `unsupported`: `for range` (`for-range`); conversions between slices
and arrays or pointers to arrays, and between `[]rune` and strings
(`conversion`); printing a slice (`println-argument-type`), whose
address gc prints; defined slice types, as `type S []int`
(`local-type-declaration`, `package-level-declaration`); `make` of maps and
channels (`builtin`).

## Strings at run time (Phase 7 increment 7.3)

The gate also accepts these operations on strings, which make strings at run
time (D-037). A string's bytes are a constant's or a heap object's, and never
change; the heap is never freed (V-5).
- **`+` and `+=`** concatenate, of any operands: variables, fields,
  elements, call results, and constants. The operands of a chain such as
  `a + f() + b[i]` are evaluated left to right, and the result is a new
  string; `x += y` evaluates `x`'s index operands, then `y`, then reads `x`.
- **`s[i:j]`**, with either bound omitted, of any string, shares its bytes.
  The bounds are evaluated and then checked as for an array of the string's
  length, with gc's messages (`slice bounds out of range [:5] with length
  3`, `[4:2]`, `[-1:]`).
- **`string(b)`** of a `[]byte` and **`[]byte(s)`** copy the bytes, so a
  later change of `b` is not seen by the string; `string(s)` of a string is
  `s`. The capacity of `[]byte(s)` is its length, where gc's is larger
  (V-6).
- **`append(b, s...)`** and **`copy(b, s)`** take a string's bytes.
- **`==` and `!=`** compare the bytes, never the addresses: two strings are
  equal exactly when they have the same length and the same byte at each
  offset, so the empty string equals every string of length 0 whatever its
  origin, and NUL bytes and invalid UTF-8 compare as bytes, not as Unicode
  (`"é" != "e\u0301"`). Both operands are evaluated, left to right, before
  they are compared. A program that compares strings gets the IR function
  `.streq`, which lowering adds (D-037).
- **`switch`** on a string: the tag is evaluated once, then the case values
  in order, each compared by its bytes, until one matches.
- **`==` and `!=` of arrays and structs** that hold strings, nested to any
  depth, compare each string by its bytes, as part of the element-by-element
  comparison of §Arrays and structs, blank fields excepted.
- **`< <= > >=`** order strings lexicographically by their bytes, read as
  unsigned numbers, a proper prefix first, as Go does: not by Unicode
  (`"Z" < "a"`, `"é" > "z"`). Both operands are evaluated, left to right,
  before they are compared, by the IR function `.strcmp`.
- **Rune literals**, with every escape, are untyped rune constants, `rune`
  (`int32`) by default, usable wherever an integer constant is.
- **`string(x)`** of an integer of any type is the UTF-8 encoding of its
  value, of 1 to 4 bytes; a value that is not a Unicode code point
  (negative, above U+10FFFF, unsigned values above the `int64` range, or a
  surrogate, U+D800 to U+DFFF) gives `"\uFFFD"`. Of a constant it is a
  constant.
- **`[]rune(s)`** (also `[]int32(s)`) decodes UTF-8 as gc and
  `unicode/utf8.DecodeRune` do: each byte that does not start a valid
  encoding (a continuation byte, `0xC0`, `0xC1`, `0xF5` to `0xFF`, an
  overlong form, a surrogate, a rune above U+10FFFF, or a sequence cut
  short) gives U+FFFD and decoding resumes at the next byte. The result is
  a new array; its capacity is its length, where gc's may be larger (V-6).
  `[]rune("")` is empty but not nil.
- **`string(rs)`** of a `[]rune` is the concatenation of each rune's
  encoding, as for `string(x)`; of a nil or empty slice, `""`.
- The conversions run in IR functions that lowering adds (D-037):
  `.encoderune`, `.decoderune`, `.stringtorunes`, and
  `.runestostring`. Every conversion allocates (V-5): `string(x)` 4
  bytes and `string(rs)` 4 bytes per rune, of which the result uses only
  its length.

Still `unsupported`: `for range` over a string (`for-range`). The feature
names `string-concatenation`, `string-comparison`, and `rune-literal` no
longer exist.

## Methods (Phase 8 increment 8.1)

The gate also accepts methods (D-038):
- **Declarations** of methods on the program's defined array and struct
  types, with a value receiver `(t T)` or a pointer receiver `(t *T)`,
  named, blank, or unnamed, and the parameters and results of a function.
  A method is lowered to an ordinary function named `T.M` whose first
  parameter is its receiver; a value receiver is the method's own copy,
  which it may change without the caller seeing it.
- **Calls** `x.M(args)`, also in package initialization, of a method of
  `x`'s type or promoted through embedded fields and embedded pointers, at
  any depth, with a method of the outer type hiding a promoted one. If `M`
  has a pointer receiver and `x` (or the embedded field reached) is
  addressable and not a pointer, the call takes its address, `(&x).M()`,
  which puts the variable on the heap as `&x` does (§Pointers); if `M` has a
  value receiver and a pointer is reached, the value it points to is
  copied, `(*p).M()`.
- **Order.** The receiver is evaluated first, then the arguments, left to
  right (D-024). A value receiver is copied, and a nil pointer to it found,
  before a call among the arguments runs; gc does both after such a call
  (V-4).
- **Nil.** A pointer receiver may be nil: `p.M()` with `p` nil calls `M`
  without a check, as in Go. A value receiver through a nil pointer, or a
  method promoted through a nil embedded pointer that the path must
  follow, panics with gc's message (`invalid memory address or nil pointer
  dereference`) before the method runs.

Still `unsupported`: methods of other defined types, whose declarations
are rejected (`package-level-declaration`); interfaces. Method values and
method expressions are func values (§Func values).

## Func values (Phase 8 increment 8.2)

The gate also accepts func values (D-039):
- **Types** `func(params) results`, not variadic, of supported types, for
  variables, package-level variables, parameters, results, struct fields,
  and array and slice elements. The zero value is `nil`; a func value
  compares with `nil` by `==` and `!=`, and with nothing else, as in Go.
- **Values**: the program's functions, by name; method values `x.M`, which
  evaluate `x` and bind the receiver at once, a copy of a value receiver
  or the pointer of a pointer receiver, with the implicit `&` and `*` and
  promotion of a call (§Methods); and method expressions `T.M` and
  `(*T).M`, whose first parameter is the receiver.
- **Calls** of any expression of func type: the function value is
  evaluated, then the arguments, left to right (D-024); then a nil func
  value panics with gc's message (`invalid memory address or nil pointer
  dereference`), after the arguments' calls have run, as with gc.
- **Nil receivers.** A method value of a value receiver through a nil
  pointer panics when it is evaluated, as gc's does; one of a pointer
  receiver binds nil and its method is called on nil. `(*T).M` of a value
  method called with nil panics with the nil dereference message, where
  gc's panics with `value method main.T.M called using nil *T pointer`
  (V-7).

Still `unsupported`: variadic func types; `print` and `println` of a func
value (`println-argument-type`), which gc prints as an address.

## Closures (Phase 8 increment 8.3)

The gate also accepts function literals (D-040), of supported func types
and with named results since increment 8.7 (§Named results), whose bodies
follow the rules of a function's.
A literal may use and assign the local variables of the functions around
it, at any depth: it captures them by reference, so it, the function, and
every other closure capturing a variable share it, also after the function
returns. Each captured variable lives on the heap (V-5), and making a
closure that captures variables allocates. A literal called at once,
`func(...) {...}(args)`, is a call of a func value.

Still `unsupported`: capturing a variable declared in a for statement's
init (`loop-variable-address`), of which Go 1.22 gives each iteration its
own copy.

## Interfaces (Phase 8 increment 8.4)

The gate also accepts interfaces (D-041):
- **Types**: interface types of methods with supported signatures, declared
  or literal, embedding others, `any`, and `error`. A value of any
  supported type converts to one whose methods it has, implicitly in
  assignments, arguments, results, fields, and elements, or explicitly,
  `I(x)`; an interface converts to one of fewer methods. The zero value is
  `nil`.
- **Calls** `i.M(args)` call the dynamic type's method: the interface, then
  the arguments, are evaluated, then a nil interface panics with gc's nil
  dereference message, as gc's does. A nil pointer in an interface is not
  a nil interface, and its pointer methods are called on nil. Method values
  `i.M`, which panic at once on nil, and method expressions `I.M` are func
  values.
- **Comparisons** `==` and `!=` of interfaces, with `nil`, and of an
  interface with a value of a type that converts to it: equal if both are
  nil, or if their dynamic types are identical and their values equal. If
  the dynamic type is not comparable, the comparison panics with gc's
  `runtime error: comparing uncomparable type T`.
- **Type assertions** `x.(T)` and `v, ok := x.(T)`, to concrete and
  interface types; a failed one panics with gc's message, such as
  `interface conversion: interface {} is string, not int` or
  `interface conversion: main.T is not main.J: missing method N`, or in
  the comma-ok form gives the zero value and false.
- **Type switches** with cases of one or several types, `nil`, and
  `default`, binding the switch's variable to each clause's type.

Still `unsupported`: an interface embedded in a struct (`non-int-type`),
whose methods would be promoted; a switch statement on an interface, or
with a case value of interface type (`non-int-type`); `print` and
`println` of an interface (`println-argument-type`), which gc prints as
two addresses.

## Maps (Phase 8 increment 8.5)

The gate also accepts maps (D-042):
- **Types**: `map[K]V` of supported key and element types, the key
  comparable, anywhere a type may appear, interfaces included. The zero
  value is `nil`; `==` and `!=` compare a map with `nil` only.
- **Making maps**: `make(map[K]V)` and `make(map[K]V, n)`, whose size hint
  is evaluated and otherwise ignored, so a negative or huge one does not
  panic, as in gc; map literals, whose keys and elements are evaluated and
  stored element by element, in source order, and may be elided literals.
- **Indexing**: `m[k]`, the element or the zero value; `v, ok := m[k]` and
  its `=` and `var` forms; `m[k]` as the target of `=`, `op=`, `++`, and
  `--`. A nil map reads as empty; assigning to its entries panics with
  gc's `assignment to entry in nil map`, after the right side is
  evaluated. An element of array or struct type is copied out when read.
- **Keys** are equal as `==` finds them: `+0` and `-0` are one key, and
  assigning to an existing key stores the new key, as gc does; a NaN key
  is never found, so each assignment adds an entry. A key whose dynamic
  type is not comparable panics: with `runtime error: hash of unhashable
  type T` for an assignment, or a read or delete on a map with entries,
  and with `hash of unhashable type: T` for a read or delete on a nil or
  empty map, gc's two messages.
- **Builtins**: `len`, `delete` (a no-op for a missing key or a nil map),
  and `clear` of a map.
- **Range** (increment 8.5b): `for k, v := range m`, `for k := range m`,
  `for range m`, and the `=` forms, with `_` for the key or element. The
  map is evaluated once; a nil map has no iterations. Variables declared
  by `:=` are new in each iteration, so closures and pointers see each
  iteration's own. Entries are visited in insertion order, not gc's
  random order (V-8), each at most once, with its current element: an
  entry deleted before it is reached is not visited, one added during the
  range may or may not be, and after a `clear` none of the old ones are.

Still `unsupported`: `for range` over anything but a map (`for-range`);
`_` outside a range clause (`blank-identifier`); defined map types (`local-type-declaration`,
`package-level-declaration`); `clear` of a slice (`builtin`); `print` and
`println` of a map (`println-argument-type`), which gc prints as an
address.

## Defer, panic, and recover (Phase 8 increments 8.6a and 8.6b)

The gate also accepts (D-043):
- **`defer`** of a call of a function, method, func value, function
  literal, or interface method, or of `print` or `println`, with arguments
  as for the call, `f(g())` included. The function value (a method's
  receiver included) and the arguments are evaluated at the statement;
  the call runs when the function returns, or when a panic passes through
  it, last deferred first. A nil interface's method panics at the
  statement; a nil func value when the call runs, as in gc.
- **`panic(v)`** of a value of any supported type. A panic, and every
  run-time error, runs the deferred calls of each frame it passes
  through; a deferred call that panics adds a later panic. Then the
  panics are written oldest first, as gc writes them: `panic: ` and the
  value, later ones after a tab; a value whose type has `Error() string` or
  `String() string` is replaced by the method's result first; a string has
  a tab after each newline; a value of any other non-basic type is written
  as `(T) ` and its data word in hex (V-9); `panic(nil)` panics with `panic
  called with nil argument`. The program exits with status 2, with no
  traceback (V-3). A panic during package initialization does the same.
- **`recover()`** (increment 8.6b), of type `any`: it stops the newest
  panic, and returns its value, only when called in the function that a
  deferred call invokes directly while a panic runs it, and only once per
  panic; otherwise it returns nil. The function that deferred the call
  then returns normally, with the results its `return` stored, or zero
  (named results: their values, §Named results),
  and the panics raised since it was entered are dropped. A recovered
  run-time error is an `error` with a `RuntimeError` method, of gc's
  dynamic type (`runtime.boundsError`, `runtime.errorString`,
  `runtime.plainError`, `maps.unhashableTypeError`,
  `*runtime.TypeAssertionError`, or for `panic(nil)`
  `*runtime.PanicNilError`), whose `Error` is gc's message. A recovered
  panic still listed when the program ends is written with ` [recovered]`,
  and one panicked again with the same value once, with ` [recovered,
  repanicked]`.

Still `unsupported`: deferring a builtin other than `print` and `println`,
`recover` included (`defer`).

## Named results (Phase 8 increment 8.7)

The gate also accepts named results (D-044), of every supported type, `_`
included, on functions, methods, and function literals, and a bare
`return` in such a function. Each named result is a local variable, zero
at entry, which the body may assign, take the address of, and capture in
a function literal, like any other local (§Pointers, §Closures).
- `return e1, …` assigns its values to the results after evaluating them
  all, and returns them; a bare `return` returns the results' values.
- In a function with a `defer` statement, the deferred calls run after the
  return's assignment and may read and set the results; the function
  returns their values once the calls end. A function that recovers
  (§Defer, panic, and recover) returns its results' values then: those its
  body or a deferred call stored before, zero for any never assigned.

## Generics (Phase 8 increment 8.8)

Generic functions and generic types are accepted (D-045): type parameter
lists with any constraint, including interfaces with type sets declared
for use as constraints; methods of generic types; instances named with
explicit type arguments (`F[int]`, `Box[P]`), partly or wholly inferred
ones (`F(x)`), and instantiated functions used as values. Each instance
is compiled as its own copy of the generic code, with its type arguments
in place of the type parameters (monomorphization), and each copy must be
inside the subset like any other code: a body that the gate rejects for
some type argument is rejected at the generic source's position. Under a
type parameter, `T(c)` of a constant is not a constant, and stays one in
the copies. Instantiated types have gc's names, such as
`main.Pair[string,*main.P]`.
- Every generic function and type must be instantiated by the program
  (`generic-uninstantiated`): the body of one that is never instantiated
  would be neither checked nor compiled. A generic interface, which has
  no code, is exempt.
- Type arguments must be types that can be written at package level: not
  a type declared in a function, a struct with field tags, or a channel
  (`generic-instance`); a copy must type-check, which rejects for example
  a type switch whose cases become duplicates for some type argument, and
  a type argument's name must not be shadowed where it is used
  (`generic-instance`); at most 1000 instances. Generic type aliases are
  rejected (`package-level-declaration`).
- The generic type's underlying type is checked as a type declaration's
  is: an array, a struct, or an interface.
- An interface that can only be a constraint (one with a type set, such
  as `interface{ ~int | ~float64 }`) is accepted, and left out of the
  program, also in a program without generic code.

## Support matrix

| Feature | Spec section | Subset / phase | State | Tests |
|---|---|---|---|---|
| Single-file `package main`, no imports, `func main()` | Program execution | S1 / Phases 2–3 | Supported (native) | S1-01; IR goldens; Milestone 1 test |
| `var` declarations of `int`, with and without initializers | Variable declarations | S1 / Phases 2–3 | Supported (native) | `run_declaration_forms`, S1-07; `TestLowerConstructs` |
| Short variable declarations, including redeclaration | Short variable declarations | S1 / Phases 2–3 | Supported (native) | S1-06, S1-10; `TestLowerConstructs` |
| Local `const` declarations (untyped or `int`) | Constant declarations | S1 / Phases 2–3 | Supported (native) | S1-05, `run_declaration_forms` |
| Integer literals (decimal, `0x`, `0o`, `0`, `0b`, `_`) | Integer literals | S1 / Phases 2–3 | Supported (native) | `run_declaration_forms` |
| Exact constant expressions | Constant expressions | S1 / Phases 2–3 | Supported (native) | S1-05; `TestLowerConstructs` |
| Assignment and tuple assignment | Assignment statements | S1 / Phases 2–3 | Supported (native) | S1-06; generated programs |
| `+=`, `-=`, `*=`, `++`, `--` | Assignment statements, IncDec statements | S1 / Phases 2–3 | Supported (native) | S1-03, S1-07 |
| Binary `+ - *`, unary `+ -`, parentheses, with wraparound | Arithmetic operators, Integer overflow | S1 / Phases 2–3 | Supported (native) | S1-03, S1-04; `internal/interp` and `internal/amd64` boundary tests |
| Nested blocks, shadowing, empty statements | Blocks, Declarations and scope | S1 / Phases 2–3 | Supported (native) | S1-03, S1-08 |
| `println` with `int` arguments | Bootstrapping | S1 / Phases 2–3 | Supported (native) | S1-02, S1-09 |
| Functions with `int` parameters and at most one `int` result, `return`, direct calls | Function declarations, Calls, Return statements | S2 / 4.2 | Supported (native) | `s2_f01_functions`; `TestNativeCalls`; gate and lowering tests |
| `bool`, comparisons, `!`, `&&`, `\|\|` with short-circuit evaluation | Boolean types, Comparison operators, Logical operators | S2 / 4.1 | Supported (native) | `s2_c02_bool`; `TestNativeControlFlow`; gate and lowering tests |
| `if`/`else`, the three `for` forms, unlabeled `break`/`continue` | If statements, For statements, Break and Continue statements | S2 / 4.1 | Supported (native) | `s2_c01_control_flow`, `s2_c03_recursion` |
| Multiple results and multi-value calls | Function types, Return statements, Assignment statements, Calls | S2 / 4.3 | Supported (native) | `s2_t01_multiple_results`; `TestNativeTuples`; `TestVerifyTuples`; gate tests |
| Every integer type, conversions, `& \| ^ &^`, unary `^`, `print` | Numeric types, Conversions, Arithmetic operators | S2 / 4.4 | Supported (native) | `s2_i01_integer_tables`, `s2_i02_conversions_print`; `TestIntegerTypes`, `TestConversions` |
| `/ % << >>` with the run-time panics (V-3) | Arithmetic operators, Run-time panics | S2 / 4.5 | Supported (native), with divergence V-3 | `s2_d01_division_shifts` to `s2_d06_panic_in_loop`; `TestIntegerTypes`, `TestPanics`, `TestPanicPrefix` |
| Package-level `var` and `const`, initialization in `InitOrder` | Variable declarations, Constant declarations, Package initialization | S2 / 4.6 | Supported (native), with divergence V-4 | `s2_g01_package_vars` to `s2_g05_init_calls_main`; `TestNativeGlobals`, `TestGlobalsAndInit`, `TestVerifyGlobals`, `TestGlobalReadOrder`; gate tests |
| Expression `switch` on `bool` and integer tags, unlabeled `break` | Switch statements, Break statements | S2 / 4.7 | Supported (native) | `s2_s01_switch`, `s2_s02_switch_panic`; gate tests; `TestLowerSwitch` |
| `float64`, `float32`: arithmetic, comparisons, conversions, constants, calls, globals, printing | Floating-point types, Arithmetic operators, Comparison operators, Conversions | S3 / 6.1 | Supported (native) | `s3_f01_float_arith` to `s3_f04_float_calls`; generated float programs; `TestNativeFloat`, `TestPrintFloat`, `TestVerifyFloat`; gate tests |
| `string`: constants, variables, calls, globals, `len`, indexing with index panics, printing | String types, String literals, Length and capacity, Index expressions | S3 / 6.2 | Supported (native), with divergence V-3 | `s3_s01_string_basics` to `s3_s07_index_empty`; generated string programs; `TestNativeStrings`, `TestPanicIndex`, `TestVerifyStrings`; gate tests |
| Arrays and structs: type declarations, literals, fields, indexing with index panics, copies, parameters, results, globals, `len`, `==`, `!=` | Array types, Struct types, Composite literals, Selectors, Index expressions, Assignment statements, Comparison operators | S3 / 6.3 | Supported (native), with divergences V-3 and V-4 | `s3_a01_struct_basics` to `s3_a09_aggregate_swap`; generated array and struct programs; `TestLayoutAgainstGc`; `TestNativeMemory`, `TestFrameMems`, `TestMemoryRoundTrip`, `TestVerifyMem`; gate tests |
| Pointers: `*T`, `&`, `new`, `*p`, auto-dereference, nil checks, address-taken variables on the heap, `==`, `!=` | Pointer types, Address operators, Allocation, Selectors, Index expressions, Run-time panics | S4 / 7.1 | Supported (native), with divergences V-3 and V-4 | `s3_p01_pointer_basics` to `s3_p08_nil_address`; generated pointer programs; `TestNativeHeap`, `TestHeap`, `TestHeapErrors`; gate tests |
| Slices: `make`, literals, indexing, two- and three-index slice expressions of slices, arrays, and pointers to arrays, `len`, `cap`, `append`, `copy`, nil comparisons | Slice types, Making slices, Index expressions, Slice expressions, Appending to and copying slices | S4 / 7.2 | Supported (native), with divergences V-3, V-5, and V-6 | `s3_l01_slice_basics` to `s3_l08_nil_array_slice`; `TestNativeMove`, `TestNativeBounds`, `TestMove`, `TestBoundsMessage`, `TestVerifySlices`; gate tests |
| Strings at run time: `+`, `+=`, `s[i:j]`, `string(b)`, `[]byte(s)`, `append(b, s...)`, `copy(b, s)`, `==`, `!=`, `switch` on strings, `==` of arrays and structs holding strings, `< <= > >=`, rune literals, `string(x)` of integers, `[]rune(s)`, `string(rs)` | Arithmetic operators, Slice expressions, Conversions, Appending to and copying slices, Comparison operators, Switch statements, Rune literals | S4 / 7.3 | Supported (native), with divergences V-3 to V-6 | `s3_c01_concatenation` to `s3_c14_utf8_tables`; generated string operations and rune statements; `TestStringEquality`, `TestStreqFunc`, `TestStringOrder`, `TestRuneConversions`, `TestGeneratedStringFuncs`, `TestMemoryPreconditions`; gate tests |
| Methods: declarations on defined array and struct types, value and pointer receivers, calls with the implicit `&` and `*`, promotion through embedded fields and pointers, pointer receivers on nil | Method declarations, Method sets, Selectors, Calls | S5 / 8.1 | Supported (native), with divergences V-3, V-4, and V-5 | `s5_m01_method_basics` to `s5_m05_type_aether`; generated method programs; `TestMethodPrograms`, `TestMethodReceiverOrder`; gate tests |
| Func values: func types, functions as values, calls of func values, `nil` and comparison with it, method values, method expressions | Function types, Method values, Method expressions, Calls, Comparison operators | S5 / 8.2 | Supported (native), with divergences V-3 to V-5 and V-7 | `s5_f01_func_values` to `s5_f04_nil_method_value`; generated func value programs; `TestFuncValuePrograms`, `TestNilMethodExpression`, `TestIndirectCalls`, `TestVerifyIndirect`; gate tests |
| Closures: function literals capturing the variables around them by reference, nested, returned, stored, and called at once | Function literals | S5 / 8.3 | Supported (native), with divergences V-3 to V-5 | `s5_c01_closures`, `s5_c02_closure_panic`; generated closure programs; `TestClosurePrograms`; gate tests |
| Interfaces: types, conversions, method calls, method values and expressions, comparisons, type assertions, type switches | Interface types, Conversions, Type assertions, Type switches, Comparison operators | S5 / 8.4 | Supported (native), with divergences V-3 to V-5 | `s5_i01_interfaces` to `s5_i06_nil_interface_call`; generated interface programs; `TestInterfacePrograms`, `TestGcName`, `TestPanicMsg`, `TestPanicPrefix`; gate tests |
| Maps: types, `make`, literals, `m[k]`, comma-ok reads, assignments with `op=`, `++`, `--`, `delete`, `len`, `clear`, nil comparisons, interface keys, `for range` | Map types, Making slices, maps and channels, Composite literals, Index expressions, Deletion of map elements, Clear, Length and capacity, For statements with range clause | S5 / 8.5 | Supported (native), with divergences V-3 to V-5 and V-8 | `s5_k01_map_basics` to `s5_k09_map_range`; generated map programs; `TestMapPrograms`, `TestMapRangeOrder`, `TestPanicPrefix`; gate tests |
| Defer, panic, and recover: `defer` of calls of functions, methods, func values, literals, interface methods, `print`, and `println`; `panic(v)`; unwinding through deferred calls, run-time errors included; `recover`, with run-time errors as gc's `error` values | Defer statements, Handling panics, Run-time panics | S5 / 8.6 | Supported (native), with divergences V-3 to V-5 and V-9 | `s5_d01_defer_basics` to `s5_d11_repanic`; `TestDeferPrograms`, `TestPanicMsg`, `TestPanicPrefix`; gate tests |
| Named results: variables zero at entry, bare `return`, set by deferred calls and by recovering functions | Function declarations, Return statements, Defer statements | S5 / 8.7 | Supported (native), with divergences V-3 to V-5 | `s5_n01_named_results`, `s5_d12_named_defer`, `s5_d13_named_recover`; generated named-result programs; `TestNamedResultPrograms`, `TestDeferPrograms`; gate tests |
| Generics: generic functions and types, methods of generic types, explicit and inferred instantiation, constraints with type sets, instantiated functions as values | Type parameter declarations, Instantiations, Type inference | S5 / 8.8 | Supported (native), with divergences V-3 to V-5 | `s5_g01_generic_funcs` to `s5_g04_generic_panic`; generated generic programs; `TestGenericPrograms`, `TestInstantiate`; gate tests |
| Unbounded recursion: stack overflow (V-2) | — | S2 / 4.2 | Supported (native), with divergence V-2 | `TestStackOverflowAgainstGc`, `TestStackOverflow`, `TestNativeStackOverflow`, `TestStackBoundary` |
| Everything else | — | — | Unsupported | gate tests, corpus |

"Tests" names corpus programs (`cmd/aether/testdata/check`, cases from
§Subset S1) and Go tests. Every corpus program expected to run, and 200
generated programs, are run by `TestDifferentialAgainstGc`, which requires
the stdout, stderr, and exit status of both the interpreter and the
executable built by `aether build` to equal those of the gc-built binary.
`internal/amd64` tests also run every operation at the integer boundaries
natively. These are finite tests, not a proof of correctness.
