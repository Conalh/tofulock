# Releasing tofulock

Releases remain experimental until the project reaches 1.0.

1. Update `internal/cli.Version`, the README Action example, and `CHANGELOG.md`.
2. Run the local verification gate:

   ```sh
   gofmt -w $(git ls-files '*.go')
   go mod verify
   go vet ./...
   go test ./...
   go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
   ```

3. Commit the release changes and wait for `CI` to pass on `main`.
4. Create and push an annotated tag matching the CLI version:

   ```sh
   git tag -a v0.6.1 -m "tofulock v0.6.1"
   git push origin v0.6.1
   ```

5. The `Release` workflow verifies the tag, builds archives for Linux, Windows,
   and macOS, writes SHA-256 checksum files, and creates a prerelease.
6. Download one archive, verify its checksum, and run `tofulock version` before
   promoting or announcing the release.
