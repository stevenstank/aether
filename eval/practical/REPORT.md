# Aether practical evaluation: report

**Date:** 2026-11-24. **Aether:** commit `d1ec4d9` (end of the reliability
campaign), with the compiler and runtime unmodified. **Reference:** gc,
`go1.26.1 linux/amd64`. **Host:** Linux 6.18 (WSL2), x86-64, GNU binutils
2.42. `results/summary.md` records the full environment of every run. To
reproduce, run `eval/practical/run.sh` (see `README.md`).

## Verdict

Every program Aether compiled ran correctly. 3 of the 10 programs built,
and every one of their cases produced the same stdout, stderr, and exit
status as gc, in both Aether modes: 26 cases × 2 modes = 52 comparisons,
52 identical. This held for:
- large inputs (1.6 MB of text)
- allocation-heavy workloads that exercise the collector
- floating-point simulation
- invalid UTF-8 input
- every error and usage path

No miscompilation, runtime crash, or internal compiler error occurred
anywhere.

The other 7 programs did not build. Each was rejected with a correct,
well-located `unsupported` diagnostic (exit 3). Every rejection was caused
by a **standard-library gap**, never by a language construct. The most
common blocker was `errors.As`, which stopped 4 of the 7 programs:
02-calc, 03-ledger, 06-graph, and 08-interp. 03-ledger and 06-graph had no
other blocker. The probes found 7 unsupported APIs in total.

In practice, Aether runs naturally written programs correctly when they
stay within `fmt`, `strings`, `strconv`, `sort`, `slices`, `bufio`,
`unicode`, `math` (no `Exp`/`Log`/`Pow`), `container/*`, `encoding/csv`, and
`sync`. A typical program written without regard to its subset is likely
to reach an unsupported API. The ones these programs hit were `errors.As`,
`time`, `math/rand`, `math.Pow`, `os.ReadFile`, `encoding/binary`, and
`hash/crc32`. Correctness was not the limit here; library coverage was.

One limit on this evidence: because 7 programs never built, their core
code was not tested by Aether at all. That code is the generics-heavy
containers, the interpreter's interfaces and panic/recover control flow,
the goroutine/channel pipelines, and the multi-package module. The project's
own test suites cover those features. This evaluation adds no real-program
evidence for them.

## Programs ranked by complexity

Ranked from simplest to most complex by code size, number of files and
packages, algorithmic depth, and the language/runtime features involved.
"Code lines" excludes blank and comment-only lines. The directory numbers
give the order in which the programs were written, which is roughly but not
exactly this ranking.

| Rank | Program | Code lines | Files / pkgs | Cases | What it does | Aether build | Result |
|---|---|---|---|---|---|---|---|
| 1 | 01-wordfreq | 113 | 1 / 1 | 11 | word frequencies of stdin; flags; Unicode words | OK (both modes) | **11/11 pass** ×2 modes |
| 2 | 07-workload | 257 | 1 / 1 | 12 | sieve, binary trees (GC), matmul, n-body, map churn, sorting, string building | OK (both modes) | **12/12 pass** ×2 modes |
| 3 | 03-ledger | 266 | 1 / 1 | 6 | CSV bank ledger: validation, money in cents, overdrafts, monthly report | unsupported | not run |
| 4 | 02-calc | 358 | 1 / 1 | 4 | calculator: tokenizer, recursive-descent parser, variables, functions, error carets | unsupported | not run |
| 5 | 04-textkit | 353 | 1 / 1 | 3 | Unicode text toolkit: Levenshtein, LCS, KMP, RLE, Caesar, wrap, stats | OK (both modes) | **3/3 pass** ×2 modes |
| 6 | 05-collections | 565 | 5 / 1 | 7 | generic AVL tree, heap, LRU (container/list), set, Map/Filter/Reduce; 1M-op stress vs maps | unsupported | not run |
| 7 | 09-pipeline | 286 | 1 / 1 | 12 | concurrent log analyzer: reader → worker pool → aggregator, cancellation | unsupported | not run |
| 8 | 06-graph | 586 | 5 / 1 | 29 | Dijkstra (container/heap), BFS, topo sort with cycle report, Tarjan SCC, Kruskal | unsupported | not run |
| 9 | 08-interp | 1199 | 6 / 1 | 15 | interpreter for a small language: lexer, parser, AST, closures, lists, maps, runtime errors | unsupported | not run |
| 10 | 10-kvstore | 685 | 5 / 4 | 7 | multi-package transactional KV store, checksummed WAL with replay/corruption detection, concurrent clients | unsupported | not run |

The 3 programs that ran together have 26 cases:
- **Normal inputs:** prose; Greek, CJK, and accented text; a full text-tool
  command set; all seven compute kernels.
- **Edge cases:** empty input, input without a final newline, `-n 0`,
  invalid UTF-8 bytes, zero-width wrap, malformed RLE, and unknown
  commands.
- **Invalid usage:** non-numeric, negative, and missing flag values, and
  unknown flags and kernels, with exit status 2 and messages on stderr.
- **Large workloads:**
  - a 1.6 MB, 40,000-line text
  - 4,000 generated text commands
  - kernels at scale 1, 2, and 3: up to 2 million sieve entries, binary
    trees of depth 17, 1 million sorted integers, and 600,000 map
    insertions with deletes

## Results by category

| Category | Count | Details |
|---|---|---|
| Pass (identical to gc) | 52 of 52 comparisons that ran | 01, 04, 07 in default and `-O0` modes |
| Compiler bug | 0 | no wrong output, no `internal` error (exit 4), no invalid assembly |
| Runtime bug | 0 | no crash, hang, GC failure, or output difference |
| Unsupported feature | 7 programs | all standard-library APIs; see below |
| Evaluation-harness issue | 0 in the final run | the harness was checked against a fake compiler (below) |
| Program issue | 2 fixture errors, found and fixed | both in 08-interp scripts; they did not affect any Aether result (below) |
| Diagnostic quality (minor) | 1 | every `unsupported` message says "subset S1" (below) |

### Unsupported features

Aether reports only the first unsupported construct in a program. To find
every blocker, each standard-library API the programs use was also checked
alone with a one-feature probe (`probes/`, `probes/run.sh`). Each probe is
valid Go (gc type-checks it).

| Probe | Aether | Diagnostic (abridged) |
|---|---|---|
| `errors.As` | unsupported | `$AETHER/errors/wrap.go:60:1: function As without a body ... [function-without-body]` |
| `math.Pow` | unsupported | `$GOROOT/src/math/exp_asm.go:11:1: function archExp without a body ... [function-without-body]` |
| `math/rand` (v1) | unsupported | `internal/bisect ... (undefined: atomic.Pointer) [stdlib-package]` |
| `math/rand/v2` | unsupported | `math/exp_asm.go: archExp without a body [function-without-body]` |
| `time` (`Parse`, `Truncate`, `Format`) | unsupported | `internal/bisect ... (undefined: atomic.Pointer) [stdlib-package]` |
| `os.ReadFile` | unsupported | `os.ReadFile, which Aether's package os does not have [stdlib-package]` |
| `encoding/binary` | unsupported | `encoding/binary ... (undefined: sync.Map) [stdlib-package]` |
| `hash/crc32` | unsupported | `hash/crc32 ... (undefined: sync.OnceFunc) [stdlib-package]` |
| `bufio` Reader/Writer/Scanner | supported | |
| `bytes.Buffer`, `bytes.Reader`, `io.ReadFull`, `io.ReadAll` | supported | |
| `container/heap`, `container/list` | supported | |
| `encoding/csv` | supported | |
| `math.Mod`, `Round`, `Sqrt`, `Hypot` | supported | |
| `strconv.QuotedPrefix` | supported | |
| type assertion to a generic pointer type | supported | |

The blockers for each program, from the probes:

| Program | First blocker (reported by Aether) | All blockers among its standard-library uses |
|---|---|---|
| 02-calc | `errors.As` | `errors.As`, `math.Pow` |
| 03-ledger | `errors.As` | `errors.As` only (`encoding/csv` is supported) |
| 05-collections | `math/rand` (via `internal/bisect`) | `math/rand` only (`container/list` is supported) |
| 06-graph | `errors.As` | `errors.As` only (`container/heap` is supported) |
| 08-interp | `os.ReadFile` | `os.ReadFile`, `errors.As` |
| 09-pipeline | `time` (via `internal/bisect`) | `time` only (`sync`, `sync/atomic`, `select` are documented as supported) |
| 10-kvstore | `encoding/binary` | `encoding/binary`, `hash/crc32` |

The probes cover the standard-library APIs. They cannot prove that nothing
else in a program would be rejected or would miscompile once these
blockers are gone: Aether never compiled the rest of the code.

All of these behaviors are documented in `docs/SEMANTICS.md`:
- `errors.As` is listed as unsupported "until Aether has reflection".
- `Exp`/`Log` and their callers are listed as `function-without-body`.
- `os` declares only `Args`, `Exit`, and `Stdin`/`Stdout`/`Stderr`.
- A standard-library package that needs an undeclared `sync`/`atomic` name
  is listed as `stdlib-package`.

So these are documented limits, not defects. Each rejection was immediate,
from 0.07 to 0.31 s, with exit 3 and a position in the source.

### Practical impact of the blockers

- **`errors.As`** (4 of 10 programs) is the most expensive gap. It is the
  standard way to inspect typed errors, and it appeared without being
  sought, in parsers (`*SyntaxError`), CSV handling (`*csv.ParseError`),
  and CLI error reporting. Supporting it would have let 03-ledger and
  06-graph build. Both had no other blocker.
- **`time`** blocks any program that parses or formats timestamps, which
  is common in log, report, and scheduling tools. Its blocker is
  `internal/bisect`'s use of `atomic.Pointer`. This is the same root cause
  that blocks `math/rand`.
- **`math/rand`** blocks seeded randomized testing and simulation, as in
  05-collections.
- **`os.ReadFile`** (and file I/O generally) restricts programs to
  stdin/stdout. 08-interp's only other blocker is `errors.As`.
- **`math.Pow`** is a single call in 02-calc, its `^` operator, but it
  blocks the whole program.
- **`encoding/binary`, `hash/crc32`** block binary formats and
  checksumming (10-kvstore).

### Discrepancies and bugs

None. No case produced different output, exit status, or stderr between
gc and Aether in either mode. No comparison needed the panic-normalization
rule, because none of the three built programs panics on its inputs: all
their errors are handled and reported as messages with exit codes.

Minor diagnostic issue (not a compilation bug): every `unsupported`
diagnostic ends with "is not supported in subset S1", even for standard
library features. Aether's own `help` text calls the supported subset S2,
and the subset has grown far beyond S1. The text is hard-coded in
`internal/gate/gate.go:307`. Per the task, this was not changed.

### Program and fixture issues found (by the gc reference)

Before the final run, every gc reference output was read to make sure the
programs themselves behave correctly. Two fixture mistakes were found in
the scripts of 08-interp, my own mistakes in writing the test inputs:
- `basics.mini` used a function before declaring it, which the small
  language does not allow. The script ended early with
  `undefined: print_me`.
- `functions.mini` assigned to a map field (`ages.bob += 1`), which the
  language's parser does not accept, so the script ended with a syntax
  error.

Both scripts were corrected so the cases test what they were meant to.
Aether never built 08-interp, so neither correction affects any Aether
result. No Go source of any program was changed after evaluation began,
except a `gofmt` reformat of `02-calc/main.go` (whitespace only). The
reformat was needed because the repository's gofmt check covers untracked
files outside `testdata/`.

### Harness validation

- `harness/main_test.go` unit-tests the comparison rules: exact match, the
  panic-prefix rule, and failures on stdout, stderr, or exit differences.
  `cd harness && go test .` passes.
- A negative end-to-end run used a fake `aether` whose binaries print the
  wrong output and always exit 0. The harness reported all 11 cases of
  01-wordfreq as `FAIL`, with the exit, stdout, and stderr mismatches named
  correctly.
- Each gc binary runs twice per case (`-gcrepeat 2`). No program produced
  different output across runs, including the concurrent ones under gc's
  multi-threaded scheduler, so no nondeterministic program could have
  caused a false failure.
- The evaluation is isolated from the repository's own checks:
  - every program, the harness, the generator, and the probes are separate
    modules, and `go list ./...` from the repository root lists none of
    them
  - the repository's gofmt check is clean with the eval tree present

## Performance observations (informal)

These are single wall-clock runs from the harness, including process
start-up, on the host above. They are not benchmarks under
`docs/BENCHMARKS.md` and should not be quoted as such. They show the
magnitude of the difference only.

| Case | gc | Aether default | Aether `-O0` |
|---|---|---|---|
| wordfreq, 1.6 MB text (`01-wordfreq/large`) | 48 ms | 96 ms | 411 ms |
| sieve to 2,000,000 (`07-workload/sieve`) | 5 ms | 8 ms | 37 ms |
| binary trees, depth 15 (allocation/GC) (`07-workload/trees`) | 157 ms | 219 ms | 328 ms |
| 120×120 float64 matmul (`07-workload/matmul`) | 3 ms | 5 ms | 59 ms |
| n-body, 200,000 steps (float64) (`07-workload/nbody`) | 14 ms | 361 ms | 2142 ms |
| 300,000 string-keyed map inserts + deletes (`07-workload/maps`) | 125 ms | 223 ms | 446 ms |
| sort 500,000 ints + stable sort of 100,000 records (`07-workload/sort`) | 129 ms | 280 ms | 1303 ms |
| 1.1 MB strings.Builder, Fields, Count, ReplaceAll (`07-workload/strings`) | 11 ms | 20 ms | 131 ms |
| binary trees, depth 17 (`07-workload/scale3-trees`) | 689 ms | 1928 ms | 2438 ms |
| all seven kernels (`07-workload/all`) | 416 ms | 1140 ms | 4528 ms |

In optimized mode, Aether's binaries took 1.3 to 2.8 times as long as gc's on
most kernels. n-body was the outlier at about 25 times. It is a tight
float64 loop over struct fields through pointers, which suggests that code
generation for floating point or field access is the weak spot. This was
not profiled. `-O0` was 2 to 20 times slower than the default mode, as
expected for code that keeps every value in a stack cell.

Aether builds took 0.75 to 1.0 s per program, against about 0.17 s for gc
(with a warm build cache). Most of that time likely goes to compiling `fmt`
and its dependencies from source. This was not measured separately.

## Conclusion

On the code it accepts, Aether was fully correct: 52 of 52 comparisons were
identical, across text processing, Unicode, dynamic programming, GC-heavy
allocation, floating point, sorting, large maps, and every error path.
That includes optimized and unoptimized code. Its rejections were all
documented standard-library limits, reported promptly and precisely. It
never failed silently.

Its practicality for programs written without regard to its subset is
limited mainly by standard-library coverage. 7 of 10 such programs were
rejected. A small set of APIs would have the largest effect:
- `errors.As` alone would have unblocked 2 programs outright (03-ledger and
  06-graph).
- `atomic.Pointer`, as `internal/bisect` uses it, would unblock `time`
  and `math/rand`.
- `os.ReadFile` and `math.Exp`/`Log` follow.

Whether the larger programs' generics, interfaces, panic/recover control
flow, goroutines, and multi-package code would also compile correctly is
still **untested by this evaluation**.
