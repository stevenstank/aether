#!/usr/bin/env bash
# Builds Aether from this checkout and the comparison harness (both with the
# gc toolchain), then compiles every evaluation program with gc and Aether,
# runs both on identical inputs, and compares stdout, stderr, and exit status.
#
# Usage: eval/practical/run.sh [harness flags]
#   e.g. run.sh -only 01,07 -modes default -timeout 2m
# Results go to eval/practical/results/ (replaced on every run).
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
mkdir -p "$here/build"

export GOWORK=off GOTOOLCHAIN=local
echo "building aether from $repo"
(cd "$repo" && go build -o "$here/build/aether" ./cmd/aether)
echo "building harness"
(cd "$here/harness" && go build -o "$here/build/harness" .)

cd "$here"
exec "$here/build/harness" -aether "$here/build/aether" -root "$here" -out "$here/results" "$@"
