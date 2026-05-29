package attest

import (
	"strings"
	"testing"

	"github.com/Conalh/tofulock/internal/lockfile"
)

func sampleLockfile() *lockfile.File {
	return &lockfile.File{
		Version: 1,
		Tool:    "tofulock",
		Modules: []lockfile.Module{
			{Name: "vpc", Source: "git::https://github.com/x/y.git?ref=v1", Type: "git",
				ResolvedCommit: "abc123", Digest: "git:sha1:abc123", Status: "locked"},
			{Name: "app", Source: "./modules/app", Type: "local", Status: "skipped"},
		},
	}
}

func TestBuildStatement(t *testing.T) {
	stmt := BuildStatement(sampleLockfile(), []byte("lockfile-bytes"), "0.4.0-test", "platform-eng@example.com")

	if stmt.Type != StatementType || stmt.PredicateType != PredicateType {
		t.Fatalf("wrong type/predicateType: %q / %q", stmt.Type, stmt.PredicateType)
	}
	// Only the locked git module becomes a subject; the skipped local one does not.
	if len(stmt.Subject) != 1 {
		t.Fatalf("got %d subjects, want 1", len(stmt.Subject))
	}
	if stmt.Subject[0].Name != "vpc" || stmt.Subject[0].Digest["gitCommit"] != "abc123" {
		t.Errorf("unexpected subject: %+v", stmt.Subject[0])
	}
	if !strings.HasPrefix(stmt.Predicate.LockfileSHA256, "sha256:") {
		t.Errorf("lockfile digest not prefixed: %q", stmt.Predicate.LockfileSHA256)
	}
	if stmt.Predicate.Approval.ApprovedBy != "platform-eng@example.com" {
		t.Errorf("approval not recorded: %+v", stmt.Predicate.Approval)
	}
	if len(stmt.Predicate.ControlMappings) == 0 {
		t.Error("expected control mappings to be populated")
	}
}

func TestPAEVector(t *testing.T) {
	got := string(pae(PayloadType, []byte("x")))
	want := "DSSEv1 28 application/vnd.in-toto+json 1 x"
	if got != want {
		t.Errorf("pae = %q, want %q", got, want)
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	priv, pub, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	stmt := []byte(`{"_type":"x"}`)
	env := Sign(stmt, priv)
	if env.PayloadType != PayloadType || len(env.Signatures) != 1 {
		t.Fatalf("bad envelope: %+v", env)
	}
	payload, err := env.Verify(pub)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if string(payload) != string(stmt) {
		t.Errorf("payload mismatch: %q", payload)
	}

	// Tampering the payload must break verification.
	env.Payload = env.Payload[:len(env.Payload)-2] + "00"
	if _, err := env.Verify(pub); err == nil {
		t.Error("expected verification to fail on tampered payload")
	}

	// A different key must not verify.
	_, otherPub, _ := GenerateKey()
	good := Sign(stmt, priv)
	if _, err := good.Verify(otherPub); err == nil {
		t.Error("expected verification to fail with wrong key")
	}
}

func TestKeyPEMRoundTrip(t *testing.T) {
	priv, pub, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	privPEM, err := EncodePrivatePEM(priv)
	if err != nil {
		t.Fatal(err)
	}
	pubPEM, err := EncodePublicPEM(pub)
	if err != nil {
		t.Fatal(err)
	}
	priv2, err := ParsePrivatePEM(privPEM)
	if err != nil {
		t.Fatal(err)
	}
	pub2, err := ParsePublicPEM(pubPEM)
	if err != nil {
		t.Fatal(err)
	}
	env := Sign([]byte("payload"), priv2)
	if _, err := env.Verify(pub2); err != nil {
		t.Errorf("round-tripped keys failed to sign/verify: %v", err)
	}
}
