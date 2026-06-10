// Package tfmod discovers module calls in a Terraform/OpenTofu configuration
// directory. .tf/.tf.json files are parsed with HashiCorp's own
// terraform-config-inspect; .tofu/.tofu.json files (which tfconfig does not
// recognize) are parsed directly, honoring OpenTofu's precedence rule that a
// foo.tofu file makes the same-basename foo.tf invisible.
package tfmod

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	hcljson "github.com/hashicorp/hcl/v2/json"
	"github.com/hashicorp/terraform-config-inspect/tfconfig"
	"github.com/zclconf/go-cty/cty"
)

// Call is a single `module "<name>" { ... }` block.
type Call struct {
	Name    string
	Source  string
	Version string // version constraint (registry modules only)
	File    string
	Line    int
}

// Discover parses every .tf/.tf.json/.tofu/.tofu.json file in dir
// (non-recursively, matching the tools' own root-module semantics) and returns
// its module calls, sorted by name for deterministic output.
func Discover(dir string) ([]Call, error) {
	mod, diags := tfconfig.LoadModule(dir)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing %s: %s", dir, diags.Error())
	}
	tofuFiles, shadowed, err := tofuLayout(dir)
	if err != nil {
		return nil, err
	}
	var calls []Call
	for _, mc := range mod.ModuleCalls {
		if shadowed[filepath.Base(mc.Pos.Filename)] {
			continue
		}
		calls = append(calls, Call{
			Name:    mc.Name,
			Source:  mc.Source,
			Version: mc.Version,
			File:    mc.Pos.Filename,
			Line:    mc.Pos.Line,
		})
	}
	for _, path := range tofuFiles {
		tc, err := parseTofuFile(path)
		if err != nil {
			return nil, err
		}
		calls = append(calls, tc...)
	}
	sort.Slice(calls, func(i, j int) bool { return calls[i].Name < calls[j].Name })
	return calls, nil
}

// tofuLayout lists dir's .tofu/.tofu.json files and the .tf/.tf.json filenames
// they shadow: OpenTofu ignores foo.tf entirely when foo.tofu exists.
func tofuLayout(dir string) (tofuFiles []string, shadowed map[string]bool, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	shadowed = map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		var base string
		switch {
		case strings.HasSuffix(name, ".tofu.json"):
			base = strings.TrimSuffix(name, ".tofu.json")
		case strings.HasSuffix(name, ".tofu"):
			base = strings.TrimSuffix(name, ".tofu")
		default:
			continue
		}
		tofuFiles = append(tofuFiles, filepath.Join(dir, name))
		shadowed[base+".tf"] = true
		shadowed[base+".tf.json"] = true
	}
	sort.Strings(tofuFiles)
	return tofuFiles, shadowed, nil
}

var (
	moduleBlockSchema = &hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{{Type: "module", LabelNames: []string{"name"}}},
	}
	moduleCallSchema = &hcl.BodySchema{
		Attributes: []hcl.AttributeSchema{{Name: "source"}, {Name: "version"}},
	}
)

// parseTofuFile extracts module calls from one .tofu/.tofu.json file. Only the
// source and version attributes matter for locking; everything else in the
// block is ignored.
func parseTofuFile(path string) ([]Call, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f *hcl.File
	var diags hcl.Diagnostics
	if strings.HasSuffix(path, ".json") {
		f, diags = hcljson.Parse(src, path)
	} else {
		f, diags = hclsyntax.ParseConfig(src, path, hcl.Pos{Line: 1, Column: 1})
	}
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing %s: %s", path, diags.Error())
	}
	content, _, _ := f.Body.PartialContent(moduleBlockSchema)
	var calls []Call
	for _, blk := range content.Blocks {
		attrs, _, _ := blk.Body.PartialContent(moduleCallSchema)
		calls = append(calls, Call{
			Name:    blk.Labels[0],
			Source:  stringAttr(attrs.Attributes["source"], src),
			Version: stringAttr(attrs.Attributes["version"], src),
			File:    path,
			Line:    blk.DefRange.Start.Line,
		})
	}
	return calls, nil
}

// stringAttr evaluates a literal string attribute; a non-literal
// (interpolated) expression is returned as raw source text so it surfaces as
// an unresolvable entry rather than being silently dropped.
func stringAttr(attr *hcl.Attribute, src []byte) string {
	if attr == nil {
		return ""
	}
	if v, diags := attr.Expr.Value(nil); !diags.HasErrors() && !v.IsNull() && v.Type() == cty.String {
		return v.AsString()
	}
	rng := attr.Expr.Range()
	return strings.TrimSpace(string(src[rng.Start.Byte:rng.End.Byte]))
}
