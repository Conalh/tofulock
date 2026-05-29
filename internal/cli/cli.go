// Package cli implements the tofulock command-line interface.
package cli

import (
	"encoding/json"
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
const Version = "0.3.0-dev"

// Run dispatches a tofulock subcommand and returns a process exit code.
func Run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}
	cmd, rest := args[0], args[1:]
	dir, jsonOut := parseArgs(rest)
	switch cmd {
	case "list":
		return cmdList(dir)
	case "lock":
		return cmdLock(dir, jsonOut)
	case "verify":
		return cmdVerify(dir, jsonOut)
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

// parseArgs extracts the target directory (first non-flag arg, default ".")
// and whether --json was requested.
func parseArgs(rest []string) (dir string, jsonOut bool) {
	dir = "."
	dirSet := false
	for _, a := range rest {
		switch a {
		case "-json", "--json":
			jsonOut = true
		default:
			if !strings.HasPrefix(a, "-") && !dirSet {
				dir, dirSet = a, true
			}
		}
	}
	return dir, jsonOut
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
	Status        string `json:"status"` // ok | drift | error | removed | new
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

	var entries []verifyEntry
	var problems, checked int

	for _, want := range lf.Modules {
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
		if lock.Module(c).Status == "locked" {
			entries = append(entries, verifyEntry{
				Name: c.Name, Kind: string(resolve.Classify(c.Source)),
				Status: "new", Detail: "in config, missing from lockfile",
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
	fmt.Fprintf(w, `tofulock %s — lock & verify Terraform/OpenTofu module sources by digest

USAGE:
  tofulock <command> [dir] [--json]

COMMANDS:
  list      List the module calls found in a config directory
  lock      Resolve module sources to pinned commits and write %s
  verify    Re-resolve sources and fail (exit 1) on drift from the lockfile
  version   Print the version
  help      Show this help

FLAGS:
  --json    Emit machine-readable JSON (lock, verify) for CI integration

The native .terraform.lock.hcl records provider dependencies only; module
versions are never pinned. tofulock fills that gap for git and registry
sources, pinning each to a commit.

  dir defaults to "." when omitted.
`, Version, lockfile.FileName)
}
