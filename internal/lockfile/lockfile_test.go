package lockfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := &File{
		Modules: []Module{
			{Name: "vpc", Source: "git::https://github.com/x/y.git?ref=v1", Type: "git",
				ResolvedCommit: "abc123", Digest: "git:sha1:abc123", Status: "locked"},
			{Name: "app", Source: "./modules/app", Type: "local", Status: "skipped"},
		},
	}
	if err := Write(dir, in); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if out.Version != SchemaVersion || out.Tool != "tofulock" {
		t.Errorf("header = {Version:%d Tool:%q}, want {%d tofulock}", out.Version, out.Tool, SchemaVersion)
	}
	if len(out.Modules) != 2 {
		t.Fatalf("got %d modules, want 2", len(out.Modules))
	}
	// Modules must be sorted by name: app before vpc.
	if out.Modules[0].Name != "app" || out.Modules[1].Name != "vpc" {
		t.Errorf("modules not sorted: %q, %q", out.Modules[0].Name, out.Modules[1].Name)
	}
	if out.Modules[1].ResolvedCommit != "abc123" {
		t.Errorf("vpc commit = %q, want abc123", out.Modules[1].ResolvedCommit)
	}
}

func TestWriteIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	f := &File{Modules: []Module{
		{Name: "b", Status: "skipped"}, {Name: "a", Status: "skipped"},
	}}
	if err := Write(dir, f); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(dir, FileName))
	if err := Write(dir, f); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, FileName))
	if string(first) != string(second) {
		t.Error("lockfile output is not byte-stable across writes")
	}
}

func TestReadRejectsUnknownSchemaAndTool(t *testing.T) {
	for name, body := range map[string]string{
		"schema": `{"version":99,"tool":"tofulock","modules":[]}`,
		"tool":   `{"version":1,"tool":"other","modules":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Read(dir); err == nil {
				t.Fatal("expected incompatible lockfile to be rejected")
			}
			if _, _, err := ReadRaw(dir); err == nil {
				t.Fatal("expected ReadRaw to reject incompatible lockfile")
			}
		})
	}
}
