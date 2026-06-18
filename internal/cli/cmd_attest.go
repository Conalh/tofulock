package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Conalh/tofulock/internal/attest"
	"github.com/Conalh/tofulock/internal/lockfile"
)

func cmdAttest(rest []string) int {
	dir, flagArgs, err := splitDir(rest, map[string]bool{"key": true, "out": true, "approved-by": true})
	if err != nil {
		return argErr("attest", err)
	}
	fs := flag.NewFlagSet("attest", flag.ContinueOnError)
	keyPath := fs.String("key", "", "ed25519 private key (PEM) to sign a DSSE envelope; unsigned statement if omitted")
	outPath := fs.String("out", "", "write output to this file instead of stdout")
	approvedBy := fs.String("approved-by", "", "identity recorded as the approver in the attestation")
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}

	lf, raw, err := lockfile.ReadRaw(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tofulock: cannot read %s: %v\n(hint: run `tofulock lock` first)\n", lockfile.FileName, err)
		return 1
	}
	stmt := attest.BuildStatement(lf, raw, Version, *approvedBy)
	stmtBytes, err := attest.Marshal(stmt)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tofulock:", err)
		return 1
	}

	out := stmtBytes
	signed := false
	if *keyPath != "" {
		keyBytes, err := os.ReadFile(*keyPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tofulock: reading signing key: %v\n", err)
			return 1
		}
		priv, err := attest.ParsePrivatePEM(keyBytes)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tofulock: loading signing key: %v\n", err)
			return 1
		}
		env := attest.Sign(stmtBytes, priv)
		if out, err = attest.Marshal(env); err != nil {
			fmt.Fprintln(os.Stderr, "tofulock:", err)
			return 1
		}
		signed = true
	}

	if *outPath == "" {
		fmt.Println(string(out))
		return 0
	}
	if err := os.WriteFile(*outPath, append(out, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "tofulock:", err)
		return 1
	}
	kind := "attestation"
	if signed {
		kind = "signed attestation"
	}
	fmt.Fprintf(os.Stderr, "wrote %s %s (%d subjects)\n", kind, *outPath, len(stmt.Subject))
	return 0
}

func cmdVerifyAttest(rest []string) int {
	dir, flagArgs, err := splitDir(rest, map[string]bool{"key": true, "att": true})
	if err != nil {
		return argErr("verify-attest", err)
	}
	fs := flag.NewFlagSet("verify-attest", flag.ContinueOnError)
	keyPath := fs.String("key", "", "ed25519 public key (PEM) to verify the DSSE signature (required)")
	attPath := fs.String("att", "", "attestation envelope file (default: <dir>/"+dsseFile+")")
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if *keyPath == "" {
		fmt.Fprintln(os.Stderr, "tofulock: --key (public key PEM) is required")
		return 2
	}
	envPath := *attPath
	if envPath == "" {
		envPath = filepath.Join(dir, dsseFile)
	}

	keyBytes, err := os.ReadFile(*keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tofulock: reading public key: %v\n", err)
		return 1
	}
	pub, err := attest.ParsePublicPEM(keyBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tofulock: loading public key: %v\n", err)
		return 1
	}
	envBytes, err := os.ReadFile(envPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tofulock: reading attestation %s: %v\n", envPath, err)
		return 1
	}
	var env attest.Envelope
	if err := json.Unmarshal(envBytes, &env); err != nil {
		fmt.Fprintf(os.Stderr, "tofulock: parsing attestation: %v\n", err)
		return 1
	}
	payload, err := env.Verify(pub)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tofulock: signature check FAILED: %v\n", err)
		return 1
	}
	var stmt attest.Statement
	if err := json.Unmarshal(payload, &stmt); err != nil {
		fmt.Fprintf(os.Stderr, "tofulock: parsing statement: %v\n", err)
		return 1
	}
	fmt.Println("  signature ok")

	lf, raw, err := lockfile.ReadRaw(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tofulock: cannot read %s: %v\n", lockfile.FileName, err)
		return 1
	}
	locked := make(map[string]string, len(lf.Modules))
	for _, m := range lf.Modules {
		if m.Status == "locked" {
			locked[m.Name] = m.ResolvedCommit
		}
	}

	problems := 0
	// The lockfile digest binds the attestation to a specific lockfile revision.
	// BuildStatement always records it, so a statement without one is either
	// forged, hand-edited, or from an incompatible older tofulock — none of
	// those should pass verification. Missing digest or missing lockfile is a
	// hard failure, not a silent skip.
	sum := sha256.Sum256(raw)
	cur := "sha256:" + hex.EncodeToString(sum[:])
	switch {
	case stmt.Predicate.LockfileSHA256 == "":
		fmt.Println("  CHANGED   attestation has no lockfile digest; cannot bind to the lockfile")
		problems++
	case stmt.Predicate.LockfileSHA256 != cur:
		fmt.Println("  CHANGED   lockfile digest differs from the attestation")
		problems++
	}
	for _, s := range stmt.Subject {
		want := s.Digest["gitCommit"]
		got, ok := locked[s.Name]
		switch {
		case !ok:
			fmt.Printf("  MISSING   %-22s attested module no longer locked in config\n", s.Name)
			problems++
		case got != want:
			fmt.Printf("  MISMATCH  %-22s attested %s, lockfile %s\n", s.Name, short(want), short(got))
			problems++
		default:
			fmt.Printf("  ok        %-22s %s\n", s.Name, short(want))
		}
	}
	if problems > 0 {
		fmt.Printf("\nFAIL: signature valid but %d subject/lockfile problem(s).\n", problems)
		return 1
	}
	fmt.Printf("\nOK: signature valid and all %d attested subjects match the lockfile.\n", len(stmt.Subject))
	return 0
}
