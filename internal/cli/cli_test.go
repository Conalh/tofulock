package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Conalh/tofulock/internal/lockfile"
)

func TestParseArgs(t *testing.T) {
	cases := []struct {
		args    []string
		want    runOpts
		wantErr bool
	}{
		{nil, runOpts{dir: "."}, false},
		{[]string{"./envs/dev"}, runOpts{dir: "./envs/dev"}, false},
		{[]string{"--json"}, runOpts{dir: ".", json: true}, false},
		{[]string{"./x", "-json"}, runOpts{dir: "./x", json: true}, false},
		{[]string{"--registry-host", "registry.opentofu.org"}, runOpts{dir: ".", registryHost: "registry.opentofu.org"}, false},
		{[]string{"--registry-host=registry.opentofu.org", "./x"}, runOpts{dir: "./x", registryHost: "registry.opentofu.org"}, false},
		{[]string{"--registry-host"}, runOpts{}, true},    // missing value
		{[]string{"--jsno"}, runOpts{}, true},             // typo'd flag must not pass silently
		{[]string{"--directory", "x"}, runOpts{}, true},   // unknown flag
		{[]string{"a", "b"}, runOpts{}, true},             // extra positional
	}
	for _, c := range cases {
		o, err := parseArgs(c.args)
		if (err != nil) != c.wantErr {
			t.Errorf("parseArgs(%v) err = %v, wantErr %v", c.args, err, c.wantErr)
			continue
		}
		if err == nil && o != c.want {
			t.Errorf("parseArgs(%v) = %+v, want %+v", c.args, o, c.want)
		}
	}
}

func TestSplitDir(t *testing.T) {
	valueFlags := map[string]bool{"key": true}

	dir, flags, err := splitDir([]string{"--key", "k.pem", "./envs/dev"}, valueFlags)
	if err != nil {
		t.Fatalf("splitDir: %v", err)
	}
	if dir != "./envs/dev" || len(flags) != 2 || flags[0] != "--key" || flags[1] != "k.pem" {
		t.Errorf("splitDir = (%q, %v)", dir, flags)
	}

	dir, flags, err = splitDir([]string{"./envs/dev", "--key", "k.pem"}, valueFlags)
	if err != nil || dir != "./envs/dev" || len(flags) != 2 {
		t.Errorf("flags after dir: (%q, %v, %v)", dir, flags, err)
	}

	if _, _, err := splitDir([]string{"a", "b"}, valueFlags); err == nil {
		t.Error("expected error for extra positional argument")
	}
}

func TestKeygenRefusesOverwrite(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), "signer")
	if code := cmdKeygen([]string{"--out", prefix}); code != 0 {
		t.Fatalf("first keygen exit = %d, want 0", code)
	}
	orig, err := os.ReadFile(prefix + ".key")
	if err != nil {
		t.Fatalf("reading generated key: %v", err)
	}

	if code := cmdKeygen([]string{"--out", prefix}); code != 1 {
		t.Fatalf("second keygen exit = %d, want 1 (refuse to overwrite)", code)
	}
	after, _ := os.ReadFile(prefix + ".key")
	if string(orig) != string(after) {
		t.Error("existing private key was clobbered")
	}

	if code := cmdKeygen([]string{"--out", prefix, "--force"}); code != 0 {
		t.Fatalf("keygen --force exit = %d, want 0", code)
	}
	forced, _ := os.ReadFile(prefix + ".key")
	if string(orig) == string(forced) {
		t.Error("--force did not regenerate the key")
	}
}

func writeConfig(t *testing.T, dir, tf string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(tf), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyFailsOnErrorLockfileEntry(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "module \"app\" {\n  source = \"./modules/app\"\n}\n")

	// A lockfile whose entries are all skipped/local verifies clean.
	if err := lockfile.Write(dir, &lockfile.File{Modules: []lockfile.Module{
		{Name: "app", Source: "./modules/app", Type: "local", Status: "skipped"},
	}}); err != nil {
		t.Fatal(err)
	}
	if code := cmdVerify(dir, false); code != 0 {
		t.Fatalf("clean verify exit = %d, want 0", code)
	}

	// An "error" entry means the module was never pinned: verify must fail
	// even though the module no longer resolves (or never did).
	if err := lockfile.Write(dir, &lockfile.File{Modules: []lockfile.Module{
		{Name: "app", Source: "./modules/app", Type: "local", Status: "skipped"},
		{Name: "vpc", Source: "git::https://example.com/vpc.git?ref=v1", Type: "git",
			Status: "error", Note: "auth failed during lock"},
	}}); err != nil {
		t.Fatal(err)
	}
	if code := cmdVerify(dir, false); code != 1 {
		t.Errorf("verify exit = %d, want 1: error-status lockfile entries must fail verification", code)
	}
}

func TestVerifyFlagsUnresolvableNewModule(t *testing.T) {
	dir := t.TempDir()
	// "git::" classifies as git but fails ParseGit — an error without network.
	writeConfig(t, dir, "module \"broken\" {\n  source = \"git::\"\n}\n")
	if err := lockfile.Write(dir, &lockfile.File{}); err != nil {
		t.Fatal(err)
	}
	if code := cmdVerify(dir, false); code != 1 {
		t.Errorf("verify exit = %d, want 1: an unlockable module missing from the lockfile must fail", code)
	}
}

func TestVerifyFlagsNewModule(t *testing.T) {
	dir := t.TempDir()
	// A 40-hex ref pins without contacting the remote, so this resolves to
	// "locked" offline and must be reported as new (missing from lockfile).
	writeConfig(t, dir, "module \"pinned\" {\n  source = \"git::https://example.com/repo.git?ref=0123456789abcdef0123456789abcdef01234567\"\n}\n")
	if err := lockfile.Write(dir, &lockfile.File{}); err != nil {
		t.Fatal(err)
	}
	if code := cmdVerify(dir, false); code != 1 {
		t.Errorf("verify exit = %d, want 1: a lockable module missing from the lockfile must fail", code)
	}
}
