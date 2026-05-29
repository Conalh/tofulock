// Package attest builds a signed, audit-grade module-provenance record from a
// tofulock lockfile. The record is an in-toto Statement (one subject per pinned
// module, digested by git commit) wrapped in a DSSE envelope and signed with
// ed25519. The format is compatible with cosign / Sigstore / Rekor, so keyless
// transparency-log signing is a drop-in roadmap step rather than a rewrite.
package attest

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"

	"github.com/Conalh/tofulock/internal/lockfile"
)

const (
	// StatementType is the in-toto Statement schema URI.
	StatementType = "https://in-toto.io/Statement/v1"
	// PredicateType identifies tofulock's module-provenance predicate.
	PredicateType = "https://tofulock.dev/attestation/module-provenance/v0.1"
	// PayloadType is the DSSE payload type for in-toto statements.
	PayloadType = "application/vnd.in-toto+json"
)

// ControlMappings are the audited change-management controls a module-provenance
// record helps evidence. Surfaced in the predicate for auditor consumption.
var ControlMappings = []string{"SOC2 CC8.1", "FedRAMP CM-3", "FedRAMP CM-4", "PCI DSS 6.5.1"}

// Statement is an in-toto v1 statement.
type Statement struct {
	Type          string    `json:"_type"`
	Subject       []Subject `json:"subject"`
	PredicateType string    `json:"predicateType"`
	Predicate     Predicate `json:"predicate"`
}

// Subject is an attested artifact, digested by its git commit.
type Subject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

// Predicate is tofulock's module-provenance payload.
type Predicate struct {
	Tool            ToolInfo          `json:"tool"`
	LockfileSHA256  string            `json:"lockfileSha256"`
	ControlMappings []string          `json:"controlMappings"`
	Approval        Approval          `json:"approval"`
	Modules         []lockfile.Module `json:"modules"`
}

// ToolInfo records the producing tool and version.
type ToolInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Approval records who signed off on this module set.
type Approval struct {
	ApprovedBy string `json:"approvedBy,omitempty"`
	Note       string `json:"note,omitempty"`
}

// BuildStatement constructs the in-toto statement for a lockfile. rawLockfile is
// the exact on-disk lockfile bytes, so the recorded digest binds the attestation
// to a specific lockfile revision.
func BuildStatement(lf *lockfile.File, rawLockfile []byte, toolVersion, approvedBy string) *Statement {
	sum := sha256.Sum256(rawLockfile)
	stmt := &Statement{
		Type:          StatementType,
		PredicateType: PredicateType,
		Predicate: Predicate{
			Tool:            ToolInfo{Name: "tofulock", Version: toolVersion},
			LockfileSHA256:  "sha256:" + hex.EncodeToString(sum[:]),
			ControlMappings: ControlMappings,
			Approval:        Approval{ApprovedBy: approvedBy},
			Modules:         lf.Modules,
		},
	}
	for _, m := range lf.Modules {
		if m.Status == "locked" && m.ResolvedCommit != "" {
			stmt.Subject = append(stmt.Subject, Subject{
				Name:   m.Name,
				Digest: map[string]string{"gitCommit": m.ResolvedCommit},
			})
		}
	}
	return stmt
}

// Marshal renders v as indented JSON.
func Marshal(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// Envelope is a DSSE envelope.
type Envelope struct {
	PayloadType string      `json:"payloadType"`
	Payload     string      `json:"payload"`
	Signatures  []Signature `json:"signatures"`
}

// Signature is one DSSE signature.
type Signature struct {
	KeyID string `json:"keyid,omitempty"`
	Sig   string `json:"sig"`
}

// pae computes the DSSE Pre-Authentication Encoding:
// "DSSEv1" SP len(type) SP type SP len(body) SP body.
func pae(payloadType string, payload []byte) []byte {
	return []byte(fmt.Sprintf("DSSEv1 %d %s %d %s",
		len(payloadType), payloadType, len(payload), payload))
}

// Sign wraps statement bytes in a DSSE envelope signed with priv.
func Sign(statement []byte, priv ed25519.PrivateKey) Envelope {
	sig := ed25519.Sign(priv, pae(PayloadType, statement))
	return Envelope{
		PayloadType: PayloadType,
		Payload:     base64.StdEncoding.EncodeToString(statement),
		Signatures:  []Signature{{Sig: base64.StdEncoding.EncodeToString(sig)}},
	}
}

// Verify checks that at least one signature is valid for pub and returns the
// decoded payload (the statement bytes).
func (e Envelope) Verify(pub ed25519.PublicKey) ([]byte, error) {
	payload, err := base64.StdEncoding.DecodeString(e.Payload)
	if err != nil {
		return nil, fmt.Errorf("decoding payload: %w", err)
	}
	msg := pae(e.PayloadType, payload)
	for _, s := range e.Signatures {
		sig, err := base64.StdEncoding.DecodeString(s.Sig)
		if err != nil {
			continue
		}
		if ed25519.Verify(pub, msg, sig) {
			return payload, nil
		}
	}
	return nil, fmt.Errorf("no signature in the envelope is valid for the provided key")
}

// GenerateKey returns a fresh ed25519 keypair.
func GenerateKey() (ed25519.PrivateKey, ed25519.PublicKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	return priv, pub, err
}

// EncodePrivatePEM serializes priv as a PKCS#8 PEM block.
func EncodePrivatePEM(priv ed25519.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// EncodePublicPEM serializes pub as a PKIX PEM block.
func EncodePublicPEM(pub ed25519.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

// ParsePrivatePEM parses a PKCS#8 PEM ed25519 private key.
func ParsePrivatePEM(b []byte) (ed25519.PrivateKey, error) {
	blk, _ := pem.Decode(b)
	if blk == nil {
		return nil, fmt.Errorf("no PEM block found in private key")
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is not an ed25519 private key")
	}
	return priv, nil
}

// ParsePublicPEM parses a PKIX PEM ed25519 public key.
func ParsePublicPEM(b []byte) (ed25519.PublicKey, error) {
	blk, _ := pem.Decode(b)
	if blk == nil {
		return nil, fmt.Errorf("no PEM block found in public key")
	}
	k, err := x509.ParsePKIXPublicKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := k.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("key is not an ed25519 public key")
	}
	return pub, nil
}
