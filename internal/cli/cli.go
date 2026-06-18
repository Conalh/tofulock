// Package cli implements the tofulock command-line interface.
//
// Command dispatch and shared helpers live here; each subcommand's
// implementation is in its own cmd_*.go file so the per-command surfaces stay
// navigable.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Conalh/tofulock/internal/lockfile"
	"github.com/Conalh/tofulock/internal/registry"
	"github.com/Conalh/tofulock/internal/resolve"
	"github.com/Conalh/tofulock/internal/terragrunt"
	"github.com/Conalh/tofulock/internal/tfmod"
)

// Version is the tofulock release version.
const Version = "0.6.0"

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
		o, err := parseArgs(rest)
		if err != nil {
			return argErr(cmd, err)
		}
		applyRegistryHost(o.registryHost)
		return cmdList(o.dir)
	case "lock":
		o, err := parseArgs(rest)
		if err != nil {
			return argErr(cmd, err)
		}
		applyRegistryHost(o.registryHost)
		return cmdLock(o.dir, o.json, o.force)
	case "verify":
		o, err := parseArgs(rest)
		if err != nil {
			return argErr(cmd, err)
		}
		applyRegistryHost(o.registryHost)
		return cmdVerify(o.dir, o.json)
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

// runOpts are the shared options of list/lock/verify.
type runOpts struct {
	dir          string
	json         bool
	registryHost string
	force        bool
}

// parseArgs extracts the target directory (one optional positional arg,
// default ".") and the shared flags. Unknown flags and extra positional args
// are errors. Used by list/lock/verify. --force is honored only by lock; it is
// accepted (and ignored) by list/verify for a uniform flag surface.
func parseArgs(rest []string) (o runOpts, err error) {
	o.dir = "."
	dirSet := false
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "-json" || a == "--json":
			o.json = true
		case a == "-force" || a == "--force":
			o.force = true
		case a == "-registry-host" || a == "--registry-host":
			if i+1 >= len(rest) {
				return o, fmt.Errorf("%s requires a value", a)
			}
			i++
			o.registryHost = rest[i]
		case strings.HasPrefix(a, "--registry-host="):
			o.registryHost = strings.TrimPrefix(a, "--registry-host=")
		case strings.HasPrefix(a, "-registry-host="):
			o.registryHost = strings.TrimPrefix(a, "-registry-host=")
		case strings.HasPrefix(a, "-"):
			return o, fmt.Errorf("unknown flag %q", a)
		case dirSet:
			return o, fmt.Errorf("unexpected extra argument %q", a)
		default:
			o.dir, dirSet = a, true
		}
	}
	return o, nil
}

// applyRegistryHost sets the registry used for bare module addresses:
// --registry-host flag, then TOFULOCK_REGISTRY_HOST, then Terraform's default.
// OpenTofu resolves bare addresses against registry.opentofu.org, so OpenTofu
// users should set one of these — consistently for both lock and verify, or
// verify may see version drift that tofu init would not.
func applyRegistryHost(flagVal string) {
	host := flagVal
	if host == "" {
		host = os.Getenv("TOFULOCK_REGISTRY_HOST")
	}
	if host != "" {
		registry.DefaultHost = host
	}
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
  --registry-host HOST   Registry for bare module addresses (list, lock, verify).
                         Default registry.terraform.io; OpenTofu users want
                         registry.opentofu.org. Env: TOFULOCK_REGISTRY_HOST.
  --force                lock: overwrite a known-good lockfile even when some
                         modules fail to resolve (default preserves it).
                         keygen: overwrite existing key files.
  --key PATH             Signing/verifying key (attest, verify-attest)
  --out PATH             Output file (attest, keygen prefix)
  --approved-by NAME     Approver identity recorded in the attestation (attest)
  --att PATH             Attestation envelope to verify (verify-attest)

The native .terraform.lock.hcl records provider dependencies only; module
versions are never pinned. tofulock pins git and registry modules to a commit
and can emit a signed, audit-grade provenance record over them.

  dir defaults to "." when omitted.
`, Version, lockfile.FileName)
}
