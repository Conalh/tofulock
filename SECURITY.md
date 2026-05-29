# Security policy

`tofulock` is **experimental** software. It is offered without warranty (see
[LICENSE](LICENSE)). Please do not rely on it as your only supply-chain control.

## Reporting a vulnerability

Please report security issues **privately** via GitHub's private vulnerability
reporting:

> Repository **Security** tab → **Report a vulnerability**
> (<https://github.com/Conalh/tofulock/security/advisories/new>)

Do not open a public issue for a suspected vulnerability. I'll acknowledge
reports as soon as I reasonably can; since this is a solo, experimental project,
please allow time for a fix before any public disclosure.

When reporting, include:

- the tofulock version / commit,
- the command and module source involved,
- what you observed vs. expected, and a reproduction if possible.

## Scope

In scope — issues in tofulock itself, for example:

- a way to make `verify` report `ok` when a locked module's commit has actually
  changed (a verification bypass),
- accepting a forged or malformed DSSE signature as valid,
- mis-parsing a module source so the wrong artifact is pinned,
- leaking signing keys or writing them with unsafe permissions.

Out of scope — by design (see [THREAT_MODEL.md](THREAT_MODEL.md)):

- vulnerabilities in the *modules* you lock (tofulock proves they haven't
  changed, not that they're safe),
- the trust-on-first-use baseline,
- provider integrity (handled natively by Terraform/OpenTofu),
- transitive/nested modules and non-git archive sources (not yet covered).

## Supported versions

Only the latest `main` is supported while the project is experimental. There are
no backported fixes for older tags.
