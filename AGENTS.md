# AGENTS.md: Working Rules for Aether

Aether is an independent Go compiler written in Go, targeting Linux x86-64. Read
`docs/ROADMAP.md` to find the current phase, and `docs/DECISIONS.md` before
changing anything architectural.

## Scope

- Work only on the current roadmap phase. Do not implement later-phase features
  early.
- The supported Go subset is defined **only** by `docs/SEMANTICS.md`. Never
  describe Aether as a compatible Go compiler. Describe it as a compiler for a
  named subset.
- The official Go toolchain must **never** compile the program under test. It
  may only be used as a test oracle and to build Aether itself.
- The build path may invoke only GNU `as` and `ld` as external tools, and only
  from `internal/toolchain` (D-007, D-008). Aether owns everything up to and
  including the emitted assembly. Do not start work on a custom assembler or
  linker without a new decision.

## Repository layout

- `cmd/aether`: the CLI. `check.go` runs the stages in the D-003 order.
- `internal/diag`: diagnostics, rendering, and exit codes. No `go/*` imports.
- `internal/frontend`: a wrapper around `go/parser` and `go/types`, plus
  program-level checks.
- `internal/gate`: the S1 subset gate (default-deny).
- `internal/ir`: the IR, its printer, and its verifier. No `go/*` imports.
- `internal/lower`: typed AST to IR.
- `internal/interp`: the IR interpreter, a test oracle (D-005, D-021).
- `cmd/aether/testdata/check`: the classification corpus. Each `*.go` file has
  an `// aether:expect` header and a reviewed `*.golden` file. Each `run` file
  also has a reviewed `*.ir` file with its lowered IR.

## Validation

```
gofmt -l $(git ls-files -co --exclude-standard '*.go' | grep -v /testdata/)
go vet ./...
go test ./...
```

- The gofmt command must print nothing. Corpus files under `testdata/` are
  deliberately not gofmt-clean.
- Regenerate goldens only intentionally, with
  `go test ./cmd/aether -run 'TestCheckCorpus|TestLowerCorpus' -update`, and
  review the diff.
- When the gate changes, also fuzz:
  `go test ./cmd/aether -run '^$' -fuzz FuzzCheck -fuzztime 60s`. When
  lowering, the IR, or the interpreter changes, also fuzz `FuzzGenerated` the
  same way.
  - Save the complete output to a file and record the exit status. Never
    truncate it (`TESTING.md`, fuzz incident FI-1).
- A new unsupported feature name must be added both to the gate's registry
  (`featureNames` in `internal/gate/gate.go`) and to the `SEMANTICS.md`
  feature table. `TestFeatureTableMatchesGate` enforces this.

## Code quality

- Go 1.26, standard library only (D-013). `gofmt`, `go vet ./...`, and
  `go test ./...` must pass.
- No placeholder packages, empty files, stub implementations, or speculative
  interfaces. A package or abstraction is created only with real code and tests
  that use it (D-016).
- `internal/ir` and `internal/amd64` must not import `go/*` packages.
  Architecture-specific code lives only under `internal/<arch>`.
- Never import `go/importer` or `go/build`: they can run the `go` command
  (D-002). `os/exec` may enter the dependency graph only through
  `internal/toolchain`.
- Unsupported constructs must be rejected by the subset gate with an
  `unsupported` diagnostic. Later stages report anything unexpected as an
  `internal` error. **Never generate code you are not sure is correct.**
- Optimizations must not assume signed overflow cannot occur and must not
  reorder observable side effects.
- Output must be deterministic: no dependence on map iteration order, time, or
  absolute paths.

## Testing

- Every behavior change includes tests. Every bug fix includes a regression
  test.
- Supported features need differential tests against a gc-built binary
  (`go build`, never `go run`). See `docs/TESTING.md`.
- Do not mark a feature supported in `docs/SEMANTICS.md` without tests that
  prove it.
- Benchmarks follow `docs/BENCHMARKS.md`. Never report numbers that were not
  measured, and always record the environment.

## Dependencies

- No new module dependency without a `docs/DECISIONS.md` entry giving purpose,
  alternatives, and consequences.
- Do not install system packages or change machine or global Git configuration.

## Architecture changes

- Any change to a stage boundary, the IR contract, the runtime interface,
  diagnostic classes, or CLI exit codes needs a `docs/DECISIONS.md` entry, new
  or superseding.
- Proposed decisions must be resolved before code depends on them. Never
  describe a Proposed decision as accepted.

## Documentation

- Keep `README.md`, `docs/ARCHITECTURE.md`, `docs/SEMANTICS.md`,
  `docs/ROADMAP.md`, and `docs/DECISIONS.md` consistent. Update them in the
  same change as the code.
- Record reused components honestly. `go/parser` and `go/types` are not
  Aether's own frontend.

## Git

- **Never** commit, push, create branches, open pull requests, or rewrite
  history. The owner handles all commits.

## Reporting

- Report only what you actually did and observed. If tests were not run, say
  so. If something failed, show the output.
- Distinguish verified facts from assumptions and open questions.
- Do not claim the compiler supports anything beyond what tests demonstrate.
