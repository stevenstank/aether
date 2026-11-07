# Independent rerun of the flagged (workload, offset) pairs from the executables
# TestABITime built: isel, abir1, and r1 round-robin, one discarded warm-up,
# then N runs, CPU time from rusage, stderr checked against the gc output.
import os, sys, subprocess, statistics as st
out, bench, n = sys.argv[1], sys.argv[2], int(sys.argv[3])
pairs = [p.split(':') for p in sys.argv[4:]]
os.sched_setaffinity(0, {6})
def run(exe, want):
    p = subprocess.Popen([exe], stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    err = p.stderr.read(); _, status, ru = os.wait4(p.pid, 0)
    if status != 0 or err != want: sys.exit(f"{exe}: bad run")
    return (ru.ru_utime + ru.ru_stime) * 1000
res = {}
for w, off in pairs:
    want = open(f"{bench}/{w}.gcout", 'rb').read()
    vs = ['isel', 'abir1', 'r1']
    for r in range(-1, n):
        for v in vs:
            t = run(f"{out}/layout/{w}.{v}.{off}", want)
            if r >= 0: res.setdefault((w, off, v), []).append(t)
    m = {v: st.median(res[(w, off, v)]) for v in vs}
    print(f"{w}\t{off}\tisel {m['isel']:.1f}\tabir1 {m['abir1']:.1f}\tr1 {m['r1']:.1f}\tabir1/isel {m['abir1']/m['isel']:.3f}\tr1/isel {m['r1']/m['isel']:.3f}", flush=True)
