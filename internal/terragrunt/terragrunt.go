// Package terragrunt discovers the module source declared in a terragrunt.hcl
// `terraform { source = ... }` block, so tofulock can lock/verify/attest
// Terragrunt units the same way it handles Terraform/OpenTofu module calls.
package terragrunt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/Conalh/tofulock/internal/tfmod"
	"github.com/Conalh/tofulock/internal/util"
)

// FileName is the Terragrunt unit configuration file.
const FileName = "terragrunt.hcl"

// Discover reads dir/terragrunt.hcl (if present) and returns the module source
// from its `terraform { source = ... }` block as a tfmod.Call. A `tfr://`
// registry source is normalized to a registry address + version constraint so
// the existing registry resolver handles it. A non-literal (interpolated)
// source is returned verbatim, so it surfaces as an unresolvable/skipped entry
// rather than being silently dropped.
func Discover(dir string) ([]tfmod.Call, error) {
	path := filepath.Join(dir, FileName)
	src, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	f, diags := hclsyntax.ParseConfig(src, path, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing %s: %s", path, diags.Error())
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return nil, nil
	}

	var calls []tfmod.Call
	for _, blk := range body.Blocks {
		if blk.Type != "terraform" {
			continue
		}
		attr, ok := blk.Body.Attributes["source"]
		if !ok {
			continue
		}
		line := attr.SrcRange.Start.Line
		// A literal string source evaluates with a nil context; an interpolated
		// one (locals/functions) does not.
		if v, vdiags := attr.Expr.Value(nil); !vdiags.HasErrors() && !v.IsNull() && v.Type() == cty.String {
			source, version := normalize(v.AsString())
			calls = append(calls, tfmod.Call{Name: "terragrunt", Source: source, Version: version, File: path, Line: line})
			continue
		}
		rng := attr.Expr.Range()
		raw := strings.TrimSpace(string(src[rng.Start.Byte:rng.End.Byte]))
		calls = append(calls, tfmod.Call{Name: "terragrunt", Source: raw, File: path, Line: line})
	}
	return calls, nil
}

// normalize converts a Terragrunt `tfr://` registry source into a plain registry
// address plus its version constraint. Non-tfr sources pass through unchanged.
func normalize(source string) (addr, version string) {
	if !strings.HasPrefix(source, "tfr://") {
		return source, ""
	}
	rest := strings.TrimPrefix(source, "tfr://")
	if i := strings.Index(rest, "?"); i >= 0 {
		version = util.QueryGet(rest[i+1:], "version")
		rest = rest[:i]
	}
	// tfr:///ns/name/provider uses the default registry host (leading slash).
	return strings.TrimPrefix(rest, "/"), version
}
