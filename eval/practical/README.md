# Practical evaluation of Aether

This directory tests whether Aether can compile and correctly run ordinary,
naturally written Go programs rather than compiler tests. It contains ten
programs of increasing complexity, their inputs, and a harness. The harness
builds each program with the official Go toolchain (gc, the reference) and
with Aether, runs both binaries on identical inputs, and compares their
standard output, standard error, and exit status.

The findings are in [`REPORT.md`](REPORT.md).

Ground rules:
- The programs were written as ordinary Go, not around Aether's documented
  subset, and were not changed to make Aether accept them.
- Neither the compiler nor the runtime was modified.
- Each program has its own `go.mod`. This keeps it out of the repository's
  `go build ./...`, `go vet ./...`, and `go test ./...`.

## Layout

```
eval/practical/
  run.sh              build Aether + harness, run everything
  harness/            the comparison harness (Go, built with gc; own module)
  gen/                deterministic generator of the large fixtures (own module)
  probes/             one-feature programs classified with `aether check` (own module)
  programs/NN-name/   one program per directory, its own module
    go.mod
    *.go              package main (10-kvstore also has store/, wal/, proto/)
    cases.txt         test cases: name | stdin file in inputs/ (or -) | arguments
    inputs/           fixtures (gen-* files are generated)
  results/            written by run.sh (git-ignored)
  build/              aether and harness binaries (git-ignored)
  REPORT.md           findings
```

| # | Program | What it is | Main features exercised |
|---|---|---|---|
| 01 | wordfreq | word-frequency counter | bufio.Scanner, maps, sort.Slice, unicode, fmt widths |
| 02 | calc | line calculator with a recursive-descent parser | tokenizer, custom error types, errors.Is/As, math |
| 03 | ledger | CSV bank ledger with validation and overdraft checks | encoding/csv, fixed-point money, sort.SliceStable |
| 04 | textkit | Unicode text toolkit (Levenshtein, LCS, KMP, RLE, wrap) | []rune, utf8, strings.Map, DP tables, closures with defer |
| 05 | collections | generic AVL tree, heap, LRU, set; stress-checked against maps | generics, container/list, math/rand, recover |
| 06 | graph | multi-file graph toolkit: Dijkstra, BFS, topo sort, Tarjan SCC, Kruskal | container/heap, recursion, several files |
| 07 | workload | compute/allocation kernels: sieve, binary trees, matmul, n-body, maps, sort | GC pressure, float64, large slices and maps |
| 08 | interp | tree-walking interpreter for a small scripting language | lexer/parser/AST, interfaces, closures, panic/recover control flow |
| 09 | pipeline | concurrent log analyzer (reader, worker pool, aggregator) | goroutines, channels, select, WaitGroup, Mutex, Once, atomics, time |
| 10 | kvstore | multi-package transactional KV store with a checksummed WAL and concurrent clients | module packages, encoding/binary, hash/crc32, bytes, goroutines |

## Requirements

- Linux x86-64 with Go 1.26 (to build Aether, the harness, and the
  reference binaries) and GNU binutils `as`/`ld` (for `aether build`).
- `bash`, and `git` (only to record the commit in the summary).

## Reproducing

From the repository root:

```sh
eval/practical/run.sh                         # everything: both Aether modes (default and -O0)
eval/practical/run.sh -modes default          # optimized mode only
eval/practical/run.sh -only 01,06 -timeout 2m # selected programs
```

`run.sh` does the following:
1. Builds `eval/practical/build/aether` from this checkout with
   `go build ./cmd/aether`.
2. Builds the harness and runs it with `-aether build/aether`.
3. For each program, runs `go build -o gc.bin .` in its directory (with
   `GOWORK=off GOTOOLCHAIN=local CGO_ENABLED=0`). For each mode, it then runs
   `aether build [-O0] -o aether-MODE.bin *.go` in the same directory.
4. Runs every case in `cases.txt` with each binary. A case runs from the
   program's directory, with stdin from `inputs/FILE` (or `/dev/null`), the
   listed arguments, a minimal environment (`PATH`, `LC_ALL=C`), and a
   5-minute timeout. The gc binary runs twice so that nondeterministic program
   output can be detected.
5. Compares exit status, stdout (byte for byte), and stderr (byte for byte).
   The one exception follows the repository's documented panic rule
   (`docs/SEMANTICS.md`, V-3/V-10): when gc's stderr has a `panic: ` or
   `fatal error: ` line, only the text up to and including that line is
   compared, because gc also prints a goroutine traceback that Aether does
   not.

Harness flags: `-only` (program prefixes), `-modes` (`default`, `O0`),
`-timeout`, `-gcrepeat`, `-out`.

### Outputs

`results/summary.md` records the environment (commit, Go, binutils, kernel,
and CPU), every build with Aether's first diagnostic, and every case. The
other outputs are:
- `results/results.tsv` and `results/builds.tsv`: the same data in
  machine-readable form
- `results/NN-name/`: each run's stdout and stderr (`CASE.gc.stdout`,
  `CASE.aether-default.stderr`, ...), the full build logs
  (`build-aether-default.log`), and for each failing case a
  `diff-MODE-CASE.txt` showing the first differing lines

Case statuses:
- `PASS`: exit status, stdout, and stderr all match
- `FAIL`: they differ; the detail column says which
- `TIMEOUT`: the Aether binary exceeded the timeout
- `NOT-BUILT`: Aether did not build the program; see the build table
- `HARNESS-*`: a problem with the gc reference, such as a nondeterministic
  program or a gc timeout

### Regenerating the large fixtures

The `inputs/gen-*` files are committed. They can be regenerated exactly,
with byte-identical output:

```sh
cd eval/practical/gen && go run .
```

The generator uses its own linear congruential generator, so its output
does not depend on `math/rand`.

### Checking a single program by hand

```sh
cd eval/practical/programs/04-textkit
go build -o /tmp/textkit.gc .
../../build/aether build -o /tmp/textkit.ae *.go
/tmp/textkit.gc < inputs/commands.txt > /tmp/gc.out;  echo "exit $?"
/tmp/textkit.ae < inputs/commands.txt > /tmp/ae.out;  echo "exit $?"
cmp /tmp/gc.out /tmp/ae.out && echo identical

cd ../06-graph
../../build/aether build -o /tmp/graph.ae *.go; echo "exit $?"   # 3: unsupported (errors.As)
```

### Feature probes

`probes/` holds one-feature programs: one for each standard-library API
that the evaluation programs use and that might not be supported.
`probes/run.sh` (run after `run.sh` has built `build/aether`) type-checks
each probe with gc and classifies it with `aether check`. Aether stops at
the first unsupported construct in a program. The probes show every blocker
of each program, not only the first. They are diagnostic aids, not part
of the pass/fail results.

```sh
eval/practical/probes/run.sh
```
