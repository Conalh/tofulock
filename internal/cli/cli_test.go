package cli

import (
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Conalh/tofulock/internal/attest"
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
		{[]string{"--registry-host"}, runOpts{}, true},  // missing value
		{[]string{"--jsno"}, runOpts{}, true},           // typo'd flag must not pass silently
		{[]string{"--directory", "x"}, runOpts{}, true}, // unknown flag
		{[]string{"a", "b"}, runOpts{}, true},           // extra positional
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

// TestVerifyAttestRequiresDigest locks in the fix for the digest-bypass bug:
// a signed statement whose predicate has no lockfileSha256 must fail
// verification rather than silently skipping the tampering check. BuildStatement
// always records the digest, so a missing one means a forged or hand-edited
// statement.
func TestVerifyAttestRequiresDigest(t *testing.T) {
	dir := t.TempDir()
	// A lockfile with one offline-pinnable git module so the subject set is
	// non-empty and the happy path is exercisable without network.
	if err := lockfile.Write(dir, &lockfile.File{Modules: []lockfile.Module{
		{Name: "vpc", Source: "git::https://example.com/repo.git?ref=0123456789abcdef0123456789abcdef01234567",
			Type: "git", Status: "locked", ResolvedCommit: "0123456789abcdef0123456789abcdef01234567"},
	}}); err != nil {
		t.Fatal(err)
	}

	priv, pub, err := attest.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	// Happy path: a properly built, signed attestation verifies clean.
	lf, raw, err := lockfile.ReadRaw(dir)
	if err != nil {
		t.Fatal(err)
	}
	good := attest.BuildStatement(lf, raw, "test", "approver@example.com")
	goodBytes, _ := attest.Marshal(good)
	goodEnv := attest.Sign(goodBytes, priv)
	goodEnvPath := filepath.Join(dir, "good.dsse.json")
	gb, _ := json.Marshal(goodEnv)
	if err := os.WriteFile(goodEnvPath, append(gb, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := cmdVerifyAttest([]string{"--key", writePub(t, dir, pub), "--att", goodEnvPath, dir}); code != 0 {
		t.Errorf("happy-path verify-attest exit = %d, want 0", code)
	}

	// Tampered path: same statement but with the lockfile digest blanked.
	tampered := *good
	tampered.Predicate = good.Predicate
	tampered.Predicate.LockfileSHA256 = ""
	tamperedBytes, _ := attest.Marshal(&tampered)
	tamperedEnv := attest.Sign(tamperedBytes, priv)
	tamperedPath := filepath.Join(dir, "tampered.dsse.json")
	tb, _ := json.Marshal(tamperedEnv)
	if err := os.WriteFile(tamperedPath, append(tb, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := cmdVerifyAttest([]string{"--key", filepath.Join(dir, "pub.pem"), "--att", tamperedPath, dir}); code != 1 {
		t.Errorf("tampered (no digest) verify-attest exit = %d, want 1: a missing digest must fail, not skip", code)
	}
}

func writePub(t *testing.T, dir string, pub ed25519.PublicKey) string {
	t.Helper()
	pemBytes, err := attest.EncodePublicPEM(pub)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "pub.pem")
	if err := os.WriteFile(p, pemBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestLockPreservesExistingOnFailure locks in the fix for the clobber bug: a
// re-lock that partially fails must NOT overwrite a known-good lockfile (a
// transient blip used to replace real pins with "error" entries). --force
// opts back in to the destructive overwrite.
func TestLockPreservesExistingOnFailure(t *testing.T) {
	dir := t.TempDir()
	// One offline-pinnable git module (40-hex ref pins without a network call).
	writeConfig(t, dir, "module \"vpc\" {\n  source = \"git::https://example.com/repo.git?ref=0123456789abcdef0123456789abcdef01234567\"\n}\n")
	if err := lockfile.Write(dir, &lockfile.File{Modules: []lockfile.Module{
		{Name: "vpc", Source: "git::https://example.com/repo.git?ref=0123456789abcdef0123456789abcdef01234567",
			Type: "git", Status: "locked", ResolvedCommit: "0123456789abcdef0123456789abcdef01234567",
			Digest: "git:sha1:0123456789abcdef0123456789abcdef01234567"},
	}}); err != nil {
		t.Fatal(err)
	}
	good, err := os.ReadFile(lockfile.Path(dir))
	if err != nil {
		t.Fatal(err)
	}

	// Add a second module whose source fails ParseGit ("git::" with no URL) so
	// lock produces an error entry without touching the network.
	writeConfig(t, dir, "module \"broken\" {\n  source = \"git::\"\n}\n")

	if code := cmdLock(dir, false, false); code != 1 {
		t.Fatalf("lock with errors exit = %d, want 1", code)
	}
	after, err := os.ReadFile(lockfile.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(good) {
		t.Error("lock with errors overwrote a known-good lockfile; it must be preserved without --force")
	}

	// --force allows the overwrite: the lockfile now records the error entry.
	if code := cmdLock(dir, false, true); code != 1 {
		t.Fatalf("lock --force with errors exit = %d, want 1 (still fails, but writes)", code)
	}
	forced, _ := os.ReadFile(lockfile.Path(dir))
	if string(forced) == string(good) {
		t.Error("lock --force did not overwrite the lockfile")
	}
	lf, _ := lockfile.Read(dir)
	var sawBroken bool
	for _, m := range lf.Modules {
		if m.Name == "broken" && m.Status == "error" {
			sawBroken = true
		}
	}
	if !sawBroken {
		t.Error("lock --force did not record the broken module's error entry")
	}
}
