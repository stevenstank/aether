# Practical evaluation v2

A second, independent real-world evaluation of Aether, with ten new
programs. It asks whether Aether generalizes to software that was not
written around its known capabilities. Evaluation v1 (`../practical/`) is
untouched and is not reused here.

The findings are in [`REPORT.md`](REPORT.md).

Ground rules followed:
- The programs are ordinary Go, written without consulting Aether's
  supported subset and without avoiding language or library features.
- The expected behavior is whatever the gc-built binary does. Every gc
  reference output was reviewed, and every case was run three times with
  gc to confirm the programs are deterministic.
- All sources, fixtures, and the harness were frozen (`FREEZE.sha256`)
  before Aether compiled anything. `run.sh` refuses to run if any frozen
  file has changed.
- Neither the compiler nor the runtime was modified during the evaluation.
- Every program, the harness, and the generator are separate modules, so
  none of them is part of the repository's `go build`/`vet`/`test ./...`.

## Programs

| # | Program | What it is | Main language and library features |
|---|---|---|---|
| 01 | logstat | HTTP access-log analyzer: percentiles, top endpoints, time buckets, JSON or text report | `regexp`, `time.Parse`/`ParseDuration`/`Truncate`, `encoding/json` (struct tags, maps, `time.Time`), `text/tabwriter`, `errors.As` on `*os.PathError` |
| 02 | markdown | Markdown to HTML renderer: CommonMark blocks, GFM tables, task lists, reference links, TOC | interfaces with type switches, recursive block parsing, `regexp`, `html`, `net/url`, `%T` |
| 03 | tmpl | Handlebars-like template engine over JSON data, with filters, partials, scopes | `encoding/json` into `any`, closures in a filter table, `init`, wrapped errors (`%w`, `Unwrap`, `errors.Is/As`), `os.ReadDir`, `time` formatting |
| 04 | regex | Own regular-expression engine (parser + Pike VM, RE2 semantics) and grep tool; checks itself against `regexp` | Unicode classes and case folding, `unicode.SimpleFold`, sparse sets, `regexp` as an oracle, `strconv.Unquote` |
| 05 | deps | Package dependency resolver: semver ranges, backtracking search, lockfile, install order, cycle detection | generics (`Set[T]`), `cmp`, `maps`/`slices` iterators (`slices.Sorted(maps.Keys)`), `errors.Join`, `encoding/json` with `DisallowUnknownFields` |
| 06 | compress | Compression utility: own LZ77 + canonical Huffman format with CRC; compared against gzip | bit I/O, `container/heap`, `encoding/binary` uvarints, `hash/crc32`, `compress/gzip`, `crypto/sha256`, `math.Log2` |
| 07 | crawler | Concurrent web crawler over a simulated web: worker pools, robots.txt, retries, redirects, timeouts, XML sitemap | goroutines, channels, `select`, `sync.WaitGroup`/`Mutex`/`Once`, `sync/atomic` types, `context.WithTimeout`, timers, `net/url`, `encoding/xml`, `math/rand/v2` (PCG) |
| 08 | minidb | In-memory SQL database: generic B-tree indexes, query planner, GROUP BY/HAVING, nested transactions, gob checkpoints | multiple packages, generic B-tree with deletion, range-over-func iterators, `encoding/gob`, `os.CreateTemp`, recursive-descent SQL parser |
| 09 | scheme | Scheme interpreter: exact numeric tower, proper tail calls, escape continuations, error handlers, hash tables | `math/big` (Int, Rat), `math/bits` overflow checks, panic/recover for control flow, deep recursion, `runtime/debug.SetMaxStack` |
| 10 | httpapi | JSON REST API: routing with path patterns, auth/logging/recovery middleware, ETags, pagination; replayed in-process and served over loopback to concurrent clients | `net/http` (`ServeMux` patterns, `PathValue`, `Server.Shutdown`), `net/http/httptest`, `net`, `http.Client`, `log`, `hash/fnv` |

Each program directory contains `go.mod`, its sources, `cases.txt`, and
`inputs/`.

## Layout

```
eval/practical-v2/
  run.sh            verify freeze, build Aether + harness, run everything
  FREEZE.sha256     checksums of every frozen file (programs, gen, harness, run.sh)
  harness/          the comparison harness (copied from v1, own module)
  gen/              deterministic generator of the inputs/gen-* fixtures (own module)
  probes/           one-feature diagnostic probes, written after the run (own module; run.sh)
  programs/NN-name/ one program per directory, its own module
    cases.txt       name | stdin file in inputs/ (or -) | arguments
    inputs/         fixtures
  results/          written by run.sh (git-ignored)
  build/            aether and harness binaries (git-ignored)
  REPORT.md         findings
```

`06-compress/inputs/*.lzh` were produced by the gc build of 06-compress
(the same frozen source) and then frozen: `mixed.lzh` and `gen-text.lzh`
are its own output, and `truncated`, `corrupt`, `badmagic`, and
`trailing` are damaged copies of `mixed.lzh`.

## Reproducing

From the repository root, on Linux x86-64 with Go 1.26 and GNU binutils:

```sh
eval/practical-v2/run.sh                          # all programs, Aether default and -O0
eval/practical-v2/run.sh -modes default -only 04  # one program, one mode
```

The harness is the v1 harness: it builds each program with
`go build` (gc), and with `aether build [-O0] -o BIN *.go` in the program
directory; runs every case with a minimal environment (`PATH`,
`LC_ALL=C`) and a 5-minute timeout; runs the gc binary twice to detect
nondeterminism; and compares exit status and stdout and stderr byte for
byte. When gc's stderr contains a `panic: ` line, only the text through
that line is compared (`docs/SEMANTICS.md` V-3/V-10). Results are in
`results/summary.md`, `results/results.tsv`, `results/builds.tsv`, and
`results/NN-name/` (per-case outputs, build logs, diffs).

To regenerate the large fixtures byte for byte: `cd gen && go run .`

Diagnostic probes (after `run.sh` has built `build/aether`):
`eval/practical-v2/probes/run.sh`. Each probe is built with gc and Aether,
and when both build, both run and are compared.
