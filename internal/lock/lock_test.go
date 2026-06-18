package lock

import (
	"strings"
	"testing"

	"github.com/Conalh/tofulock/internal/tfmod"
)

// Module routes through resolve.GitCommit and registry.Resolve, both of which
// touch the network. These tests stick to paths that resolve offline so the
// suite stays hermetic (the registry package has its own httptest coverage):
//   - local sources are skipped,
//   - a 40-hex git ref is already a pin (GitCommit short-circuits, no network),
//   - a malformed git source errors at ParseGit, no network,
//   - archive/other sources are skipped as a roadmap gap.
const hexRef = "0123456789ABCDEF0123456789abcdef01234567"

func TestModuleLocalSkipped(t *testing.T) {
	m := Module(tfmod.Call{Name: "app", Source: "./modules/app"})
	if m.Status != "skipped" || m.Type != "local" {
		t.Errorf("local module = %+v, want skipped/local", m)
	}
}

func TestModuleGitPinnedOffline(t *testing.T) {
	m := Module(tfmod.Call{Name: "vpc", Source: "git::https://example.com/repo.git?ref=" + hexRef})
	if m.Status != "locked" {
		t.Fatalf("status = %q, want locked: %+v", m.Status, m)
	}
	want := strings.ToLower(hexRef)
	if m.ResolvedCommit != want {
		t.Errorf("resolved_commit = %q, want %q", m.ResolvedCommit, want)
	}
	if m.Digest != "git:sha1:"+want {
		t.Errorf("digest = %q, want git:sha1:%s", m.Digest, want)
	}
	if m.CloneURL != "https://example.com/repo.git" {
		t.Errorf("clone_url = %q", m.CloneURL)
	}
	if m.Constraint != hexRef {
		t.Errorf("constraint = %q, want %q", m.Constraint, hexRef)
	}
}

func TestModuleGitParseError(t *testing.T) {
	// "git::" classifies as git but ParseGit fails — an error without network.
	m := Module(tfmod.Call{Name: "bad", Source: "git::"})
	if m.Status != "error" || m.Note == "" {
		t.Errorf("malformed git = %+v, want error with a note", m)
	}
}

func TestModuleArchiveSkipped(t *testing.T) {
	for _, src := range []string{
		"s3::https://s3.amazonaws.com/bucket/vpc.zip",
		"https://example.com/vpc-module.zip",
	} {
		m := Module(tfmod.Call{Name: "arch", Source: src})
		if m.Status != "skipped" {
			t.Errorf("archive %q = %+v, want skipped", src, m)
		}
		if !strings.Contains(m.Note, "roadmap") {
			t.Errorf("archive %q note = %q, want a roadmap mention", src, m.Note)
		}
	}
}

// TestModuleGitCommitDeterministic pins the fix for non-deterministic peeled-
// tag selection: pinning the same 40-hex ref twice must yield the same commit
// and the same digest, every run.
func TestModuleGitCommitDeterministic(t *testing.T) {
	first := Module(tfmod.Call{Name: "vpc", Source: "git::https://example.com/repo.git?ref=" + hexRef})
	for i := 0; i < 5; i++ {
		again := Module(tfmod.Call{Name: "vpc", Source: "git::https://example.com/repo.git?ref=" + hexRef})
		if again != first {
			t.Fatalf("run %d differs from first: %+v vs %+v", i, again, first)
		}
	}
	if first.Digest != "git:sha1:"+strings.ToLower(hexRef) {
		t.Errorf("digest = %q", first.Digest)
	}
}
