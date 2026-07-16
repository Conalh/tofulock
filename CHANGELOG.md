# Changelog

All notable changes to tofulock are documented here. The project is experimental,
so releases may still introduce breaking changes before 1.0.

## [0.6.1] - 2026-07-16

### Security

- Restrict Git resolution to HTTPS and SSH sources, reject option-shaped and
  unsafe clone URLs, and pass an explicit option terminator to `git ls-remote`.
- Reject credentials embedded in HTTP(S) or SSH URLs and sanitize persisted
  module sources so credentials cannot enter lockfiles or attestations.
- Validate registry hosts, lockfile schema/tool identifiers, DSSE payload types,
  and in-toto statement metadata before verification.
- Document the network boundary for configurations supplied by untrusted forks.

### Fixed

- Compare the complete module identity during verification, including source,
  type, constraint, clone URL, and subdirectory. Repository or subdirectory
  changes that happen to resolve to the same commit now fail verification.
- Reject a previously local/skipped module that changes to a remote module.
- Explain identity drift even when the old and new commit hashes are equal.

### Changed

- Require Go 1.25 or newer and refresh direct and transitive dependencies.
- Expand CI across supported Go versions and operating systems, with formatting,
  linting, vulnerability, race, Action smoke, and end-to-end checks.
- Build the composite Action from the exact checked-out Action source.
- Add automated cross-platform release archives and checksums.

## [0.6.0]

- Add explicit OpenTofu registry support and OpenTofu examples.

## [0.5.0]

- Add composite GitHub Action inputs for CI verification.

## [0.4.0]

- Add signed DSSE/in-toto attestations and the initial public OSS documentation.

[0.6.1]: https://github.com/Conalh/tofulock/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/Conalh/tofulock/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/Conalh/tofulock/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/Conalh/tofulock/releases/tag/v0.4.0
