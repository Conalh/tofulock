// Package cli implements the tofulock command-line interface.
package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/Conalh/tofulock/internal/lock"
	"github.com/Conalh/tofulock/internal/lockfile"
	"github.com/Conalh/tofulock/internal/resolve"
	"github.com/Conalh/tofulock/internal/tfmod"
)

// Version is the tofulock release version.
const Version = "0.2.0-dev"

// Run dispatches a tofulock subcommand and returns a process exit code.
func Run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "list":
		return cmdList(dirArg(rest))
	case "lock":
		return cmdLock(dirArg(rest))
	case "verify":
		return cmdVerify(dirArg(rest))
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

// dirArg returns the first non-flag argument, defaulting to the current dir.
func dirArg(rest []string) string {
	for _, a := range rest {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return "."
}

func cmdList(dir string) int {
	calls, err := tfmod.Discover(dir)
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
		kind := resolve.Classify(c.Source)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.Name, kind, refOrVersion(c), c.Source)
	}
	_ = w.Flush()
	return 0
}

func cmdLock(dir string) int {
	calls, err := tfmod.Discover(dir)
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
		fmt.Println(statusLine(m))
		f.Modules = append(f.Modules, m)
	}
	if err := lockfile.Write(dir, f); err != nil {
		fmt.Fprintln(os.Stderr, "tofulock:", err)
		return 1
	}
	fmt.Printf("\nwrote %s  (%d locked, %d skipped, %d error)\n",
		lockfile.FileName, locked, skipped, errored)
	if errored > 0 {
		return 1
	}
	return 0
}

func cmdVerify(dir string) int {
	lf, err := lockfile.Read(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tofulock: cannot read %s: %v\n(hint: run `tofulock lock` first)\n",
			lockfile.FileName, err)
		return 1
	}
	calls, err := tfmod.Discover(dir)
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

	var problems int
	for _, want := range lf.Modules {
		if want.Status != "locked" {
			continue
		}
		c, ok := current[want.Name]
		if !ok {
			fmt.Printf("  REMOVED %-22s present in lockfile, absent from config\n", want.Name)
			problems++
			continue
		}
		got := lock.Module(c)
		switch {
		case got.Status != "locked":
			fmt.Printf("  ERROR   %-22s %s\n", want.Name, got.Note)
			problems++
		case got.Version != want.Version:
			fmt.Printf("  DRIFT   %-22s constraint now selects %s (locked %s)\n",
				want.Name, orDash(got.Version), orDash(want.Version))
			problems++
		case got.ResolvedCommit != want.ResolvedCommit:
			fmt.Printf("  DRIFT   %-22s %s\n            locked %s\n            now    %s\n",
				want.Name, pinLabel(want), want.ResolvedCommit, got.ResolvedCommit)
			problems++
		default:
			fmt.Printf("  ok      %-22s %s @ %s\n", want.Name, pinLabel(want), short(want.ResolvedCommit))
		}
	}

	// Lockable modules added to config but missing from the lockfile.
	for _, c := range calls {
		if stored[c.Name] {
			continue
		}
		if lock.Module(c).Status == "locked" {
			fmt.Printf("  NEW     %-22s in config, missing from lockfile\n", c.Name)
			problems++
		}
	}

	if problems > 0 {
		fmt.Printf("\nFAIL: %d problem(s). Run `tofulock lock` to update the lockfile.\n", problems)
		return 1
	}
	fmt.Println("\nOK: every locked module matches its recorded pin.")
	return 0
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
	fmt.Fprintf(w, `tofulock %s — lock & verify Terraform/OpenTofu module sources by digest

USAGE:
  tofulock <command> [dir]

COMMANDS:
  list      List the module calls found in a config directory
  lock      Resolve module sources to pinned commits and write %s
  verify    Re-resolve sources and fail on drift from the lockfile
  version   Print the version
  help      Show this help

The native .terraform.lock.hcl records provider dependencies only; module
versions are never pinned. tofulock fills that gap for git and registry
sources, pinning each to a commit.

  dir defaults to "." when omitted.
`, Version, lockfile.FileName)
}
