#!/bin/sh
# Usage: tools/fuzz.sh TARGET DURATION [WORKERS]
#
# Runs one fuzz target of cmd/aether (FuzzCheck, FuzzGenerated, FuzzGC) for
# DURATION (a go test -fuzztime value) with WORKERS fuzzing processes, by
# default 3/8 of the CPUs. Go's fuzzer kills a worker whose input runs over
# 10 s; each worker process keeps about 3 CPUs busy, and with half the
# CPUs or more as workers FuzzGenerated's inputs (about 2 s each alone)
# passed that limit and killed the run (TESTING.md, fuzz incident FI-2).
#
# The complete output goes to $FUZZOUT/TARGET.out (default directory:
# ${TMPDIR:-/tmp}/aether-fuzz), never truncated (FI-1). The script then
# prints the exit status, the corpus size before and after, and whether the
# run left baseline coverage and mutated inputs: a run that never does has
# not fuzzed, whatever its exit status. It exits with go test's status, or 3
# if the run did not mutate.
set -u
if [ $# -lt 2 ] || [ $# -gt 3 ]; then
	echo "usage: tools/fuzz.sh TARGET DURATION [WORKERS]" >&2
	exit 2
fi
target=$1 duration=$2
workers=${3:-$(( $(nproc) * 3 / 8 ))}
[ "$workers" -ge 1 ] || workers=1
out=${FUZZOUT:-${TMPDIR:-/tmp}/aether-fuzz}
mkdir -p "$out" || exit 2
log=$out/$target.out
cd "$(dirname "$0")/.." || exit 2
cache=$(go env GOCACHE)/fuzz/github.com/stevenstank/aether/cmd/aether/$target
count() { if [ -d "$cache" ]; then find "$cache" -type f | wc -l; else echo 0; fi; }
before=$(count)
echo "$target for $duration, $workers workers, $(nproc) CPUs; output in $log"
go test ./cmd/aether -run '^$' -fuzz "^$target\$" -fuzztime "$duration" -parallel "$workers" >"$log" 2>&1
status=$?
after=$(count)
last=$(grep -E 'execs: [0-9]+' "$log" | tail -n 1)
echo "exit status $status; cached corpus $before -> $after"
echo "last progress: ${last:-none}"
if grep -q 'gathering baseline coverage' "$log" && [ -z "$last" ]; then
	echo "NOT FUZZED: the run ended during baseline coverage; give it longer"
	[ "$status" -ne 0 ] || status=3
fi
exit "$status"
