#!/usr/bin/env bash
# Diagnostic probes (not part of the pass/fail results). Each probe uses one
# standard-library package, with the APIs the v2 programs call, or one
# language construct the programs rely on. For each: gc builds and runs it
# (reference), Aether builds it (default mode); if Aether builds it, both
# binaries run and their stdout and exit status are compared.
# Usage: eval/practical-v2/probes/run.sh [path/to/aether]   (after run.sh built it)
set -uo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
aether="${1:-$here/../build/aether}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
export GOWORK=off GOTOOLCHAIN=local CGO_ENABLED=0
printf '%-26s %-6s %-12s %s\n' PROBE GC AETHER DETAIL
for d in "$here"/*/; do
	name="$(basename "$d")"
	gc=ok
	(cd "$d" && go build -o "$tmp/$name.gc" .) >/dev/null 2>&1 || gc=FAIL
	out="$(cd "$d" && "$aether" build -o "$tmp/$name.ae" main.go 2>&1)"
	code=$?
	case $code in
	0) cls=built ;; 1) cls=invalid ;; 3) cls=unsupported ;; 4) cls=INTERNAL ;; *) cls="exit-$code" ;;
	esac
	detail="$(printf '%s' "$out" | head -1)"
	if [ $code -eq 0 ] && [ $gc = ok ]; then
		(cd "$d" && timeout 60 "$tmp/$name.gc" >"$tmp/gc.out" 2>/dev/null); ge=$?
		(cd "$d" && timeout 60 "$tmp/$name.ae" >"$tmp/ae.out" 2>"$tmp/ae.err"); ae=$?
		if [ $ge -eq $ae ] && cmp -s "$tmp/gc.out" "$tmp/ae.out"; then
			cls=same
			detail=""
		else
			cls=DIFFERS
			detail="exit gc $ge aether $ae; $(head -c 200 "$tmp/ae.err" | head -1)"
		fi
	fi
	printf '%-26s %-6s %-12s %s\n' "$name" "$gc" "$cls" "$detail"
done
