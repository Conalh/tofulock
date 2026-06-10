// Package cli implements the tofulock command-line interface.
package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/Conalh/tofulock/internal/attest"
	"github.com/Conalh/tofulock/internal/lock"
	"github.com/Conalh/tofulock/internal/lockfile"
	"github.com/Conalh/tofulock/internal/resolve"
	"github.com/Conalh/tofulock/internal/terragrunt"
	"github.com/Conalh/tofulock/internal/tfmod"
)

// Version is the tofulock release version.
const Version = "0.5.0"

// Default attestation filenames.
const (
	attestFile = "tofulock.attestation.json"
	dsseFile   = "tofulock.attestation.dsse.json"
)

// Run dispatches a tofulock subcommand and returns a process exit code.
func Run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "list":
		dir, _, err := parseArgs(rest)
		if err != nil {
			return argErr(cmd, err)
		}
		return cmdList(dir)
	case "lock":
		dir, jsonOut, err := parseArgs(rest)
		if err != nil {
			return argErr(cmd, err)
		}
		return cmdLock(dir, jsonOut)
	case "verify":
		dir, jsonOut, err := parseArgs(rest)
		if err != nil {
			return argErr(cmd, err)
		}
		return cmdVerify(dir, jsonOut)
	case "attest":
		return cmdAttest(rest)
	case "verify-attest":
		return cmdVerifyAttest(rest)
	case "keygen":
		return cmdKeygen(rest)
	case "version", "--version", "-v":
		fmt.Println("tofulock " + Version)
		return 0
	case "help", "-h", "--help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "tofulock: unknown command %q\n\n", cmd)
		usage(os.Stderr)
		return 2
	}
}

// argErr reports a bad command line. A typo'd flag must fail loudly (exit 2):
// silently ignoring it in a CI gate would give false confidence.
func argErr(cmd string, err error) int {
	fmt.Fprintf(os.Stderr, "tofulock %s: %v\n", cmd, err)
	return 2
}

// parseArgs extracts the target directory (one optional positional arg,
// default ".") and whether --json was requested. Unknown flags and extra
// positional args are errors. Used by list/lock/verify.
func parseArgs(rest []string) (dir string, jsonOut bool, err error) {
	dir = "."
	dirSet := false
	for _, a := range rest {
		switch {
		case a == "-json" || a == "--json":
			jsonOut = true
		case strings.HasPrefix(a, "-"):
			return "", false, fmt.Errorf("unknown flag %q", a)
		case dirSet:
			return "", false, fmt.Errorf("unexpected extra argument %q", a)
		default:
			dir, dirSet = a, true
		}
	}
	return dir, jsonOut, nil
}

// splitDir separates the optional positional directory from flag args so flags
// may appear before or after the directory (Go's flag package otherwise stops
// parsing at the first positional). valueFlags lists flags that consume a value.
func splitDir(rest []string, valueFlags map[string]bool) (dir string, flags []string, err error) {
	dir = "."
	dirSet := false
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			if !strings.Contains(a, "=") {
				name := strings.TrimLeft(a, "-")
				if valueFlags[name] && i+1 < len(rest) {
					flags = append(flags, rest[i+1])
					i++
				}
			}
			continue
		}
		if dirSet {
			return "", nil, fmt.Errorf("unexpected extra argument %q", a)
		}
		dir, dirSet = a, true
	}
	return dir, flags, nil
}

// discoverAll combines Terraform/OpenTofu module calls with any Terragrunt
// terraform{} source in the directory, sorted by name for determinism.
func discoverAll(dir string) ([]tfmod.Call, error) {
	calls, err := tfmod.Discover(dir)
	if err != nil {
		return nil, err
	}
	tg, err := terragrunt.Discover(dir)
	if err != nil {
		return nil, err
	}
	calls = append(calls, tg...)
	sort.Slice(calls, func(i, j int) bool { return calls[i].Name < calls[j].Name })
	return calls, nil
}

func cmdList(dir string) int {
	calls, err := discoverAll(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tofulock:", err)
		return 1
	}
	if len(calls) == 0 {
		fmt.Printf("No module calls found in %s\n", dir)
		return 0
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tVERSION/REF\tSOURCE")
	for _, c := range calls {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.Name, resolve.Classify(c.Source), refOrVersion(c), c.Source)
	}
	_ = w.Flush()
	return 0
}

type lockReport struct {
	Lockfile string            `json:"lockfile"`
	Locked   int               `json:"locked"`
	Skipped  int               `json:"skipped"`
	Errored  int               `json:"errored"`
	Modules  []lockfile.Module `json:"modules"`
}

func cmdLock(dir string, jsonOut bool) int {
	calls, err := discoverAll(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tofulock:", err)
		return 1
	}
	f := &lockfile.File{}
	var locked, skipped, errored int
	for _, c := range calls {
		m := lock.Module(c)
		switch m.Status {
		case "locked":
			locked++
		case "error":
			errored++
		default:
			skipped++
		}
		if !jsonOut {
			fmt.Println(statusLine(m))
		}
		f.Modules = append(f.Modules, m)
	}
	if err := lockfile.Write(dir, f); err != nil {
		fmt.Fprintln(os.Stderr, "tofulock:", err)
		return 1
	}
	if jsonOut {
		emitJSON(lockReport{
			Lockfile: lockfile.Path(dir), Locked: locked, Skipped: skipped,
			Errored: errored, Modules: f.Modules,
		})
	} else {
		fmt.Printf("\nwrote %s  (%d locked, %d skipped, %d error)\n",
			lockfile.FileName, locked, skipped, errored)
	}
	if errored > 0 {
		return 1
	}
	return 0
}

type verifyEntry struct {
	Name          string `json:"name"`
	Kind          string `json:"kind,omitempty"`
	Status        string `json:"status"` // ok | drift | error | removed | new | unlocked
	Pin           string `json:"pin,omitempty"`
	LockedCommit  string `json:"locked_commit,omitempty"`
	CurrentCommit string `json:"current_commit,omitempty"`
	Detail        string `json:"detail,omitempty"`
}

type verifyReport struct {
	OK       bool          `json:"ok"`
	Dir      string        `json:"dir"`
	Checked  int           `json:"checked"`
	Problems int           `json:"problems"`
	Results  []verifyEntry `json:"results"`
}

func cmdVerify(dir string, jsonOut bool) int {
	lf, err := lockfile.Read(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tofulock: cannot read %s: %v\n(hint: run `tofulock lock` first)\n",
			lockfile.FileName, err)
		return 1
	}
	calls, err := discoverAll(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tofulock:", err)
		return 1
	}

	current := make(map[string]tfmod.Call, len(calls))
	for _, c := range calls {
		current[c.Name] = c
	}
	stored := make(map[string]bool, len(lf.Modules))
	for _, m := range lf.Modules {
		stored[m.Name] = true
	}

	var entries []verifyEntry
	var problems, checked int

	for _, want := range lf.Modules {
		// An "error" lockfile entry means lock never pinned this module; it
		// must fail verification until a clean lock run, or it stays
		// unpinned (and invisible) forever.
		if want.Status == "error" {
			entries = append(entries, verifyEntry{
				Name: want.Name, Kind: want.Type, Status: "unlocked",
				Detail: "lockfile records a lock-time resolution error: " + want.Note,
			})
			problems++
			continue
		}
		if want.Status != "locked" {
			continue
		}
		checked++
		e := verifyEntry{Name: want.Name, Kind: want.Type, Pin: pinLabel(want), LockedCommit: want.ResolvedCommit}
		c, ok := current[want.Name]
		if !ok {
			e.Status, e.Detail = "removed", "present in lockfile, absent from config"
			problems++
			entries = append(entries, e)
			continue
		}
		got := lock.Module(c)
		switch {
		case got.Status != "locked":
			e.Status, e.Detail = "error", got.Note
			problems++
		case got.Version != want.Version:
			e.Status, e.CurrentCommit = "drift", got.ResolvedCommit
			e.Detail = fmt.Sprintf("constraint now selects %s (locked %s)", orDash(got.Version), orDash(want.Version))
			problems++
		case got.ResolvedCommit != want.ResolvedCommit:
			e.Status, e.CurrentCommit = "drift", got.ResolvedCommit
			e.Detail = "ref now points to a different commit"
			problems++
		default:
			e.Status, e.CurrentCommit = "ok", got.ResolvedCommit
		}
		entries = append(entries, e)
	}

	for _, c := range calls {
		if stored[c.Name] {
			continue
		}
		got := lock.Module(c)
		switch got.Status {
		case "locked":
			entries = append(entries, verifyEntry{
				Name: c.Name, Kind: got.Type,
				Status: "new", Detail: "in config, missing from lockfile",
			})
			problems++
		case "error":
			entries = append(entries, verifyEntry{
				Name: c.Name, Kind: got.Type,
				Status: "error", Detail: "in config, missing from lockfile, and failed to resolve: " + got.Note,
			})
			problems++
		}
	}

	report := verifyReport{OK: problems == 0, Dir: dir, Checked: checked, Problems: problems, Results: entries}
	if jsonOut {
		emitJSON(report)
	} else {
		renderVerify(entries, problems)
	}
	if problems > 0 {
		return 1
	}
	return 0
}

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

	lf, err := lockfile.Read(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tofulock: cannot read %s: %v\n(hint: run `tofulock lock` first)\n", lockfile.FileName, err)
		return 1
	}
	raw, err := os.ReadFile(lockfile.Path(dir))
	if err != nil {
		fmt.Fprintln(os.Stderr, "tofulock:", err)
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

	lf, err := lockfile.Read(dir)
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
	if raw, err := os.ReadFile(lockfile.Path(dir)); err == nil {
		sum := sha256.Sum256(raw)
		cur := "sha256:" + hex.EncodeToString(sum[:])
		if stmt.Predicate.LockfileSHA256 != "" && stmt.Predicate.LockfileSHA256 != cur {
			fmt.Println("  CHANGED   lockfile digest differs from the attestation")
			problems++
		}
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

func renderVerify(entries []verifyEntry, problems int) {
	for _, e := range entries {
		switch e.Status {
		case "ok":
			fmt.Printf("  ok      %-22s %s @ %s\n", e.Name, e.Pin, short(e.LockedCommit))
		case "drift":
			fmt.Printf("  DRIFT   %-22s %s\n", e.Name, e.Pin)
			if e.LockedCommit != "" && e.CurrentCommit != "" {
				fmt.Printf("            locked %s\n            now    %s\n", e.LockedCommit, e.CurrentCommit)
			} else {
				fmt.Printf("            %s\n", e.Detail)
			}
		case "error":
			fmt.Printf("  ERROR   %-22s %s\n", e.Name, e.Detail)
		case "removed":
			fmt.Printf("  REMOVED %-22s %s\n", e.Name, e.Detail)
		case "new":
			fmt.Printf("  NEW     %-22s %s\n", e.Name, e.Detail)
		case "unlocked":
			fmt.Printf("  UNLOCKED %-21s %s\n", e.Name, e.Detail)
		}
	}
	if problems > 0 {
		fmt.Printf("\nFAIL: %d problem(s). Run `tofulock lock` to update the lockfile.\n", problems)
	} else {
		fmt.Println("\nOK: every locked module matches its recorded pin.")
	}
}

func emitJSON(v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "tofulock: encoding json:", err)
		return
	}
	fmt.Println(string(b))
}

// statusLine renders a one-line summary of a freshly resolved module.
func statusLine(m lockfile.Module) string {
	switch m.Status {
	case "locked":
		return fmt.Sprintf("  locked  %-22s %s @ %s", m.Name, pinLabel(m), short(m.ResolvedCommit))
	case "error":
		return fmt.Sprintf("  error   %-22s %s", m.Name, m.Note)
	default:
		return fmt.Sprintf("  skip    %-22s (%s)", m.Name, m.Type)
	}
}

// pinLabel describes what a locked module is pinned to: a resolved registry
// version, or a git ref.
func pinLabel(m lockfile.Module) string {
	if m.Version != "" {
		if m.Constraint != "" && m.Constraint != m.Version {
			return m.Constraint + " => " + m.Version
		}
		return m.Version
	}
	if m.Constraint == "" {
		return "(default branch)"
	}
	return m.Constraint
}

func refOrVersion(c tfmod.Call) string {
	if c.Version != "" {
		return c.Version
	}
	if gs, err := resolve.ParseGit(c.Source); err == nil && gs.Ref != "" {
		return gs.Ref
	}
	return "-"
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func usage(w *os.File) {
	fmt.Fprintf(w, `tofulock %s — lock, verify & attest Terraform/OpenTofu module sources

USAGE:
  tofulock <command> [dir] [flags]

COMMANDS:
  list             List the module calls found in a config directory
  lock             Resolve module sources to pinned commits and write %s
  verify           Re-resolve sources and fail (exit 1) on drift from the lockfile
  attest           Emit an in-toto module-provenance statement (DSSE-signed with --key)
  verify-attest    Verify a signed attestation and that its subjects match the lockfile
  keygen           Generate an ed25519 keypair for signing attestations
  version          Print the version
  help             Show this help

FLAGS:
  --json                 Machine-readable output (lock, verify)
  --key PATH             Signing/verifying key (attest, verify-attest)
  --out PATH             Output file (attest, keygen prefix)
  --approved-by NAME     Approver identity recorded in the attestation (attest)
  --att PATH             Attestation envelope to verify (verify-attest)
  --force                Overwrite existing key files (keygen)

The native .terraform.lock.hcl records provider dependencies only; module
versions are never pinned. tofulock pins git and registry modules to a commit
and can emit a signed, audit-grade provenance record over them.

  dir defaults to "." when omitted.
`, Version, lockfile.FileName)
}
