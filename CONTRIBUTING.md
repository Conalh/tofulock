# Contributing to tofulock

Thanks for taking a look. tofulock is an early, experimental project, so issues,
bug reports, and small focused PRs are all welcome — including "the README was
confusing" or "the threat model doesn't hold here."

## Prerequisites

- **Go 1.23+**
- **git** on your `PATH` (used for `git ls-remote`)

## Build, test, run

```sh
git clone https://github.com/Conalh/tofulock
cd tofulock

go build -o tofulock .      # build the CLI
go vet ./...                # static checks
go test ./...               # unit tests (no network)

# try it end to end against the bundled example:
./tofulock lock   ./examples/basic
./tofulock verify ./examples/basic
```

Unit tests are offline by design — registry resolution is covered with an
`httptest` server and source parsing with table tests, so `go test ./...` never
hits the network. Live resolution is exercised by the `dogfood` job in CI.

## Project layout

```
internal/
  cli/        command dispatch and output rendering
  tfmod/      module-call discovery (terraform-config-inspect)
  terragrunt/ terragrunt.hcl terraform{} source discovery
  resolve/    source classification + git ref → commit
  registry/   Module Registry Protocol client
  lock/       resolution engine shared by lock and verify
  lockfile/   deterministic lockfile read/write
  attest/     in-toto statement + DSSE envelope + ed25519 signing
  util/       tiny shared helpers (e.g. query-string parsing)
```

See [THREAT_MODEL.md](THREAT_MODEL.md) for the design's trust boundaries.

## Conventions

- **Standard library first.** Dependencies are limited to the parsing/versioning
  libraries the ecosystem itself uses (`terraform-config-inspect`,
  `go-version`). Prefer not to add new ones.
- **Deterministic output.** The lockfile and attestation must be byte-stable
  across runs (sorted, timestamp-free) so diffs stay clean. Add a test if you
  touch serialization.
- **New source types** should plug into `resolve.Classify` + `lock.Module` and
  come with tests. Keep network calls out of unit tests (use `httptest` or pure
  parsing tests).
- Run `gofmt`, `go vet ./...`, and `go test ./...` before opening a PR. CI
  enforces `gofmt -l` (no diffs) and `golangci-lint` (see `.golangci.yml`),
  and runs the test suite on Ubuntu, Windows, and macOS.

## Pull requests

- Keep PRs focused; describe the behavior change and how you verified it.
- Update the README / THREAT_MODEL if you change user-facing behavior or trust
  assumptions.
- By contributing you agree your work is licensed under the project's
  [MIT License](LICENSE).

## Good first areas

- Terragrunt `terraform { source = … }` discovery.
- OCI (`oci://`) module sources.
- Content-hashing for non-git registry/archive downloads.
- HCL lockfile output alongside JSON.
