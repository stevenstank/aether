# Aether Measurement Strategy

Status: **strategy only.** No benchmarks exist and no measurements have been
taken. This document contains no baseline numbers and makes no performance
claims. Aether's generated code is expected to be much slower than gc's for a
long time. Phase 3 deliberately uses a slot-per-value backend with no
register allocation.

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
| Instruction counts | Static: instructions per function from emitted `.s` / `objdump`. Dynamic: retired instructions, where hardware counters are available. | Static counts are always available. Dynamic counts need `perf` and PMU access, which may not be exposed under WSL2 (unverified). |
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
  noise band must be explained in the change description.

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
