# ABI-R1 acceptance evidence (D-030, D-031)

Measurements of ABI-R1 against `isel` (ABI-S0 with instruction selection, the
default build of D-029), by the method of `BENCHMARKS.md`. All runs were under
WSL2 on an AMD Ryzen 7 7435HS, pinned to CPU 6, with go1.26.1 and GNU as/ld
2.42. Each directory's `environment.txt` has the details.

- `data/`: the experiment, measured before the switch. The commit was f9051eb
  with the harness change later committed as 0dcc09b. `abir1` was then behind
  the test option. `isel`, the model `r1`, and `abir1` were timed.
- `final/`: the acceptance check after the switch of D-031. The commit was
  0dcc09b with that change, where `aether build` emits exactly `abir1`
  (`TestABIR1Suite`, `TestBuildBackend`). `isel` and `abir1` were timed.

Both runs passed every criterion of D-030.

| Workload | Experiment (`data/`) | Final (`final/`) | Criterion |
|---|---|---|---|
| b01_fib | 0.881 | 0.881 | ≤ 0.900 |
| b02_primes | 0.985 | 0.991 | ≤ 1.020 |
| b03_collatz | 0.997 | 1.000 | ≤ 1.020 |
| b04_matpow | 1.001 | 1.001 | ≤ 1.020 |
| b05_hash | 0.827 | 0.816 | ≤ 0.900 |
| b06_vm | 0.916 | 0.914 | ≤ 0.950 |
| b07_gcd | 0.954 | 0.955 | ≤ 1.020 |
| b08_ackermann | 0.716 | 0.712 | ≤ 0.900 |
| b09_crc | 1.003 | 1.001 | ≤ 1.020 |

Each figure is the CPU-time ratio `abir1`/`isel`, using the median over the
eight code offsets of each offset's median of 11 runs.

## Files

- `abi_layout.tsv`: every timed run (CPU ns, user + system) by workload,
  variant, and offset.
- `abi_layout_summary.tsv`: the per-offset medians, their median, and the ratio.
- `abi_summary.tsv`: dynamic counts, peak stack, and code size for every
  variant. Its `isel` and model rows are byte-identical to
  `../2026-11-06-abi/data/abi_summary.tsv`, and `abir1`'s dynamic instructions
  and memory operations equal the `r1` row's.
- `abi_suite.txt`: each variant on the 239 programs of the S2 suite, against
  the interpreter.
- `rerun.py`, `rerun.tsv`: the independent rerun of the per-offset outliers from
  the same executables, with medians only in `data/`. `final/` also has every
  run (`rerun_raw.tsv`) and the outliers themselves (`outliers.tsv`).

An offset is an outlier when its ratio differs from the aggregate by more than
the sum of the two variants' relative interquartile ranges at that offset.
Every outlier, faster or slower, reproduced in its rerun. The cause of the
outliers is not established, since there are no hardware counters under WSL2.
They follow code placement:
- *Measured:* where `abir1` and the model have identical machine code
  (b06, b07, b09), they show the same outliers at the same offsets. Several
  outliers come from `isel`'s own layout, not from `abir1`'s.
- *Measured:* at b08's offset 24, `main.ack` starts at the same address in
  both variants, and `abir1` loses its whole gain there, in both runs.

## Reproducing

From the repository root at the commit of `final/`, with `OUT` an empty
directory:

```
AETHER_BENCH_OUT=$OUT go test -count=1 -timeout 30m ./cmd/aether \
  -run '^(TestABICounts|TestABISuite)$'
AETHER_ABI_LAYOUT=1 AETHER_ABI_VARIANTS=isel,abir1 AETHER_BENCH_OUT=$OUT \
  AETHER_BENCH_RUNS=11 taskset -c 6 \
  go test -count=1 -timeout 120m ./cmd/aether -run '^TestABITime$'
python3 docs/benchmarks/2026-11-07-abir1/final/rerun.py $OUT \
  cmd/aether/testdata/bench 11 $OUT/rerun_raw.tsv isel,abir1 WORKLOAD:OFFSET ...
```

`data/` used `AETHER_ABI_VARIANTS=isel,r1,abir1`, and its rerun script, which
times `isel`, `abir1`, and `r1`, takes no raw-output argument.
