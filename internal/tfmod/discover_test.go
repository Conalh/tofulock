package tfmod

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverBasicExample(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "basic")
	calls, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover(%s): %v", dir, err)
	}
	got := map[string]Call{}
	for _, c := range calls {
		got[c.Name] = c
	}
	for _, name := range []string{"vpc_registry", "vpc_git", "network", "local_app"} {
		if _, ok := got[name]; !ok {
			t.Errorf("expected module %q in discovered calls, got %v", name, keys(got))
		}
	}
	if v := got["vpc_registry"].Version; v != "5.8.1" {
		t.Errorf("vpc_registry version = %q, want 5.8.1", v)
	}
	// Calls must come back sorted by name.
	for i := 1; i < len(calls); i++ {
		if calls[i-1].Name > calls[i].Name {
			t.Errorf("calls not sorted: %q before %q", calls[i-1].Name, calls[i].Name)
		}
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverTofuShadowing(t *testing.T) {
	dir := t.TempDir()
	// main.tofu shadows main.tf entirely (OpenTofu precedence); extra.tf has
	// no .tofu counterpart and stays visible.
	write(t, dir, "main.tf", "module \"old\" {\n  source = \"./old\"\n}\n")
	write(t, dir, "main.tofu", "module \"vpc\" {\n  source = \"git::https://example.com/vpc.git?ref=v1\"\n}\n")
	write(t, dir, "extra.tf", "module \"extra\" {\n  source = \"./extra\"\n}\n")

	calls, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	got := map[string]Call{}
	for _, c := range calls {
		got[c.Name] = c
	}
	if _, ok := got["old"]; ok {
		t.Error("main.tf should be shadowed by main.tofu, but its module was discovered")
	}
	if c, ok := got["vpc"]; !ok || c.Source != "git::https://example.com/vpc.git?ref=v1" {
		t.Errorf("vpc from main.tofu = %+v", got["vpc"])
	}
	if _, ok := got["extra"]; !ok {
		t.Error("extra.tf (not shadowed) should still be discovered")
	}
}

func TestDiscoverTofuJSON(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "mods.tofu.json", `{"module":{"reg":{"source":"ns/name/aws","version":"~> 1.0"}}}`)
	calls, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(calls) != 1 || calls[0].Name != "reg" || calls[0].Source != "ns/name/aws" || calls[0].Version != "~> 1.0" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestDiscoverTofuInterpolatedSource(t *testing.T) {
	dir := t.TempDir()
	// A non-literal source must surface verbatim (classified unresolvable),
	// not vanish.
	write(t, dir, "main.tofu", "module \"dyn\" {\n  source = local.src\n}\n")
	calls, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(calls) != 1 || calls[0].Source != "local.src" {
		t.Errorf("calls = %+v", calls)
	}
}

func keys(m map[string]Call) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
