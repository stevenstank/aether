#!/usr/bin/env bash
# Practical evaluation v2. Verifies the frozen sources and fixtures against
# FREEZE.sha256, builds Aether from this checkout and the comparison harness
# (both with gc), then compiles every program with gc and Aether (default
# and -O0), runs all binaries on identical inputs, and compares stdout,
# stderr, and exit status.
#
# Usage: eval/practical-v2/run.sh [harness flags]
#   e.g. run.sh -only 01,07 -modes default -timeout 2m
# Results go to eval/practical-v2/results/ (replaced on every run).
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"

echo "verifying frozen inputs"
(cd "$here" && sha256sum --quiet -c FREEZE.sha256)

mkdir -p "$here/build"
export GOWORK=off GOTOOLCHAIN=local
echo "building aether from $repo"
(cd "$repo" && go build -o "$here/build/aether" ./cmd/aether)
echo "building harness"
(cd "$here/harness" && go build -o "$here/build/harness" .)

cd "$here"
exec "$here/build/harness" -aether "$here/build/aether" -root "$here" -out "$here/results" "$@"
