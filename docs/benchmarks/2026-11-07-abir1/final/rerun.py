# Independent rerun of (workload, offset) pairs from the executables that
# TestABITime built in OUT/layout: the given variants round-robin, one discarded
# warm-up, then N runs each, CPU time (user + system) from rusage, stderr
# checked against the recorded gc output. Pinned to CPU 6.
#
#   python3 rerun.py OUT cmd/aether/testdata/bench N RAW.tsv isel,abir1 w:off ...
#
# Writes every run to RAW.tsv and prints the medians.
import os, subprocess, statistics as st, sys

out, bench, n, raw, variants = sys.argv[1], sys.argv[2], int(sys.argv[3]), sys.argv[4], sys.argv[5].split(",")
pairs = [p.split(":") for p in sys.argv[6:]]
os.sched_setaffinity(0, {6})


def run(exe, want):
    p = subprocess.Popen([exe], stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    err = p.stderr.read()
    _, status, ru = os.wait4(p.pid, 0)
    if status != 0 or err != want:
        sys.exit(f"{exe}: exit status {status} or stderr differs from gc's")
    return round((ru.ru_utime + ru.ru_stime) * 1e9)


with open(raw, "w") as f:
    f.write("workload\tvariant\toffset\trun\tcpu ns\n")
    for w, off in pairs:
        want = open(f"{bench}/{w}.gcout", "rb").read()
        times = {v: [] for v in variants}
        for r in range(-1, n):
            for v in variants:
                t = run(f"{out}/layout/{w}.{v}.{off}", want)
                if r >= 0:
                    times[v].append(t)
                    f.write(f"{w}\t{v}\t{off}\t{r}\t{t}\n")
        m = {v: st.median(times[v]) / 1e6 for v in variants}
        cols = "\t".join(f"{v} {m[v]:.1f}" for v in variants)
        print(f"{w}\t{off}\t{cols}\t{variants[1]}/{variants[0]} {m[variants[1]] / m[variants[0]]:.3f}", flush=True)
