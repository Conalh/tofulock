package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/Conalh/tofulock/internal/attest"
)

func cmdKeygen(rest []string) int {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	out := fs.String("out", "tofulock", "output key path prefix (writes <prefix>.key and <prefix>.pub)")
	force := fs.Bool("force", false, "overwrite existing key files")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	privFile, pubFile := *out+".key", *out+".pub"
	// Check both paths before writing either, so a refusal can never leave a
	// fresh .key next to a stale .pub (a mismatched pair).
	if !*force {
		for _, p := range []string{privFile, pubFile} {
			if _, err := os.Stat(p); err == nil {
				fmt.Fprintf(os.Stderr, "tofulock: %s already exists; use --force to overwrite\n", p)
				return 1
			}
		}
	}
	priv, pub, err := attest.GenerateKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tofulock:", err)
		return 1
	}
	privPEM, err := attest.EncodePrivatePEM(priv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tofulock:", err)
		return 1
	}
	pubPEM, err := attest.EncodePublicPEM(pub)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tofulock:", err)
		return 1
	}
	if err := os.WriteFile(privFile, privPEM, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "tofulock:", err)
		return 1
	}
	if err := os.WriteFile(pubFile, pubPEM, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "tofulock:", err)
		return 1
	}
	fmt.Printf("wrote %s (private, keep secret) and %s (public)\n", privFile, pubFile)
	return 0
}
