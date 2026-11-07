# Aether Measurement Strategy

Status: **harness and four measurements.** The workloads and the harness
are checked in ([The harness](#the-harness)). Four measurements exist, all
under WSL2: the register-allocation baseline of 2026-11-05
([`benchmarks/2026-11-05-regalloc/REPORT.md`](benchmarks/2026-11-05-regalloc/REPORT.md)),
the RA-0 results of 2026-11-06
([`benchmarks/2026-11-06-ra0/REPORT.md`](benchmarks/2026-11-06-ra0/REPORT.md)),
instruction selection, also of 2026-11-06
([`benchmarks/2026-11-06-isel/REPORT.md`](benchmarks/2026-11-06-isel/REPORT.md)),
and the calling-convention investigation, also of 2026-11-06
([`benchmarks/2026-11-06-abi/REPORT.md`](benchmarks/2026-11-06-abi/REPORT.md)).
This document itself contains no numbers. Aether's generated code is much
slower than gc's and is expected to stay so for a long time.

Benchmarking starts in Phase 5 (`ROADMAP.md`). Its purpose is to decide which
optimizations are worth implementing and to detect regressions. It is not for
marketing comparisons.

## Metric categories

Each category is reported separately. They are never combined into a single
score.

| Category | What is measured | How (candidate) |
|---|---|---|
| Compilation latency | Wall time of `aether build`, broken down per stage (parse, type-check, gate, lower, passes, codegen, `as`, `ld`) | Internal stage timers behind a flag; external wall time over repeated runs |
| Compiler memory | Peak RSS of the compiler process; Go heap allocations per stage | GNU `/usr/bin/time -v` (available locally, verified); `runtime.MemStats` / `testing.B` alloc reporting |
| Executable size | Total file size; size of `.text`, `.data`, `.rodata` | `readelf -S`, `size` |
| Runtime performance | Wall time and CPU time of generated executables on fixed workloads | Repeated runs. `perf stat` and `hyperfine` are **not installed** in this environment. Adopting either is a tooling decision for later. |
| Instruction counts | Static: instructions per function from emitted `.s` / `objdump`. Dynamic: executed instructions and memory operands of the `main.*` functions. | Static counts are always available. Dynamic counts come from instrumented executables ([The harness](#the-harness)). Retired-instruction counts would need `perf` and PMU access, which may not be exposed under WSL2 (unverified). |
| Pass effectiveness | Per pass: IR instructions removed or changed, and the resulting static instruction count and runtime delta | Pass statistics emitted by the compiler under a flag |
| Scalability | Compile time and memory against source size (functions, statements, block nesting) | Generated synthetic inputs at geometric sizes; report the growth trend |
| Reproducibility | Variance across repeated runs | See methodology |

## Methodology

- **Repeated measurement:** at least 10 runs per configuration (to be tuned
  once variance is known). Report the median and the spread (for example the
  interquartile range or min/max), never a single run.
- **Statistics:** comparisons are reported with a significance assessment.
  `golang.org/x/perf/cmd/benchstat` is a candidate. Adopting it requires a
  D-013 dependency note (it is a tool, not a module dependency of the compiler).
- **Environment recorded with every result:**
  - CPU model
  - kernel (currently WSL2 6.6.87)
  - OS
  - Aether commit
  - Go version used to build Aether
  - binutils version
  - power/frequency settings, if known
  - virtualization (WSL2 adds noise; results from it are labeled as such)
- **Warm vs. cold:** state whether file-system caches were warm. Default to
  warm, with a discarded warm-up run.
- **Workloads:** checked into the repository and versioned. Never change a
  workload without renaming it.
- **Regressions:** a change that worsens a tracked metric beyond the measured
  noise band must be explained in the change description. If the cause is not
  established, it is recorded as an unresolved regression, with measurements and
  hypotheses kept apart.

## The harness

The harness is test code in `cmd/aether` (`bench_*_test.go`). It uses the
compiler's own stages (`compile`, `emitProgram`, `assemble`), so its `isel`
variant is exactly what `aether build` produces, and it also builds the earlier
slot-based and SSA variants. It does not change the compiler.

**Workloads.** `cmd/aether/testdata/bench/*.go`, S2 programs that `aether
check` accepts. Each has `name.gcout`, the stderr of its gc-built binary.
Every measured executable must exit 0 with exactly that stderr.

**Variants.** `abir1` (what `aether build` produces since D-031: `isel` with the
calling convention ABI-R1), `isel` (what it produced from D-029 to D-031: promoted and
emitted with registers and instruction selection), `ra` (the same without
selection, what it produced under D-027), `slot` (what `aether build -O0` produces: every value
in a cell), `ssa` (promoted, with every value in a cell), `gc` (`go build`), and
`gcN` (`go build -gcflags=all='-N -l'`). The gc variants are reference points only
([Comparing against gc](#comparing-against-gc)).

**Dynamic counts without hardware counters.** `bench_profile_test.go`
instruments a copy of each executable's assembly:
- a counter at the start of every straight-line region (after every label
  and every conditional jump), where the flags are dead;
- a probe of `%rsp` in every function's entry block, which gives the peak
  stack;
- a wrapper around `main.main` that writes the peak stack and the counters
  to stdout.

Region count × static region contents gives exact dynamic counts of machine
instructions and memory operands, by operand class and by the IR op that
emitted them. The runtime's helpers are not counted.

**Register pressure** (`bench_pressure_test.go`) is liveness of SSA values,
static and weighted by the measured block frequencies. The pressure at an
instruction counts what is live before it plus its result, so it is an upper
bound, by at most one, on the registers that instruction needs. Constants
are excluded where a column says so.

**The model** (`writeModel` in `bench_test.go`) estimates the instructions a
register allocator would leave. It is an estimate, never reported as a
measurement.

**The calling-convention model** (`bench_abi_test.go`) rewrites the assembly of
`aether build` into candidate conventions, so that they can be counted and timed
without changing the compiler. It is a model, not an implementation, and nothing
in it reaches a built executable. The one exception is `abir1`, which is not
rewritten: it is ABI-R1 as the compiler emits it, the default build since D-031. `AETHER_ABI_VARIANTS` (comma-separated) limits `TestABITime` to the
variants named. Besides timing at the shipped layout,
`AETHER_ABI_LAYOUT` times every variant at eight code offsets modulo 64, because
placement alone can move a result. `TestABIModel` and `TestParallelCopy` run in
the normal suite; the rest skip unless `AETHER_BENCH_OUT` is set.

**Tests.** The normal suite tests the harness, not the workloads' speed:
operand parsing and classification, the instrumentation (it only inserts
lines; instrumented and plain executables of a sample of the S2 suite give
the same stderr and exit status; call, block, fall-through, and peak-stack
counts of small programs match counts known from their source), output
checking, the pressure analysis on hand-checked functions, and the timing
statistics against Python's `statistics` module. The benchmark tests skip
unless `AETHER_BENCH_OUT` is set.

**Commands.** From the repository root, with `OUT` an empty directory:

```
# gc variants, checked against the recorded outputs
AETHER_BENCH_OUT=$OUT go test -count=1 ./cmd/aether -run 'TestBenchOracle$' -v
# slot, SSA, RA-0, and isel builds, checked and profiled; static and dynamic
# counts, pressure, and the model: summary, functions, pressure, ops, model .tsv
AETHER_BENCH_OUT=$OUT go test -count=1 -timeout 30m ./cmd/aether -run 'TestBenchMeasure$' -v
# static pressure of the S2 suite: suite_pressure.tsv
AETHER_BENCH_OUT=$OUT go test -count=1 ./cmd/aether -run 'TestBenchSuitePressure$'
# timing, after the two above: times.tsv and times_summary.tsv
AETHER_BENCH_OUT=$OUT AETHER_BENCH_RUNS=15 taskset -c 6 \
  go test -count=1 -timeout 30m ./cmd/aether -run 'TestBenchTime$' -v
```

The calling-convention model, in the same `OUT`, after the gc variants:

```
# abi_summary, abi_attr, abi_attr_mem, abi_arity .tsv, and abi_suite.txt
AETHER_BENCH_OUT=$OUT go test -count=1 -timeout 30m ./cmd/aether \
  -run '^(TestABICounts|TestABIAttribution|TestABIArity|TestABISuite)$' -v
# abi_times.tsv and abi_times_summary.tsv, at the shipped layout
AETHER_BENCH_OUT=$OUT AETHER_BENCH_RUNS=15 taskset -c 6 \
  go test -count=1 -timeout 60m ./cmd/aether -run '^TestABITime$' -v
# abi_layout.tsv and abi_layout_summary.tsv: per-offset medians and their median
AETHER_ABI_LAYOUT=1 AETHER_BENCH_OUT=$OUT AETHER_BENCH_RUNS=7 taskset -c 6 \
  go test -count=1 -timeout 60m ./cmd/aether -run '^TestABITime$' -v
```

Each writes `environment.txt` (commit, toolchains, CPU, CPU affinity,
kernel) next to its results. `TestBenchTime` runs the variants round-robin,
one discarded warm-up run each and then `AETHER_BENCH_RUNS` runs (default
15), and records CPU time (user + system, from `rusage`) and wall time. Pin
it to one CPU (`taskset`) on an otherwise idle machine. The deterministic
outputs (everything except the times) are byte-identical across runs at the
same commit.

## Comparing against gc

Comparisons with the official compiler are allowed only when:

1. Both toolchain versions are recorded exactly (gc `go version`, Aether
   commit).
2. The workloads are identical where practical. Each program is valid for both
   and inside Aether's supported subset.
3. Optimization settings are equivalent and stated. Example: gc default vs.
   Aether's highest level, or gc with `-gcflags=-N -l` vs. Aether `-O0`. The
   mismatch in capability is called out.
4. Measurements are repeated as above.
5. Limitations are reported next to the numbers. These include:
   - gc binaries include a full runtime, GC, and scheduler (a 1.8 MB binary for
     a one-line `println` program in E1)
   - Aether's do not
   - so size and startup comparisons are not like-for-like

No claim of superiority over gc is made without data meeting all five
conditions, and none is expected in early phases.

## What justifies optimization work

An optimization is implemented only when:
- a measurement shows the cost it targets (instruction count, runtime, or size)
  on a checked-in workload, and
- after implementation, the same measurement shows the effect.

Passes that show no measurable effect are candidates for removal, because each
pass adds miscompilation risk (`TESTING.md` §Optimization cross-checks).
