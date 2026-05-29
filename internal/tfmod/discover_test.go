package tfmod

import (
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

func keys(m map[string]Call) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
