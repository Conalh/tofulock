// Package tfmod discovers module calls in a Terraform/OpenTofu configuration
// directory using HashiCorp's own terraform-config-inspect parser.
package tfmod

import (
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-config-inspect/tfconfig"
)

// Call is a single `module "<name>" { ... }` block.
type Call struct {
	Name    string
	Source  string
	Version string // version constraint (registry modules only)
	File    string
	Line    int
}

// Discover parses every .tf/.tf.json file in dir (non-recursively, matching
// Terraform's own root-module semantics) and returns its module calls,
// sorted by name for deterministic output.
func Discover(dir string) ([]Call, error) {
	mod, diags := tfconfig.LoadModule(dir)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing %s: %s", dir, diags.Error())
	}
	calls := make([]Call, 0, len(mod.ModuleCalls))
	for _, mc := range mod.ModuleCalls {
		calls = append(calls, Call{
			Name:    mc.Name,
			Source:  mc.Source,
			Version: mc.Version,
			File:    mc.Pos.Filename,
			Line:    mc.Pos.Line,
		})
	}
	sort.Slice(calls, func(i, j int) bool { return calls[i].Name < calls[j].Name })
	return calls, nil
}
