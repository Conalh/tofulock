# tofulock

[![CI](https://github.com/Conalh/tofulock/actions/workflows/ci.yml/badge.svg)](https://github.com/Conalh/tofulock/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
![Status: experimental](https://img.shields.io/badge/status-experimental-orange)

`tofulock` is an experimental open-source supply-chain integrity tool for
Terraform / OpenTofu **modules**. It verifies pinned module provenance, detects
drift in CI, and can emit signed in-toto / DSSE attestations as module-approval
evidence.

> **Experimental.** The CLI, lockfile format, and attestation predicate may
> change between versions. Use it, file issues — but don't depend on stability yet.

## The gap

Terraform and OpenTofu write a lock file (`.terraform.lock.hcl`), but it pins
**providers only — never modules**. Their docs are explicit: the lock file
"tracks only provider dependencies," and a remote module is re-resolved to the
newest version satisfying its constraint unless you pin an exact version. So a
`module` block constrained to `~> 5.0` — or even pinned to a git tag like
`?ref=v1.2.0` — is *not* content-pinned: a moved tag, a force-pushed branch, or
a re-published registry version can change what you actually deploy, and `init`
won't notice. Registry modules can't be pinned to a commit at all. `tofulock`
records the exact commit behind every module in a small sidecar lockfile and
fails CI when reality drifts from it.

## Install

```sh
go install github.com/Conalh/tofulock@latest   # needs Go 1.23+
# or from a clone:
go build -o tofulock .
```

`git` must be on your `PATH` — tofulock uses `git ls-remote` to resolve refs to
commits and never downloads module content during `lock`/`verify`.

## Quickstart

Try it against the bundled example (`examples/basic`):

```console
$ tofulock lock ./examples/basic
  skip    local_app              (local)
  locked  network                v4.1.2 @ 8a0b697adfbc
  locked  vpc_git                v5.8.1 @ 25322b6b6be6
  locked  vpc_registry           5.8.1 @ 25322b6b6be6
wrote .tofulock.lock.json  (3 locked, 1 skipped, 0 error)

$ tofulock verify ./examples/basic
  ok      network                v4.1.2 @ 8a0b697adfbc
  ok      vpc_git                v5.8.1 @ 25322b6b6be6
  ok      vpc_registry           5.8.1 @ 25322b6b6be6
OK: every locked module matches its recorded pin.
```

`lock` writes `.tofulock.lock.json` (commit it). `verify` re-resolves every
locked module and exits non-zero if a tag moved, a ref was re-pointed, or a
constraint now selects a different version.

```sh
tofulock list   [dir]            # show module calls and their classified source kind
tofulock lock   [dir] [--json]   # resolve git & registry modules to commits, write the lockfile
tofulock verify [dir] [--json]   # re-resolve and fail (exit 1) on any drift
tofulock attest [dir] --key K    # emit an in-toto module-provenance record (signed with --key)
tofulock verify-attest [dir] --key K.pub   # verify a signed attestation against the lockfile
tofulock keygen --out signer     # generate an ed25519 signing keypair
```

## Use in CI (GitHub Actions)

Fail the build the moment a locked module's tag is re-pointed, a branch
advances, or a registry constraint starts resolving to a new version:

```yaml
# .github/workflows/module-integrity.yml
name: module integrity
on: [pull_request]
jobs:
  verify:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: Conalh/tofulock@main
        with:
          directory: .
```

`verify` exits non-zero on drift; add `--json` (CLI) for a machine-readable
report.

## Sign & verify attestations

`tofulock` can emit a signed **module-provenance record**: an
[in-toto](https://in-toto.io) statement with one subject per pinned module
(digested by git commit), wrapped in a DSSE envelope and signed with ed25519.

```sh
tofulock keygen --out signer                         # signer.key (secret) + signer.pub
tofulock attest . --key signer.key \
  --approved-by you@example.com \
  --out tofulock.attestation.dsse.json               # signed DSSE envelope
tofulock verify-attest . --key signer.pub \
  --att tofulock.attestation.dsse.json               # verify signature + subjects vs lockfile
```

`verify-attest` confirms the signature **and** that every attested commit still
matches the current lockfile (and that the lockfile's SHA-256 is unchanged),
exiting non-zero otherwise. Omit `--key` on `attest` to print the unsigned
in-toto statement (example:
[`examples/basic/tofulock.attestation.json`](examples/basic/tofulock.attestation.json)).

The record uses the in-toto / DSSE envelope format (the same shape `cosign`
produces), so keyless Sigstore/Rekor signing is a roadmap item rather than a
rewrite. The predicate maps the evidence to SOC2 / FedRAMP / PCI-style
change-control concepts — it is an aid to producing change-control evidence, not
a compliance certification.

## What tofulock does *not* protect against

tofulock proves the module you deploy is the **same** one you pinned. It does
**not**:

- **Vet module contents.** A pinned commit can still contain malicious or
  vulnerable code — tofulock proves it hasn't *changed*, not that it's *safe*.
- **Audit the first pin (trust-on-first-use).** The commit captured at `lock`
  time is trusted as-is; tofulock catches drift *after* that point.
- **Cover providers.** Providers already have native checksum locking; tofulock
  is only about modules.
- **Lock transitive / nested modules** that a pinned module itself calls — only
  the modules in the configuration directory you point it at (for now).
- **Hash non-git registry or archive downloads** (tarballs, `s3::`,
  `https://….zip`) — these are reported as `skipped`.
- **Manage keys or provide a transparency log.** You hold the ed25519 key; there
  is no Sigstore/Rekor identity yet.
- **Decide policy.** It produces evidence and a pass/fail drift gate; it does not
  decide which modules are allowed.

## Source coverage

| Source kind                       | `list` | `lock`                          | `verify`                  |
| --------------------------------- | :----: | ------------------------------- | ------------------------- |
| git (`git::`, shorthand, `?ref=`) |   ✅   | ✅ pinned to commit SHA          | ✅ drift detection         |
| registry (`ns/name/provider`)     |   ✅   | ✅ constraint → version → commit | ✅ version + commit drift  |
| local (`./…`)                     |   ✅   | skipped (versioned with root)   | n/a                       |
| archive (`s3::`, `https://….zip`) |   ✅   | skipped (roadmap)               | roadmap                   |

A git commit SHA *is* a content hash (a Merkle root over the tree), so pinning a
git module to its resolved commit is genuine content verification. Registry
modules are resolved through the Module Registry Protocol (service discovery →
version list → constraint selection → download endpoint), then pinned to the
commit behind the selected version — covering the git-backed modules that make
up the bulk of the public registry. `verify` flags two kinds of registry drift:
the constraint now selecting a newer version, and a published version being
re-pointed to a different commit.

## Lockfile

`.tofulock.lock.json` is sorted by module name and timestamp-free, so it is
byte-stable across runs and produces clean review diffs:

```json
{
  "version": 1,
  "tool": "tofulock",
  "modules": [
    {
      "name": "vpc_git",
      "source": "git::https://github.com/terraform-aws-modules/terraform-aws-vpc.git//?ref=v5.8.1",
      "type": "git",
      "constraint": "v5.8.1",
      "clone_url": "https://github.com/terraform-aws-modules/terraform-aws-vpc.git",
      "resolved_commit": "…",
      "digest": "git:sha1:…",
      "status": "locked"
    }
  ]
}
```

## Design

Module discovery uses HashiCorp's own
[`terraform-config-inspect`](https://github.com/hashicorp/terraform-config-inspect),
so source parsing matches Terraform/OpenTofu semantics rather than being
re-implemented. Git resolution shells out to `git ls-remote`, which returns the
commit SHA for a ref without cloning.

```
main.go
└─ internal/
   ├─ cli/        command dispatch (list / lock / verify / attest / …)
   ├─ tfmod/      module-call discovery via terraform-config-inspect
   ├─ resolve/    source classification + git ref → commit resolution
   ├─ registry/   Module Registry Protocol: discovery, version select, download
   ├─ lock/       resolution engine shared by lock and verify
   ├─ lockfile/   deterministic lockfile read/write
   └─ attest/     in-toto statement + DSSE envelope + ed25519 signing
```

See [THREAT_MODEL.md](THREAT_MODEL.md) for the trust assumptions and
[CONTRIBUTING.md](CONTRIBUTING.md) to build and test.

## Roadmap

- **Terragrunt** module sources (`terragrunt.hcl` `terraform { source }`).
- **OCI** (`oci://`) module sources.
- **Keyless signing** via Sigstore/Fulcio with Rekor transparency-log inclusion.
- Non-git registry & archive sources (`s3::`, `gcs::`, `https://….zip`) via
  downloaded content hash.
- HCL (`.hcl`) lockfile output alongside JSON.
- Approved-module allowlists and source-host pinning.

## License

MIT — see [LICENSE](LICENSE).
