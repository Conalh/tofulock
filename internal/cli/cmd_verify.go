package cli

import (
	"fmt"
	"os"

	"github.com/Conalh/tofulock/internal/lock"
	"github.com/Conalh/tofulock/internal/lockfile"
	"github.com/Conalh/tofulock/internal/tfmod"
)

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
