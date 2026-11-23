#!/usr/bin/env bash
# Classifies one-feature probe programs with `aether check`, to list which
# standard-library features the evaluation programs depend on are supported.
# Every probe is first type-checked by gc (go vet) to show it is valid Go.
# Usage: eval/practical/probes/run.sh [path/to/aether]
set -uo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
aether="${1:-$here/../build/aether}"
export GOWORK=off GOTOOLCHAIN=local
printf '%-22s %-6s %-12s %s\n' PROBE GC AETHER DIAGNOSTIC
for d in "$here"/*/; do
	name="$(basename "$d")"
	gc=ok
	(cd "$d" && go vet . >/dev/null 2>&1) || gc=FAIL
	out="$(cd "$d" && "$aether" check main.go 2>&1)"
	code=$?
	case $code in
	0) cls=supported ;; 1) cls=invalid ;; 3) cls=unsupported ;; 4) cls=INTERNAL ;; *) cls="exit-$code" ;;
	esac
	printf '%-22s %-6s %-12s %s\n' "$name" "$gc" "$cls" "$(printf '%s' "$out" | head -1)"
done
