package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/Conalh/tofulock/internal/resolve"
)

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
