# Aether practical evaluation v2: report

**Date:** 2026-11-25 (UTC). **Aether:** commit `1294f75`, compiler and
runtime unmodified (`git status` shows only `eval/practical-v2/` as new).
**Reference:** gc, `go1.26.1 linux/amd64`. **Host:** Linux 6.18 (WSL2),
x86-64, AMD Ryzen 7 7435HS (16 logical CPUs), GNU binutils 2.42.
`results/summary.md` records the environment of the run. To reproduce:
`eval/practical-v2/run.sh` (see `README.md`).

## Verdict

**0 of 10 programs built.** Aether rejected all ten, in both default and
`-O0` modes, with a correct, well-located `unsupported` diagnostic (exit
3) in 0.13–0.18 s. None of the 125 cases × 2 modes = 250 comparisons
could run. No crash, internal error (exit 4), toolchain error, hang, or
wrong output occurred, because no Aether binary was produced.

Every rejection is a limit already documented in `docs/SEMANTICS.md`. No
undocumented gap and no compiler or runtime bug was found.

The main finding is that Evaluation v1's 10/10 result does not
generalize. The v1 programs ended up inside the standard-library surface
that the post-v1 batches added. Ten new programs, written in ordinary
modern Go, all hit gaps outside that surface:
- **`flag` blocks all 10 programs** by itself. Its first blocker is that
  `flag` needs `reflect.New`. Every program has a command-line interface,
  and all ten parse it with `flag`.
- **Reflection-based encoders block 6 of 10:** `encoding/json` (4:
  01, 03, 05, 10), `encoding/gob` (08), and `encoding/xml` (07). All three stop at an undeclared `reflect`
  function (`reflect.TypeFor`).
- **Two Go 1.22/1.23 language features block 4 programs.** Unlike in v1,
  some blockers here are in the language, not the library:
  - range over functions, directly or through `maps.Keys` /
    `slices.Sorted`: 05, 07, 08
  - a closure capturing a 3-clause `for` loop variable under per-iteration
    semantics: 07, 10
  - a struct embedding an interface: 10
- **Networking, crypto, and big numbers** are absent:
  - `net/url`, `net`, and `net/http` stop at package `weak` or at a
    `//go:linkname` in FIPS `crypto`
  - `crypto/sha256` stops at the same FIPS `//go:linkname`
  - `math/big` stops at its assembly
  - `context` and `log` are unsupported

The 60 one-feature probes (`probes/`) show the other side. Aether built
**40 probes, and all 40 ran identically to gc**. They cover `bufio`,
`bytes`, `regexp`, `time` (parse, format, durations, timers),
`compress/gzip`, `container/heap`, `encoding/binary`, `hash/*`,
`math/rand/v2`, `sync`, `sync/atomic`, `slices`, `sort`, `strconv`,
`text/tabwriter`, `unicode`, `errors.Is/As/Join`, generic types with
methods, method expressions, panic/recover control flow, `goto`, 200,000-
deep recursion, struct map keys, and `select` with timers. So what Aether
accepts still appears correct. What it accepts is too narrow for these
programs.

## The programs

Written as ordinary, idiomatic Go 1.26 without consulting Aether's subset.
Ranked by complexity. "Code lines" excludes blank and comment-only lines.

| # | Program | Code lines | Files / pkgs | Cases | What it does |
|---|---|---|---|---|---|
| 01 | logstat | 402 | 1 / 1 | 12 | access-log analyzer: regexp parsing, RFC 3339 timestamps, latency percentiles, endpoint normalization, time buckets, text (tabwriter) or JSON report |
| 07 | crawler | 628 | 3 / 1 | 13 | concurrent crawler over a simulated web: level-synchronous worker pools, robots.txt, retries with backoff, redirects, per-request timeouts, XML sitemap, seeded synthetic sites of up to 20,000 pages |
| 06 | compress | 646 | 3 / 1 | 17 | own LZ77 (hash chains) + canonical length-limited Huffman format with CRC-32; corruption and truncation detection; benchmark against gzip with SHA-256 |
| 10 | httpapi | 687 | 2 / 1 | 6 | JSON REST API: `ServeMux` path patterns, auth/logging/recovery middleware, validation, ETags, optimistic concurrency, pagination; replayed via `httptest` and served on loopback to 8–32 concurrent clients, then shut down gracefully |
| 05 | deps | 847 | 3 / 1 | 18 | dependency resolver: full semver ranges (caret, tilde, x-ranges, hyphen ranges, `\|\|`, prereleases), fail-first backtracking, lockfile, Kahn install order, cycle reporting, `why`, `outdated` |
| 03 | tmpl | 952 | 3 / 1 | 17 | template engine over JSON: lexer with whitespace control, nested blocks, `each` over lists and maps, scoped variables, partials with recursion limit, 15 filters, positioned errors |
| 02 | markdown | 956 | 3 / 1 | 8 | Markdown to HTML: ATX/setext headings, fenced/indented code, nested quotes and lists, task lists, GFM tables, reference links, emphasis, autolinks, URL sanitizing, TOC |
| 04 | regex | 1148 | 3 / 1 | 15 | own regex engine (parser, compiler, Pike VM, leftmost-first submatches, Unicode case folding, POSIX classes, flags) and grep tool, cross-checked against `regexp` on 49 patterns |
| 09 | scheme | 1767 | 6 / 1 | 12 | Scheme interpreter: fixnum/bignum/rational/float tower, tail calls, named let, `do`, quasiquote, `call/ec` and error handlers via panic/recover, `dynamic-wind`, hash tables, prelude in Scheme |
| 08 | minidb | 2468 | 5 / 3 | 7 | SQL database: generic B-tree with deletion, unique/secondary indexes, cost-ranked index planner, expressions with 3-valued logic, GROUP BY/HAVING/ORDER/LIMIT, statement atomicity, nested transactions with undo log, gob checkpoint and reload |

Total: 10,501 code lines, 32 files, 125 cases. Inputs include:
- **Normal inputs:** realistic logs, documents, templates, registries,
  SQL, and Scheme programs.
- **Edge cases:**
  - CRLF, invalid UTF-8, and empty input
  - int64 overflow into bignums, and SQL integer wraparound
  - regex empty matches, prerelease ordering, and redirect loops
  - flaky and slow pages, and 304/412 responses
- **Invalid input:** malformed files, syntax errors, unknown flags and
  commands, missing files, corrupt and truncated archives, constraint
  violations, and arity errors.
- **Large workloads:**
  - a 5.8 MB access log and a 2 MB grep corpus
  - 1 MB compression inputs and an 815 KB Markdown book
  - 4,000-product templates and a 20,000-row SQL workload
  - 20,000-page crawls
  - 13.9 million interpreter steps
  - 32 concurrent HTTP clients

Standard-library packages used (44 distinct): `bufio bytes cmp
compress/gzip container/heap context crypto/sha256 encoding/binary
encoding/gob encoding/hex encoding/json encoding/xml errors flag fmt
hash/crc32 hash/fnv html io io/fs log maps math math/big math/bits
math/rand/v2 net net/http net/http/httptest net/url os path/filepath
regexp runtime/debug slices sort strconv strings sync sync/atomic
text/tabwriter time unicode unicode/utf8`. The `README.md` table lists
each program's imports.

## Ground truth and freeze

- Expected behavior is the gc binary's. Before freezing, every case ran
  three times with gc: twice inside the harness, plus a third time through
  a stand-in "compiler" that also called `go build`. All 125 cases
  produced identical output every time, including the concurrent crawler
  and HTTP self-test. Every gc output was read to confirm the programs
  behave as intended.
- Mistakes found and fixed before the freeze (none after):
  - a parse-order bug in `03-tmpl` (`.` was rejected as a path)
  - a `|` separator clash in `05-deps` semver test mode
  - a map-iteration-order dependency in `10-httpapi` query validation
  - a synthetic site in `07-crawler` whose robots.txt blocked its only
    entry link
  - an invalid date and an `If-None-Match` value in fixtures
  - non-UTF-8 capitalization in the fixture generator
  - a Scheme workload too slow for the timeout (6.7 s with gc), which was
    reduced
  - `gofmt` whitespace
- Frozen at 2026-11-25T22:07:55Z, before Aether compiled anything:
  131 files (sources, `go.mod`s, `cases.txt`, fixtures, generator,
  harness, `run.sh`), listed in `FREEZE.sha256`, whose own SHA-256 is
  `41a202aeacb9a868c9c1fcc6f3e1d2a247bf813dcd4db0edb6fb1be000ee1309`.
  `run.sh` checks the manifest before every run, and the files were
  intact after the run.
- `gen/` regenerates every `inputs/gen-*` file byte for byte (verified
  twice).

Per-program digests: SHA-256, first 16 hex digits, of that program's
`FREEZE.sha256` lines. Reproduce with
`grep ' programs/NN-name/' FREEZE.sha256 | grep -v /inputs/ | sha256sum`
(sources), or with `grep ' programs/NN-name/inputs/'` (fixtures).

| Program | Sources digest | Fixtures digest | Fixture files | Fixture bytes |
|---|---|---|---|---|
| 01-logstat | `d87fae47da49a219` | `5e5cbe29b7749b39` | 4 | 5,818,985 |
| 02-markdown | `e1453c470a9fd8a8` | `7554db25b05e6b55` | 5 | 816,925 |
| 03-tmpl | `fb5fae65e3e74efc` | `03f2795832d8a4b3` | 18 | 637,407 |
| 04-regex | `6f562f517c3b221b` | `b0545e7186fdc2ba` | 3 | 2,048,267 |
| 05-deps | `fd9fb8c9c06751ad` | `f42d552a731e3e73` | 12 | 75,924 |
| 06-compress | `fb16cbe4a0c1d276` | `454200a3a360eaa3` | 13 | 2,207,851 |
| 07-crawler | `707e617cc2cb1ccf` | `41e9e18d8f6e3fa1` | 3 | 2,614 |
| 08-minidb | `032cf179164f7d3b` | `44e057c24d0b75f7` | 5 | 1,101,805 |
| 09-scheme | `e49fcd30565ddc4a` | `e455fb6451e3c5f4` | 8 | 8,029 |
| 10-httpapi | `364d3aa625606acc` | `b6417945d4098c24` | 2 | 3,292 |

The generator, harness, and `run.sh` digests are `fd72aa984e412ee8`,
`22a1a276d6315b5d`, and `6ff249413d3ead49`.

## Compilation results

Both modes were rejected identically for every program. The table gives
Aether's first diagnostic (abridged), its exit status, and its time in
default / `-O0` mode.

| Program | gc | Aether default | Aether `-O0` | First diagnostic |
|---|---|---|---|---|
| 01-logstat | OK | unsupported (3), 172 ms | unsupported (3), 133 ms | `encoding/json/decode.go:600: package encoding/json ... (undefined: reflect.TypeFor) [stdlib-package]` |
| 02-markdown | OK | unsupported (3), 159 ms | unsupported (3), 148 ms | `weak/pointer.go:70: package weak ... (undefined: abi.Escape) [stdlib-package]` (via `net/url`) |
| 03-tmpl | OK | unsupported (3), 143 ms | unsupported (3), 139 ms | `encoding/json ... reflect.TypeFor [stdlib-package]` |
| 04-regex | OK | unsupported (3), 149 ms | unsupported (3), 148 ms | `flag/flag.go:545: package flag ... (undefined: reflect.New) [stdlib-package]` |
| 05-deps | OK | unsupported (3), 151 ms | unsupported (3), 147 ms | `encoding/json ... reflect.TypeFor [stdlib-package]` |
| 06-compress | OK | unsupported (3), 180 ms | unsupported (3), 163 ms | `crypto/internal/fips140/check/check.go:32: directive //go:linkname of variable Linkinfo [directive]` (via `crypto/sha256`) |
| 07-crawler | OK | unsupported (3), 142 ms | unsupported (3), 139 ms | `weak/pointer.go:70: package weak ... abi.Escape [stdlib-package]` (via `net/url`) |
| 08-minidb | OK | unsupported (3), 146 ms | unsupported (3), 151 ms | `encoding/gob/decode.go:1206: package encoding/gob ... reflect.TypeFor [stdlib-package]` |
| 09-scheme | OK | unsupported (3), 183 ms | unsupported (3), 178 ms | `flag/flag.go:545: package flag ... reflect.New [stdlib-package]` |
| 10-httpapi | OK | unsupported (3), 135 ms | unsupported (3), 135 ms | `encoding/json ... reflect.TypeFor [stdlib-package]` |

## Comparison results

| | Default | `-O0` |
|---|---|---|
| Cases | 125 | 125 |
| Built programs | 0 | 0 |
| PASS / FAIL / TIMEOUT | 0 / 0 / 0 | 0 / 0 / 0 |
| NOT-BUILT | 125 | 125 |
| Harness problems (nondeterministic or failing gc reference) | 0 | 0 |

## Unsupported APIs and language features

Aether reports only the first unsupported construct in a program. To list
every blocker, each standard-library package the programs import was
probed with the APIs the programs call, and so was each language
construct they rely on (`probes/`, `probes/run.sh`). Each probe is built
with gc and Aether, and when both build, both run and are compared. The
probes are diagnostics written after the run. They are not part of the
pass/fail result, and they cannot rule out further blockers behind these.

### Probe results: 60 probes, 40 built and identical to gc, 20 unsupported, 0 different

| Unsupported probe | Aether's first diagnostic (abridged) | Documented in `SEMANTICS.md` |
|---|---|---|
| `flag` | `flag`: undefined `reflect.New` | yes, "Still unsupported: … `flag` (`reflect.New`)" |
| `encoding/json`, `encoding/gob`, `encoding/xml` | undefined `reflect.TypeFor` | yes, `reflect` is supported only in part |
| `net/url`, `net` | package `weak`: undefined `abi.Escape` | yes, `stdlib-package` rule |
| `net/http`, `net/http/httptest`, `crypto/sha256` | `crypto/internal/fips140/check`: `//go:linkname` of a variable | yes, `//go:linkname` (`directive`) |
| `context` | `*context.cancelCtx` (a struct embedding an interface) | yes, "`context` (a struct embedding an interface)" |
| `log` | `runtime.CallersFrames` has no body | yes, "`log` (`runtime.Caller`)" |
| `math/big` | `addVV` has no body (assembly) | yes, "`math/big` then stops at its assembly" |
| `runtime/debug` | `internal/syscall/unix`: undefined `syscall.Stat_t` | yes, `stdlib-package` rule |
| `path/filepath` | undefined `os.Lstat` | yes, `os` lists what it lacks |
| `os.ReadDir`, `os.CreateTemp` | not in Aether's package `os` | yes, both are listed as absent |
| `maps.Keys` + `slices.Sorted` | `slices/iter.go`: `for-range` over a function | yes, "range over functions" and iterators are unsupported |
| range over a user-defined iterator | `for-range statement [for-range]` | yes |
| closure capturing a 3-clause `for` variable | `loop-variable-address` | yes |
| struct embedding an interface | `type struct{Writer; n int} [non-int-type]` | yes, "a struct embedding an interface" |

Supported and identical to gc (40):
- **Packages:** `bufio`, `bytes`, `cmp`, `compress/gzip`,
  `container/heap`, `encoding/binary`, `encoding/hex`, `errors` (`Is`,
  `As`, `Join`, `Unwrap`, `*os.PathError`), `fmt` (`%T`, `%+v`, widths),
  `hash/crc32`, `hash/fnv`, `html`, `io`, `math`, `math/bits`,
  `math/rand/v2` (PCG), `regexp`, `slices` (without iterators), `sort`,
  `strconv`, `strings`, `sync`, `sync/atomic`, `text/tabwriter`, `time`
  (`Parse`, `ParseDuration`, `Truncate`, `Format`, timers), `unicode`,
  `unicode/utf8`
- **Language constructs:**
  - generic types with methods
  - method expressions
  - panic/recover as control flow with re-panic
  - `goto` to a label at the end of a loop body
  - 200,000-deep recursion
  - struct map keys
  - range-variable capture and `range` over an int
  - recursive closures
  - `init` with a closure table
  - indexing an array literal
  - anonymous-struct slices
  - `select` with a timer
  - type switches

### Blockers by program

From the probes, matched against each program's imports and constructs:

| Program | Blockers |
|---|---|
| 01-logstat | `flag`, `encoding/json` |
| 02-markdown | `flag`, `net/url` |
| 03-tmpl | `flag`, `encoding/json`, `os.ReadDir`, `path/filepath` |
| 04-regex | `flag` only |
| 05-deps | `flag`, `encoding/json`, `maps.Keys`/`slices.Sorted` (iterators) |
| 06-compress | `flag`, `crypto/sha256` |
| 07-crawler | `flag`, `context`, `encoding/xml`, `net/url`, iterators, loop-variable capture |
| 08-minidb | `flag`, `encoding/gob`, `os.CreateTemp`, iterators (own `BTree.All` and `maps.Keys`) |
| 09-scheme | `flag`, `math/big`, `runtime/debug` |
| 10-httpapi | `flag`, `encoding/json`, `net`, `net/http`, `net/http/httptest`, `context`, `log`, interface embedding, loop-variable capture |

How many programs each gap blocks:

| Gap | Programs blocked | Programs for which it is the only blocker |
|---|---|---|
| `flag` (`reflect.New`) | 10 | 1 (04-regex) |
| `reflect.TypeFor`-based encoders (`json`, `gob`, `xml`) | 6 | 0 |
| range over functions / iterator helpers | 3 | 0 |
| `net/url` / `net` / `net/http` (`weak`, FIPS `linkname`) | 3 | 0 |
| `context` | 2 | 0 |
| loop-variable capture in 3-clause `for` | 2 | 0 |
| `os.ReadDir` / `CreateTemp` / `path/filepath` | 2 | 0 |
| `crypto/sha256` | 1 | 0 |
| `math/big`, `runtime/debug` | 1 | 0 |
| `log`, interface embedding | 1 | 0 |

With `flag` supported, 04-regex would have no blocker among its library
and construct uses. With `flag` and the reflection encoders, 01-logstat
would also have none. Every other program needs at least two separate
gaps closed.

## Compiler and runtime bugs

None found:
- No internal compiler error.
- No toolchain error.
- No probe whose Aether binary differed from gc's.
- No crash or hang.

This is weak evidence. No evaluation program ran under Aether, so their
code generation, garbage collection under load, goroutine scheduling, and
deep recursion are **untested by this evaluation**. Only the 40 small
probes executed.

Diagnostic quality: precise positions, immediate rejection, exit 3. The
v1 observation still holds: every message says "is not supported in
subset S1". Also, the reported location is often deep in GOROOT (e.g.
`weak/pointer.go`, `fips140/check/check.go`) rather than at the user's
import. That makes the user-visible cause (`net/url`, `crypto/sha256`)
hard to identify without probes.

## Performance

Not measurable: no Aether binary was produced. For reference, the gc
binaries' single-run wall times for the heaviest cases (from
`results/results.tsv`, including process start; not a benchmark):
- `07-crawler/synthetic-big`: 0.94 s
- the `09-scheme` `control`/`queens` scripts: 1.1 s each
- `06-compress/large-bench`: 0.28 s
- `04-regex/large-emails`: 0.23 s
- `08-minidb/large`: 0.24 s
- `01-logstat/large`: 0.19 s

All other cases took under 0.15 s. Aether's rejections took 0.13–0.18 s.

## Comparison with Evaluation v1

| | v1, first run (commit `d1ec4d9`) | v1, latest run (`07c9503` plus the uncommitted Batch 4 work that became `1294f75`) | v2 (`1294f75`) |
|---|---|---|---|
| Programs built | 3 / 10 | 10 / 10 | **0 / 10** |
| Identical comparisons | 52 / 52 | 212 / 212 | 0 / 0 (250 not run) |
| Miscompilations / runtime bugs | 0 | 0 | 0 observed (nothing ran) |
| Kind of blocker | standard library only | none | standard library (mostly reflection), plus 3 language features |

The batches that lifted v1 from 3/10 to 10/10 added these, among others:
- `errors.As`
- `atomic.Pointer`, which unblocked `time` and `math/rand`
- `os` files
- `math.Exp`/`Log`
- `encoding/binary` and `hash/crc32`

They closed exactly the gaps that v1's programs hit, and v1 was then
re-run on the same programs. v2 shows those gains are real but narrow:
- `time`, `math/rand/v2`, `errors.As`, `encoding/binary`, `hash/crc32`,
  `compress/gzip`, and `regexp` all now work in v2's probes.
- An independent set of ten programs hit a different, wider frontier:
  `flag`, the `reflect`-based encoders, networking, iterators, and
  per-iteration loop variables.
- None of v1's programs imports `flag` or `encoding/json` (checked with
  `grep`), which is why neither showed up there.

## Conclusion

Aether did not generalize to this new set of ordinary Go programs: 0 of
10 built, in either mode. All rejections were correct, immediate, and
documented. The evidence on correctness remains that the code Aether
accepts runs like gc's (40/40 probes here, 212/212 in v1). But this set
shows that Aether currently covers too little of idiomatic Go to compile
typical programs unmodified.

The most valuable next steps, in order of how many v2 programs each
unblocks:
1. `flag`, through `reflect.New`. It is in all 10 programs.
2. The `reflect` surface used by `encoding/json` (and `gob`/`xml`), which
   blocks 6.
3. Range over functions, with the `maps`/`slices` iterator helpers.
4. Per-iteration capture of 3-clause loop variables.
5. `net/url` (package `weak`) and `context`.

These are recommendations only. Per the evaluation rules, no compiler or
runtime change was made.
