package main

import (
	"os"
	"strings"
	"testing"

	"github.com/Conalh/tofulock/internal/cli"
)

func TestReleaseMetadataAgrees(t *testing.T) {
	version := cli.Version

	assertFileContains(t, "CHANGELOG.md", "## ["+version+"]")
	assertFileContains(t, "README.md", "uses: Conalh/tofulock@v"+version)
	assertFileContains(t, "go.mod", "go 1.25.0")

	action := readFile(t, "action.yml")
	if strings.Contains(action, "go install github.com/Conalh/tofulock") {
		t.Fatal("action.yml must build the checked-out Action source")
	}
	if strings.Contains(action, "\n  version:") {
		t.Fatal("action.yml must not accept a version that can differ from the Action ref")
	}
}

func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	contents := readFile(t, path)
	if !strings.Contains(contents, want) {
		t.Fatalf("%s does not contain %q", path, want)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(contents)
}
