# Threat model

`tofulock` is an experimental tool. This document states plainly what it
defends against, what it assumes, and what it explicitly does **not** cover, so
you can judge whether it fits your needs.

## What tofulock is for

Terraform/OpenTofu pin **providers** in `.terraform.lock.hcl` but never
**modules**. A module constrained by version (or even pinned to a mutable git
tag) can resolve to different content over time without any change to your
configuration. tofulock records the exact commit behind every module and fails
when that resolution changes.

## Assets

- The infrastructure code your team actually applies.
- The integrity of the modules that code pulls in (the artifact you run).
- An auditable record of which module versions/commits were approved.

## Trust assumptions

- **Trust on first use (TOFU).** The commit captured at `lock` time is treated
  as the trusted baseline. tofulock does not judge whether that initial commit
  is itself benign — it detects *change* from the baseline.
- **`git ls-remote` and the registry are answered honestly during lock.** A
  remote that lies at lock time would be recorded as the baseline. Verification
  thereafter is meaningful relative to that baseline.
- **The signer controls their ed25519 key.** Attestation integrity is only as
  good as key custody. There is no transparency log or identity binding yet.
- **The lockfile is reviewed in version control.** tofulock makes drift visible
  in diffs and CI; a human/PR review is what turns visibility into a control.

## Threats tofulock detects

| Threat                                                   | How it is caught |
| -------------------------------------------------------- | ---------------- |
| A git **tag is moved / force-pushed** to a new commit    | `verify` re-resolves the ref and compares to the locked commit → drift |
| A **branch advances** under a `?ref=branch` pin          | same — the resolved commit no longer matches |
| A **registry version is re-published** at a new commit   | `verify` re-resolves the version's download → commit and compares |
| A **constraint silently selects a newer version**        | `verify` re-runs version selection and flags the change |
| A module is **added/removed** without re-locking         | `verify` reports `NEW` / `REMOVED` |
| **Tampering with the lockfile** after attestation        | `verify-attest` checks the recorded lockfile SHA-256 and per-subject commits against a signed record |

## Out of scope (does *not* protect against)

- **Malicious or vulnerable code inside a pinned commit.** tofulock proves
  immutability, not safety. Pair it with code review and SCA/misconfig scanners.
- **A compromised baseline.** If the first locked commit is already malicious,
  tofulock will faithfully hold you to it.
- **Providers.** Covered natively by `.terraform.lock.hcl`.
- **Transitive/nested modules** called by a pinned module. Only the modules in
  the configuration directory you scan are pinned (today).
- **Non-git registry/archive downloads** (tarballs, `s3::`, `gcs::`, `http`):
  currently `skipped`, not content-hashed.
- **Key/secret management.** You are responsible for protecting `*.key`.
- **Compromise of the local toolchain** (a malicious `git`, Go, or CI runner).
- **Policy decisions.** tofulock emits evidence and a drift gate; it does not
  decide what is permitted.

## Residual risk

Because resolution is TOFU-based and content safety is out of scope, tofulock
raises the cost of a *silent substitution* attack (the most common module
supply-chain risk in practice) but is not a complete supply-chain assurance
solution. Treat it as one deterministic control in a layered setup, not a
guarantee.

## Reporting

See [SECURITY.md](SECURITY.md) for how to report a vulnerability in the tool
itself.
