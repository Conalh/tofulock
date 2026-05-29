package terragrunt

import (
	"path/filepath"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct{ in, addr, ver string }{
		{"tfr://registry.terraform.io/terraform-aws-modules/vpc/aws?version=5.8.1",
			"registry.terraform.io/terraform-aws-modules/vpc/aws", "5.8.1"},
		{"tfr:///terraform-aws-modules/vpc/aws?version=5.8.1",
			"terraform-aws-modules/vpc/aws", "5.8.1"},
		{"git::https://github.com/x/y.git//m?ref=v1",
			"git::https://github.com/x/y.git//m?ref=v1", ""},
	}
	for _, c := range cases {
		a, v := normalize(c.in)
		if a != c.addr || v != c.ver {
			t.Errorf("normalize(%q) = (%q, %q), want (%q, %q)", c.in, a, v, c.addr, c.ver)
		}
	}
}

func TestDiscoverExample(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "terragrunt")
	calls, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "terragrunt" {
		t.Errorf("name = %q, want terragrunt", calls[0].Name)
	}
	if calls[0].Source == "" {
		t.Error("expected a non-empty source")
	}
}

func TestDiscoverNoFile(t *testing.T) {
	calls, err := Discover(t.TempDir())
	if err != nil {
		t.Fatalf("Discover on empty dir: %v", err)
	}
	if calls != nil {
		t.Errorf("expected nil for a dir without terragrunt.hcl, got %v", calls)
	}
}
