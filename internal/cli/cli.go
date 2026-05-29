// Package cli implements the tofulock command-line interface.
package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/Conalh/tofulock/internal/lockfile"
	"github.com/Conalh/tofulock/internal/resolve"
	"github.com/Conalh/tofulock/internal/tfmod"
)

// Version is the tofulock release version.
const Version = "0.1.0-dev"

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
		m := lockfile.Module{Name: c.Name, Source: c.Source}
		kind := resolve.Classify(c.Source)
		m.Type = string(kind)
		switch kind {
		case resolve.KindGit:
			gs, perr := resolve.ParseGit(c.Source)
			if perr != nil {
				m.Status, m.Note = "error", perr.Error()
				errored++
				fmt.Printf("  error   %-22s %v\n", c.Name, perr)
				break
			}
			m.CloneURL, m.Subdir, m.Constraint = gs.CloneURL, gs.Subdir, gs.Ref
			sha, rerr := resolve.GitCommit(gs.CloneURL, gs.Ref)
			if rerr != nil {
				m.Status, m.Note = "error", rerr.Error()
				errored++
				fmt.Printf("  error   %-22s %v\n", c.Name, rerr)
				break
			}
			m.ResolvedCommit = sha
			m.Digest = "git:sha1:" + sha
			m.Status = "locked"
			locked++
			fmt.Printf("  locked  %-22s %s @ %s\n", c.Name, displayRef(gs.Ref), short(sha))
		case resolve.KindLocal:
			m.Status = "skipped"
			m.Note = "local module; versioned with the root module"
			skipped++
			fmt.Printf("  skip    %-22s (local)\n", c.Name)
		default:
			m.Status = "skipped"
			m.Constraint = c.Version
			m.Note = fmt.Sprintf("%s sources are not lockable yet (see roadmap)", kind)
			skipped++
			fmt.Printf("  skip    %-22s (%s, not lockable yet)\n", c.Name, kind)
		}
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
	inLock := make(map[string]bool, len(lf.Modules))
	for _, m := range lf.Modules {
		inLock[m.Name] = true
	}

	var problems int
	for _, m := range lf.Modules {
		if m.Status != "locked" {
			continue
		}
		c, ok := current[m.Name]
		if !ok {
			fmt.Printf("  REMOVED %-22s present in lockfile, absent from config\n", m.Name)
			problems++
			continue
		}
		gs, perr := resolve.ParseGit(c.Source)
		if perr != nil {
			fmt.Printf("  ERROR   %-22s %v\n", m.Name, perr)
			problems++
			continue
		}
		sha, rerr := resolve.GitCommit(gs.CloneURL, gs.Ref)
		if rerr != nil {
			fmt.Printf("  ERROR   %-22s %v\n", m.Name, rerr)
			problems++
			continue
		}
		if sha != m.ResolvedCommit {
			fmt.Printf("  DRIFT   %-22s %s\n            locked %s\n            now    %s\n",
				m.Name, displayRef(gs.Ref), m.ResolvedCommit, sha)
			problems++
			continue
		}
		fmt.Printf("  ok      %-22s %s @ %s\n", m.Name, displayRef(gs.Ref), short(sha))
	}
	// Lockable modules added to config but never locked.
	for _, c := range calls {
		if inLock[c.Name] {
			continue
		}
		if resolve.Classify(c.Source) == resolve.KindGit {
			fmt.Printf("  NEW     %-22s in config, missing from lockfile\n", c.Name)
			problems++
		}
	}

	if problems > 0 {
		fmt.Printf("\nFAIL: %d problem(s). Run `tofulock lock` to update the lockfile.\n", problems)
		return 1
	}
	fmt.Println("\nOK: every locked module matches its recorded commit.")
	return 0
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

func displayRef(ref string) string {
	if ref == "" {
		return "(default branch)"
	}
	return ref
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
versions are never pinned. tofulock fills that gap for git-sourced modules
today, with registry/archive sources on the roadmap.

  dir defaults to "." when omitted.
`, Version, lockfile.FileName)
}
