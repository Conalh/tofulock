package cli

import (
	"fmt"
	"os"

	"github.com/Conalh/tofulock/internal/lock"
	"github.com/Conalh/tofulock/internal/lockfile"
)

type lockReport struct {
	Lockfile  string            `json:"lockfile"`
	Locked    int               `json:"locked"`
	Skipped   int               `json:"skipped"`
	Errored   int               `json:"errored"`
	Preserved bool              `json:"preserved,omitempty"` // true when an existing lockfile was kept despite errors
	Modules   []lockfile.Module `json:"modules"`
}

func cmdLock(dir string, jsonOut, force bool) int {
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
	// A failed resolution must not silently destroy a known-good lockfile: a
	// transient git ls-remote or registry blip would replace real pins with
	// "error" entries, and the previous commits would be gone from version
	// control on the next commit. Preserve the existing lockfile unless the
	// user explicitly opts in with --force. A first-time lock (no existing
	// file) is always written so the attempt is visible.
	if errored > 0 && !force {
		if _, statErr := os.Stat(lockfile.Path(dir)); statErr == nil {
			if jsonOut {
				emitJSON(lockReport{
					Lockfile: lockfile.Path(dir), Locked: locked, Skipped: skipped,
					Errored: errored, Preserved: true, Modules: f.Modules,
				})
			} else {
				fmt.Fprintf(os.Stderr, "\nlock had %d error(s); existing %s preserved (use --force to overwrite).\n",
					errored, lockfile.FileName)
			}
			return 1
		}
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
