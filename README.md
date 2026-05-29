# tofulock

**Lock and verify Terraform / OpenTofu *module* sources by content digest.**

`tofulock` closes a documented hole in the Terraform/OpenTofu dependency model:
the native lock file pins **providers**, but never **modules**.

> From the OpenTofu docs: *"the dependency lock file tracks only **provider**
> dependencies. OpenTofu does not remember version selections for remote
> modules."* Terraform's docs say the same, adding that it *"will always select
> the newest available module version that meets the specified version
> constraints"* unless an exact version is pinned.

So a `module` block pinned to `?ref=v1.2.0` or constrained to `~> 5.0` is **not**
content-pinned. A moved tag, a force-pushed branch, a re-published registry
version, or a compromised upstream silently changes what you deploy — and
`tofu init` won't notice. Providers get trust-on-first-use checksum
verification; modules get nothing.

`tofulock` records the resolved commit for every git-sourced module in a small,
deterministic sidecar lockfile, and fails CI when reality drifts from it.

---

## Why this, why now

- **The gap is real and upstream-acknowledged.** Open feature requests to lock
  modules like providers have sat open for years
  ([opentofu/opentofu#586](https://github.com/opentofu/opentofu/issues/586),
  [hashicorp/terraform#31301](https://github.com/hashicorp/terraform/issues/31301)).
- **The users are integrity-sensitive by default** — this is infrastructure.
- **OpenTofu is community/Linux-Foundation governed**, so there's room for an
  independent provenance layer rather than fighting a vendor's commercial core.

`tofulock` deliberately owns the *layer above the primitive*: lock, verify,
drift detection, and (on the roadmap) policy gates and audit attestation — the
parts that survive even if a bare lock primitive eventually lands upstream.

## Status

Early v0. Working today:

| Source kind            | `list` | `lock`            | `verify`          |
| ---------------------- | :----: | ----------------- | ----------------- |
| git (`git::`, shorthand, `?ref=`) | ✅ | ✅ pinned to commit SHA | ✅ drift detection |
| local (`./…`)          | ✅     | skipped (versioned with root) | n/a       |
| registry (`ns/name/provider`) | ✅ | skipped (roadmap) | roadmap           |
| archive (`s3::`, `https://….zip`) | ✅ | skipped (roadmap) | roadmap   |

A git commit SHA *is* a content hash (a Merkle root over the tree), so pinning a
git module to its resolved commit is genuine content verification — the same
property that makes provider checksums meaningful.

## Install

```sh
go install github.com/Conalh/tofulock@latest
# or, from a clone:
go build -o tofulock .
```

Requires `git` on `PATH` (used for `git ls-remote` — no module content is
downloaded during lock/verify).

## Usage

```sh
tofulock list   [dir]   # show module calls and their classified source kind
tofulock lock   [dir]   # resolve git modules to commits, write the lockfile
tofulock verify [dir]   # re-resolve and fail (exit 1) on any drift
```

`dir` defaults to `.`.

```console
$ tofulock lock ./examples/basic
  skip    vpc_registry           (registry, not lockable yet)
  locked  vpc_git                v5.8.1 @ 8a1...
  locked  network                v4.1.2 @ c3f...
  skip    local_app              (local)

wrote .tofulock.lock.json  (2 locked, 2 skipped, 0 error)
```

In CI, add a gate:

```sh
tofulock verify .   # exit 1 if any locked module's ref now points elsewhere
```

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
so source parsing matches Terraform/OpenTofu semantics exactly rather than being
re-implemented. Git resolution shells out to `git ls-remote`, which returns the
commit SHA for a ref without cloning.

```
main.go
└─ internal/
   ├─ cli/        command dispatch (list / lock / verify)
   ├─ tfmod/      module-call discovery via terraform-config-inspect
   ├─ resolve/    source classification + git ref → commit resolution
   └─ lockfile/   deterministic lockfile read/write
```

## Roadmap

- Registry source locking (resolve constraint → version → download → digest).
- Archive sources (`s3::`, `gcs::`, `https://….zip`) via content hash.
- HCL (`.hcl`) lockfile output alongside JSON.
- `--ci` / machine-readable output and a GitHub Action.
- Policy gate: allowlists, source-host pinning, attestation export for audit.

## License

TBD.
