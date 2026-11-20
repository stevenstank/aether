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

Since Phase 7 increment 7.2, `generateSlices` (`gen_slice_test.go`) writes
programs of its own, from a sixth random stream, so that `generate`'s are
unchanged; `TestDifferentialAgainstGc` runs 100 of them, `TestFrameBound`
100, and `FuzzGenerated` the one of each seed. Their `main` is straight-line
code over slices of six element types (one zero-size), with a global slice
and array, a slice in a struct, and functions that sum, fill, and grow: nil,
empty, literal, and made slices; two- and three-index slices of slices,
arrays, and pointers to arrays; element writes; appends of values and of
slices, within and beyond the capacity; copies, also within one array; and
prints. The generator models every length and capacity and which slices share
an array, so it keeps every index and bound in range; one program in five
ends with a deliberate panic: an index, a slice bound, `makeslice`, a nil
pointer to an array, or `growslice`. A capacity after a growing append
differs between gc and Aether (V-6): it is never printed, and such a slice
is appended to only when nothing shares its array, so differential results
never depend on it. Aether's own rule is tested apart, against a model of
D-036, by `TestAppendGrowthRule`. On 2026-11-15, slice programs of seeds 1001
to 2000 all gave gc's output in the interpreter, `aether build`, and `-O0`;
187 of them panic (52 index, 45 slice bounds, 34 and 29 `makeslice` length
and capacity, 15 nil pointer, 12 `growslice`). Negative controls in lowering,
run by hand and reverted: an `append` that always grows fails 15 of the 100
slice programs (and `s3_l01`), a `copy` of the longer length 58, and a
minimum capacity of 8 `TestAppendGrowthRule`.

Since Phase 7 increment 7.3, a seventh random stream (`gen_str_test.go`)
adds up to `maxStrOpStmts` (8) string operations to `generate`'s programs:
concatenations of two or three operands (literals, variables, slices of
variables, `string([]byte(s))`, `string(s)`), assignments and `+=` of them,
each followed by a cut to at most 24 bytes so that loops cannot double a
string; slices with constant bounds under a length test, or with a
variable's bounds, unguarded one time in six so that the program may panic;
a `[]byte(s)` changed and printed beside `s`, which must not change (aliasing);
and `append` and `copy` of a string's bytes. A `string(b)` is never in one
statement with a call that changes `b`, which gc evaluates first (V-4); the
first version of the generator did that and found the divergence, now
documented. The cap keeps `main` within promotion's limit
(`TestBuildBackendLimits`). On 2026-11-16, of seeds 1001 to 2000, the 449
valid programs with string operations (9 of them panic with a slice bound)
all gave gc's output in the interpreter, `aether build`, and `-O0`. Negative
controls in lowering, run by hand and reverted, against
`TestDifferentialAgainstGc`: a concatenation that moves every operand to
offset 0 fails 82 generated programs and `s3_c01` to `s3_c03`, and a
`[]byte(s)` that shares the string's bytes 33 generated programs (25 of
them by storing into a program string in the interpreter) and `s3_c03`.

Since increment 7.3b the same stream also compares strings with `==` and
`!=`: equal bytes at other addresses, prefixes, and a copy with one byte
flipped at its start, middle, or end, of the same length; switches on
strings whose case values may call `strs`, which prints, so the order of
evaluation shows (constant case values never repeat, which Go rejects); and
`==` and `!=` of arrays and structs that hold strings, also after a copy
whose strings are remade at other addresses and then after one of them
grows. `TestStringEquality` checks every pair of 13 strings (empty, NUL
bytes, invalid UTF-8, prefixes, differing first or last bytes), as constants
and as bytes made at run time, against Go's own `==`, without gc.
Negative controls, run by hand and reverted, against
`TestDifferentialAgainstGc` and `TestStringEquality`: an `.streq` that
compares only the lengths fails 17 generated programs, `s3_c07` to `s3_c09`,
and `TestStringEquality`; one that skips the last byte `s3_c07` to `s3_c09`
and `TestStringEquality`; and an aggregate comparison that compares a
string's pointer instead of its bytes 1 generated program and `s3_c09`.

Since increment 7.3c an eighth random stream (`runeStmt` in
`gen_str_test.go`) adds up to `maxRuneStmts` (4) statements that read
string and integer variables but change none, so every line the other
streams generate is unchanged (of seeds 0 to 2999, 2,602 programs gained
lines and none lost or changed one): `< <= > >=` of strings, also against
equal bytes at another address, a prefix, and a concatenation; `[]rune(s)`
and back; runes appended, among them values that are not code points, then
encoded; `string(x)` of integer variables of every type; and the runes of
a string with invalid UTF-8 appended. On 2026-11-16, of seeds 1001 to
2000, the 465 valid programs with these statements (90 of them panic) all
gave gc's output in the interpreter, `aether build`, and `-O0`.
`TestStringOrder` checks every ordered pair of the 13 strings of
`TestStringEquality`, and `TestRuneConversions` `[]rune(s)`, `string(rs)`,
and `string(x)` of 29 strings and 19 values (boundaries of each encoded
length, surrogates, invalid UTF-8 of each kind, values outside the rune
range), against Go's own conversions, without gc; `s3_c14` decodes every
two-byte string and checks them against gc. Negative controls, run by hand
and reverted, against `TestDifferentialAgainstGc` and those tests: an
`.strcmp` that ignores the lengths of a common prefix fails 49
generated programs, `s3_c11`, and `TestStringOrder`; one that compares
bytes as signed 46 generated programs, `s3_c11`, and `TestStringOrder`; an
`.decoderune` that accepts overlong forms, surrogates, and runes above
U+10FFFF 3 generated programs, `s3_c13`, `s3_c14`, and
`TestRuneConversions`; and an `.encoderune` that encodes surrogates
18 generated programs, `s3_c12` to `s3_c14`, and `TestRuneConversions`.

Since Phase 7 increment 7.1, a fifth random stream (`gen_ptr_test.go`)
declares a list node type `PN`, a function `pinc` that writes through its
pointer parameter and returns the address of its other one, a global list,
and up to two pointer globals. Statements on pointers, at most `maxPtrStmts`
(30) per program, declare pointers from `&x` of a variable, field, or
element, `new(T)`, `&T{…}`, `nil`, or another pointer; reassign and swap
them; read and write through them with `op=`, `++`, `--`, and implicit
indirection in selectors and indices; compare them and switch on them; call
`pinc` alone in its statement (V-4); take a pointer to a pointer; write
through a pointer to a variable and then read the variable directly
(aliasing); and push onto, walk, copy, and compare nodes of the list, some
literals eliding `&PN`. Accesses are usually under a nil test and otherwise
may panic. No loop counter's address is taken (`loop-variable-address`). On
2026-11-15, of seeds 1001 to 3000, 1,171 were valid, 892 of them with pointer
statements in `main`; 157 of those panic, only 8 with a nil dereference. All
892 gave gc's output in the interpreter, `aether build`, and `-O0`. The
generated programs found two problems, both fixed with regression tests:
the gate's frame bound did not count the temporary of a composite literal or
call that is indexed or selected from, as in `[2]int{1, 2}[i]` or `f()[1]`
(a gap since increment 6.3; `TestFrameBound`), and register coalescing put
`x &^ y` in x's register, which needs one more instruction than y's
(`TestCoalesceAndNot`, `TestCoalesceNoLarger`). Negative
controls in lowering, run by hand and reverted, against
`TestDifferentialAgainstGc`: `&` ignoring a field's offset (1 generated
program and 2 corpus programs fail), no nil checks (1 generated, 5 corpus),
and an address-taken parameter not copied to the heap (3 generated, 1
corpus).

Since Phase 8 increment 8.1, `generateMethods` (`gen_method_test.go`)
writes programs of its own, from a ninth random stream, so that the other
generators' programs are unchanged; `TestDifferentialAgainstGc` and
`TestFrameBound` run 100 of them, `TestMethodPrograms` 30 also under `-O0`
and ABI-R1, and `FuzzGenerated` the one of each seed. Their types are
fixed: a struct `A` of an int, an int8, a string, and an array; `B`
embedding `A`, `C` embedding `*A`, `D` embedding `B`, and an array type.
The bodies of their methods are random: value receivers that read fields,
pointer receivers that write them, value receivers that write their own
copy, directly or through the implicit `&` of a pointer method (which puts
the parameter on the heap), and methods that call others promoted through
embedded fields. `main` calls them on variables, pointers, `*p`, embedded
fields at every depth, through an embedded pointer, array and slice
elements, literals, and call results, and prints every field at the end;
one program in about seven ends with a deliberate nil dereference: a value
method through a nil pointer, or a method promoted through a nil embedded
pointer. A call that may write a variable is alone in its statement or
println's only argument, and a method argument is a constant or a call that
writes nothing (V-4). Negative controls in lowering, run by hand and
reverted, against `TestMethodPrograms` and `TestDifferentialAgainstGc`: a
value receiver passed without a copy fails `s5_m01` and 5 of the 100
generated programs; a pointer receiver passed a copy fails generated
programs from the first seed on; no nil check on a value receiver through a
pointer stops a generated program in the interpreter on a read of address
0; no nil check on a path through a nil embedded pointer fails `s5_m04`;
and the implicit `&` not making its variable address-taken fails `s5_m01`
(its `escape`) in the interpreter on a read of a dead frame object.
On 2026-11-16, method programs of seeds 1001 to 2000 all gave gc's output
in the interpreter, `aether build`, and `-O0`; 162 of them panic.

Since Phase 8 increment 8.2, `generateFuncValues` (`gen_funcvalue_test.go`)
writes programs of its own, from a tenth random stream;
`TestDifferentialAgainstGc` and `TestFrameBound` run 100 of them,
`TestFuncValuePrograms` 30 also under `-O0` and ABI-R1, and `FuzzGenerated`
the one of each seed. Five functions of type `func(int, int) int` have
random bodies; a struct `S` holds an int and a func value, with a value
method and a pointer method, and `W` embeds `S` and holds another. `main`
keeps func values in variables, a package-level variable, a slice, an
array, fields, and call results, makes method values and method
expressions, directly and promoted, and calls them all, directly and
through higher-order functions; one program in about seven ends with a call
of a nil func value or a method value of a value method through a nil
pointer. A call that writes a variable is alone in its statement with a
constant argument, and a method value is made in a statement of its own
(V-4). Negative controls, run by hand and reverted, against
`TestFuncValuePrograms` and `TestDifferentialAgainstGc`: no nil check on a
call of a func value stops a generated program in the interpreter on a read
of address 0; a method value binding its value receiver's address instead
of a copy, and a bound wrapper calling its method on the closure's copy
instead of a fresh one, each fail `s5_f02` in every path; and ABI-R1
loading the code address after the argument moves fails `checkR1` on
generated programs. On 2026-11-17, func value programs of seeds 1001 to 2000
all gave gc's output in the interpreter, `aether build`, `-O0`, and ABI-R1;
152 of them panic.

Since Phase 8 increment 8.3, `generateClosures` (`gen_closure_test.go`)
writes programs of its own, from an eleventh random stream;
`TestDifferentialAgainstGc` and `TestFrameBound` run 100 of them (the
latter checks each literal's own frame bound), `TestClosurePrograms` 30
also under `-O0` and ABI-R1, and `FuzzGenerated` the one of each seed.
`main` declares an int pair, a string, an array, a struct, and a slice, and
makes closures with random bodies that read and assign them, closures made
by closures and by a function with variables of their own, and one closure
per iteration of a loop over the body's variable; it calls them directly
and through a higher-order function, printing the variables between calls.
One program in about seven ends with a closure indexing out of range or a
call of a nil func value. A closure's call is the only call of its
statement, with constant operands (V-4). Negative controls, run by hand
and reverted, against `TestClosurePrograms`: captured variables not made
address-taken stop lowering with an internal error on the first generated
program; the captured Ptrs read one word too low fail it in the
interpreter; and closures capturing a copy of each variable, not the
variable, give wrong output from the first generated program in every
path. On 2026-11-18, closure programs of seeds 1001 to 2000 all gave gc's
output in the interpreter, `aether build`, `-O0`, and ABI-R1; 126 of them
panic.

Since Phase 8 increment 8.4, `generateIfaces` (`gen_iface_test.go`) writes
programs of its own, from a twelfth random stream;
`TestDifferentialAgainstGc` and `TestFrameBound` run 100 of them,
`TestInterfacePrograms` 30 also under `-O0` and ABI-R1, and `FuzzGenerated`
the one of each seed. Its types are a struct with value methods, a struct
with pointer methods, one of which writes it, and an array type; its
interfaces embed one another. `main` converts values of these and of
predeclared types to the interfaces and to `any`, calls methods through
them, makes method values and expressions, compares interface values,
asserts with the comma-ok form, and switches on types. One program in about
seven ends with a failed assertion, a call on a nil interface, or a
comparison of uncomparable values. The panic comparison rule accepts gc's
`panic: interface conversion:` lines as it does run-time errors. Negative
controls, run by hand and reverted, against `TestInterfacePrograms`: an
itab wrapper calling a value method on the interface's own copy fails
`s5_i01` in every path, and an equality function that finds every two
values of a type equal fails a generated program in every path; while
the increment was written, `branches` not knowing that interface
operations branch failed the verifier on `println` operands. On
2026-11-18, interface programs of seeds 1001 to 2000 all gave gc's output in
the interpreter, `aether build`, `-O0`, and ABI-R1; 122 of them panic.

Map programs (`gen_map_test.go`, increment 8.5, D-042) come from their own
random stream. `TestDifferentialAgainstGc` and `TestFrameBound` run 100,
`TestMapPrograms` 30 also under `-O0` and ABI-R1, and `FuzzGenerated` the
one of each seed. Their maps have int, string, struct, interface, and
float64 keys, and int, string, slice, and map elements; `main` assigns,
uses `op=`, `++`, and comma-ok reads, deletes, clears, grows maps in
loops, ranges over maps to sum, count, delete or change the entry
visited, or break after a number of entries, and passes a map to a
function that changes it. It prints every entry at the end by looking up
each key of a small key domain, so nothing printed depends on iteration
order (V-8), which `TestMapRangeOrder` pins without gc. One program in
about seven ends with an assignment to a nil map or an unhashable key. The
panic comparison rule also accepts gc's `panic: assignment to entry in nil
map` and `panic: hash of unhashable type: ` lines, which have no `runtime
error: `.

Defer, panic, and recover programs (increments 8.6a and 8.6b, D-043)
are corpus programs `s5_d01` to `s5_d11`, which `TestDeferPrograms` also runs under `-O0` and
ABI-R1; there is no generator for them yet. Since D-043 the panic
comparison rule accepts any `panic: ` line with a message, and its Q takes
in the following lines that begin with a tab: gc's later panics and the
lines of a value with newlines.

Named-result programs (`gen_named_test.go`, increment 8.7, D-044) come
from their own random stream. `TestDifferentialAgainstGc` and
`TestFrameBound` run 100, `TestNamedResultPrograms` 30 also under `-O0`
and ABI-R1 (with corpus program `s5_n01`; `s5_d12` and `s5_d13` run in
`TestDeferPrograms`), and `FuzzGenerated` the one of each seed. Functions
`f0` to `f3` have the named results `(r int, s string, p P, a [2]int)`;
their bodies assign them directly, through a pointer, and from a literal
that captures them, and return early or at the end, bare or with values;
some defer calls that set the results or print them, and some recover and
may then panic, by `panic` or by indexing out of range. `main` prints each
call's results. One program in about seven ends with an unrecovered panic
from a function whose deferred call prints its named result. Negative
control, run by hand and reverted: a `return` with values in a function
with `defer` that does not assign the results makes all 30 generated
programs differ from gc in `TestNamedResultPrograms`. On 2026-11-20,
named-result programs of seeds 1001 to 2000 all gave gc's output in the
interpreter, `aether build`, `-O0`, and ABI-R1; 125 of them panic.

Generic programs (`gen_generic_test.go`, increment 8.8, D-045) come from
their own random stream. `TestDifferentialAgainstGc` and `TestFrameBound`
run 100, `TestGenericPrograms` 30 also under `-O0` and ABI-R1 (with corpus
programs `s5_g01` to `s5_g04`), and `FuzzGenerated` the one of each seed.
Generic functions `i0` to `i2` (integer type sets), `n0` and `n1` (every
number type), and `f0` (floats) have random bodies over a type parameter,
with constant conversions `T(c)` that must stay non-constant in the copies,
shifts by constants up to 69, and calls of another instance; `main` calls
each with random type arguments, explicit or inferred, directly, as func
values, through `Apply` and `Fold`, and into the generic types `Box` and
`Pair`, on values that wrap around in the narrow types. Negative controls,
run by hand and reverted: copying `T(c)` as a constant conversion makes the
generated programs unsupported (a constant overflows in a copy), and
writing unexported field names unqualified in type arguments makes
`s5_g02` differ from gc.

Goroutine and channel programs (`gen_chan_test.go`, Phase 10 increment
10.4, D-048) come from their own random stream. `main` runs 2 to 6 random
blocks:
- pipelines of 1 to 3 stages over buffered and unbuffered channels;
- worker pools with a closer goroutine, over struct elements with strings;
- `select` loops over two producers that drop closed channels;
- a server goroutine answering requests on reply channels carried in a
  struct, stopped through a `select`;
- buffered channels and non-blocking selects that only `main` uses;
- a goroutine that recovers its own panic and sends the value.

Only `main` prints, and only what does not depend on the schedule (V-10).
About one program in four ends in a deadlock, a close of a closed channel,
or a send on a closed channel in a goroutine.

Where they run:
- `TestDifferentialAgainstGc` runs 60 of them.
- `TestChanPrograms` runs 30, with the `s6_*` corpus programs, also under
  `-O0` and ABI-R1.
- `TestGCStress` runs 10 under stress.
- `FuzzGenerated` runs the one of each seed.
- `FuzzGC` builds the one of each seed and runs it under stress against
  the interpreter, whose schedule is the same.

On 2026-11-20, the programs of seeds 1001 to 1200 all gave gc's output
in the interpreter and natively; 55 of them end in a panic or fatal
error.

Stress tests:
- `TestGCMemoryBound` runs a million goroutines one at a time (7 MiB
  peak) and a `select` that blocks two million times (7 MiB), each under
  64 MiB.
- It also runs 100,000 goroutines blocked at once (417 MB peak, gc's
  binary 271 MB) under 1 GiB.
- `TestStackOverflowAgainstGc` overflows a goroutine's stack.
- `internal/amd64`'s `TestGoroutinePrograms` reaches the 131,072-goroutine
  limit and reuses stacks past it.

Allocation programs (`gen_alloc_test.go`, Phase 9, D-046) come from their
own random stream, for the garbage collector. A loop of 1000 to 4000
iterations runs random statements, each on some iterations, that build
and drop binary trees, linked lists, map entries of slices of pointers,
closures capturing pointers, interfaces, run-time strings, large slices,
tails of slices, and pointers to arrays inside structs; trees, the map,
tails, and inner pointers are package-level variables. Checksums that
read everything reachable are printed along the way and at the end.
`TestGCStress` runs 60 of them, with every corpus program expected to run
and 10 programs of each other generator, built by `aether build` and with
`-O0`, under `AETHER_GCSTRESS=1` (a collection every 8 KiB plus an eighth
of the live bytes, freed objects overwritten with `0xdeadbeefdeadbeef`),
and compares them with gc. Negative controls, run by hand and reverted:
without the stack scan, or without the `.bss` scan, an allocation program
hangs until the run timeout; marking only through pointers to an object's
start makes allocation programs differ from gc. On 2026-11-20,
allocation programs of seeds 1001 to 1300, built by `aether build`, all
gave gc's output under stress. The interpreter never frees, so these run
natively only, and `FuzzGenerated` does not reach them; `FuzzGC`
(increment 9.3) fuzzes them instead: each input is a seed of
`generateAllocs`, whose program is built by `aether build`, run under
stress, and compared with the interpreter, which never frees and so needs
no gc build; a program over the interpreter's step budget is skipped.

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

#### Fuzz incident FI-1 (closed 2026-11-03: unexplained, not reproduced; recurred 2026-11-14 with its cause found)

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

**Recurrence (2026-11-14).** A 60 s `FuzzCheck` run on the Phase 6.3 tree,
output captured in full, failed the same way: 2,785,324 executions, then at
the end of the run `--- FAIL: FuzzCheck (61.31s)` with only the message
`context deadline exceeded`, and no input written to `testdata/fuzz`. The
cause is a race in the engine, not a property failure:
- In `internal/fuzz.CoordinateFuzzing` (go1.26.1), the coordinator wakes on
  the `-fuzztime` context's `Done` and calls `stop(ctx.Err())`, which
  suppresses the error only if it equals `fuzzCtx.Err()`, a child context.
- `context.(*cancelCtx).cancel` closes the parent's `Done` before it cancels
  the children, and a child's `Err` is a lock-free load, so for a moment the
  child reports `nil`. The deadline error then becomes the run's result.
- A standalone program of that pattern (a parent `WithTimeout`, a child
  `WithCancel`, wait for the parent's `Done`, test the child's `Err`) saw the
  child `nil` in 2 of 20,000 trials.

The rerun on the same tree passed (2,805,662 executions). The race explains a
failure at the end with no input and no other message, as FI-1 had; whether
FI-1's lost message was this one cannot be known.

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
- From Phase 11 it is used for pass-rate reporting. `TestGorootRun`
  (`cmd/aether/goroot_test.go`, D-049) builds every test of testdir's
  directories whose recipe is exactly `// run`, skips those whose build
  constraints exclude linux/amd64, and runs the rest in `GOROOT/test`. A
  test passes on exit status 0 with stdout and stderr together equal to its
  `.out` file, or empty without one: testdir's rule. Every other test is
  `unsupported FEATURE` (rejected by Aether) or `bug KIND` (`invalid`,
  `internal`, `output`, `exit N`, `signal`, `timeout`). The reviewed golden
  `cmd/aether/testdata/goroot/run.golden` lists each test's class and ends
  with the counts, which are the published pass rate. A changed class fails
  the test until the golden is regenerated with `-update` and reviewed.
  - Baseline (2026-11-20, increment 11.1, go1.26.1): 1,008 tests, 3
    skipped, 265 pass (26.4 %), 740 `unsupported`, 0 bugs. Before 11.1, a
    script applying the same rule passed 207 and found 2 `internal` bugs.
  - After increment 11.2 (2026-11-20): 395 pass (39.3 %), 610
    `unsupported`, 0 bugs.
  - After increment 11.3 (2026-11-20): 439 pass (43.7 %), 566
    `unsupported` (494 of them for an import), 0 bugs.
  - After increment 11.4 (2026-11-20): unchanged, 439 pass. Nine tests
    now report a directive before their import, which the loader rejects
    after the pre-check.
  - After increment 11.5 (2026-11-20): 467 pass (46.5 %), 538
    `unsupported` (376 for an import, almost all of `runtime` through
    `fmt` or `os`; 59 for a `//go:linkname` variable, mostly in `reflect`;
    50 for generic code of another package, mostly `sync`), 0 bugs.
  - After increment 11.6 (2026-11-20): 516 pass (51.3 %; 49 more, none
    lost), 489 `unsupported` (334 `stdlib-package`, almost all a standard-library
    package that needs more of `runtime`, `sync`, or `sync/atomic` than
    Aether's: `runtime.Caller` in `internal/reflectlite`, `sync.Pool` in
    `io`, `runtime.Goexit` in `iter`), 0 bugs.
- **Programs of several packages** (D-049, increment 11.4).
  `TestPackagePrograms` builds each program under
  `cmd/aether/testdata/packages/cmd`, in the module
  `testdata/packages`, with gc and with Aether (default and `-O0`), and
  compares exit status, stdout, and stderr. `TestPackageRejects` writes
  small modules and checks how cycles, missing and excluded packages,
  standard-library imports, and generics across packages are classified.
- **Standard-library programs** (D-049, increment 11.5).
  `TestStdPrograms` builds each program of `cmd/aether/testdata/std`,
  which use `unicode/utf8`, `unicode`, `math/bits`, `math`, and `strconv`
  compiled from GOROOT, with gc and with Aether (default and `-O0`), and
  compares them. `TestStdRejects` covers assembly, `runtime`, `internal`,
  `unsafe`, and unreached code; `TestGOROOTVersion` the GOROOT check.
  Since increment 11.6 the programs also use `runtime`, `os`, `sync`, and
  `sync/atomic`; each also runs in the interpreter, and a program ending
  in a fatal error (`syncfatal`, `syncdeadlock`) is compared by the panic
  rule. `TestStdoutErrors` runs a program with its standard output closed
  and on `/dev/full` under gc and Aether.
- It is read in place from GOROOT, not copied into the repository. Copying would
  require handling the BSD license.
