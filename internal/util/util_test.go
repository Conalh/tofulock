package util

import "testing"

func TestQueryGet(t *testing.T) {
	cases := []struct {
		query, key, want string
	}{
		{"ref=v1.2.0", "ref", "v1.2.0"},
		{"version=5.8.1&depth=1", "version", "5.8.1"},
		{"version=5.8.1&depth=1", "depth", "1"},
		{"a=1&a=2", "a", "1"}, // first occurrence wins
		{"ref=v1.2.0", "missing", ""},
		{"", "ref", ""},
		{"novalue", "novalue", ""},
	}
	for _, c := range cases {
		if got := QueryGet(c.query, c.key); got != c.want {
			t.Errorf("QueryGet(%q, %q) = %q, want %q", c.query, c.key, got, c.want)
		}
	}
}
