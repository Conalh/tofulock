package resolve

import (
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		source string
		want   Kind
	}{
		{"terraform-aws-modules/vpc/aws", KindRegistry},
		{"app.terraform.io/example/vpc/aws", KindRegistry},
		{"./modules/app", KindLocal},
		{"../shared", KindLocal},
		{"git::https://github.com/hashicorp/example.git//consul?ref=v1.2.0", KindGit},
		{"github.com/Azure/terraform-azurerm-network?ref=v3.5.0", KindGit},
		{"git@github.com:org/repo.git", KindGit},
		{"git::ssh://git@example.com/repo.git", KindGit},
		{"s3::https://s3.amazonaws.com/bucket/vpc.zip", KindArchive},
		{"https://example.com/vpc-module.zip", KindArchive},
		{"hg::https://example.com/vpc", KindOther},
	}
	for _, c := range cases {
		if got := Classify(c.source); got != c.want {
			t.Errorf("Classify(%q) = %q, want %q", c.source, got, c.want)
		}
	}
}

func TestParseGit(t *testing.T) {
	cases := []struct {
		source   string
		cloneURL string
		subdir   string
		ref      string
	}{
		{
			"git::https://github.com/hashicorp/example.git//consul?ref=v1.2.0",
			"https://github.com/hashicorp/example.git", "consul", "v1.2.0",
		},
		{
			"github.com/Azure/terraform-azurerm-network?ref=v3.5.0",
			"https://github.com/Azure/terraform-azurerm-network", "", "v3.5.0",
		},
		{
			"git::https://gitlab.com/group/repo.git//modules/vpc?ref=main&depth=1",
			"https://gitlab.com/group/repo.git", "modules/vpc", "main",
		},
		{
			"git::https://example.com/network.git",
			"https://example.com/network.git", "", "",
		},
	}
	for _, c := range cases {
		gs, err := ParseGit(c.source)
		if err != nil {
			t.Errorf("ParseGit(%q) error: %v", c.source, err)
			continue
		}
		if gs.CloneURL != c.cloneURL || gs.Subdir != c.subdir || gs.Ref != c.ref {
			t.Errorf("ParseGit(%q) = %+v, want {CloneURL:%q Subdir:%q Ref:%q}",
				c.source, gs, c.cloneURL, c.subdir, c.ref)
		}
	}
}

func TestParseGitRejectsUnsafeTransportsAndOptions(t *testing.T) {
	for _, source := range []string{
		"git::--upload-pack=sh?ref=.",
		"git::ext::sh -c id",
		"git::file:///tmp/repo.git",
		"git::http://example.com/repo.git",
		"git::https://token@example.com/repo.git",
		"git::ssh://git:secret@example.com/repo.git",
		"git@-oProxyCommand=evil:repo.git",
		"git@bad;host:repo.git",
		"git::example.com/repo.git",
	} {
		if _, err := ParseGit(source); err == nil {
			t.Errorf("ParseGit(%q) expected a safety error", source)
		}
	}
}

func TestParseGitAllowsHTTPSAndSSH(t *testing.T) {
	for _, source := range []string{
		"git::https://example.com/repo.git?ref=v1",
		"git::ssh://git@example.com/repo.git?ref=v1",
		"git@example.com:org/repo.git",
	} {
		if _, err := ParseGit(source); err != nil {
			t.Errorf("ParseGit(%q): %v", source, err)
		}
	}
}

func TestSanitizeSourceRemovesHTTPUserinfo(t *testing.T) {
	got := SanitizeSource("git::https://user:secret@example.com/org/repo.git//mod?ref=v1")
	want := "git::https://example.com/org/repo.git//mod?ref=v1"
	if got != want {
		t.Fatalf("SanitizeSource = %q, want %q", got, want)
	}
	if got := SanitizeSource("git::ssh://git@example.com/org/repo.git"); got != "git::ssh://git@example.com/org/repo.git" {
		t.Fatalf("SSH identity was unexpectedly changed: %q", got)
	}
	got = SanitizeSource("git::ssh://git:secret@example.com/org/repo.git")
	if want := "git::ssh://git@example.com/org/repo.git"; got != want {
		t.Fatalf("SanitizeSource SSH password = %q, want %q", got, want)
	}
}

func TestGitCommitPinnedSHA(t *testing.T) {
	// A 40-hex ref is already a pin: GitCommit must return it (lowercased)
	// without contacting the remote.
	sha := "0123456789ABCDEF0123456789abcdef01234567"
	got, err := GitCommit("https://example.invalid/repo.git", sha)
	if err != nil {
		t.Fatalf("GitCommit: %v", err)
	}
	if got != strings.ToLower(sha) {
		t.Errorf("GitCommit = %q, want lowercased input", got)
	}
}

func TestGitCommitRejectsOptionShapedRepository(t *testing.T) {
	if _, err := GitCommit("--upload-pack=sh", "0123456789abcdef0123456789abcdef01234567"); err == nil {
		t.Fatal("expected option-shaped repository to be rejected before git execution")
	}
}

func TestIsHex40(t *testing.T) {
	if !isHex40("0123456789abcdef0123456789abcdef01234567") {
		t.Error("expected valid 40-hex SHA to pass")
	}
	if isHex40("v1.2.0") {
		t.Error("expected tag to fail isHex40")
	}
	if isHex40("abc") {
		t.Error("expected short string to fail isHex40")
	}
}
