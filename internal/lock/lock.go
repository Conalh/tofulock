// Package lock is tofulock's resolution engine: it turns a discovered module
// call into a lockfile entry, pinning git and registry sources to a commit.
// Both `lock` and `verify` route through Module so they resolve identically.
package lock

import (
	"fmt"

	"github.com/Conalh/tofulock/internal/lockfile"
	"github.com/Conalh/tofulock/internal/registry"
	"github.com/Conalh/tofulock/internal/resolve"
	"github.com/Conalh/tofulock/internal/tfmod"
)

// Module resolves a single module call to its lockfile entry. Network access
// happens here (git ls-remote, registry HTTP); failures are captured as an
// "error" status rather than returned, so a single bad module never aborts a
// whole lock/verify run.
func Module(c tfmod.Call) lockfile.Module {
	m := lockfile.Module{Name: c.Name, Source: c.Source}
	kind := resolve.Classify(c.Source)
	m.Type = string(kind)

	switch kind {
	case resolve.KindGit:
		gs, err := resolve.ParseGit(c.Source)
		if err != nil {
			return errMod(m, err)
		}
		m.CloneURL, m.Subdir, m.Constraint = gs.CloneURL, gs.Subdir, gs.Ref
		return pinGit(m, gs.CloneURL, gs.Ref)

	case resolve.KindRegistry:
		m.Constraint = c.Version
		addr, err := registry.ParseAddress(c.Source)
		if err != nil {
			return errMod(m, err)
		}
		m.Subdir = addr.Subdir
		res, err := registry.Resolve(addr, c.Version)
		if err != nil {
			return errMod(m, err)
		}
		m.Version = res.Version
		if resolve.Classify(res.Source) != resolve.KindGit {
			m.Status = "skipped"
			m.Note = "registry download is not git-backed; content hashing is on the roadmap"
			return m
		}
		gs, err := resolve.ParseGit(res.Source)
		if err != nil {
			return errMod(m, err)
		}
		m.CloneURL = gs.CloneURL
		if gs.Subdir != "" {
			m.Subdir = gs.Subdir
		}
		return pinGit(m, gs.CloneURL, gs.Ref)

	case resolve.KindLocal:
		m.Status = "skipped"
		m.Note = "local module; versioned with the root module"
		return m

	default:
		m.Constraint = c.Version
		m.Status = "skipped"
		m.Note = fmt.Sprintf("%s sources are not lockable yet (see roadmap)", kind)
		return m
	}
}

func pinGit(m lockfile.Module, cloneURL, ref string) lockfile.Module {
	sha, err := resolve.GitCommit(cloneURL, ref)
	if err != nil {
		return errMod(m, err)
	}
	m.ResolvedCommit = sha
	m.Digest = "git:sha1:" + sha
	m.Status = "locked"
	return m
}

func errMod(m lockfile.Module, err error) lockfile.Module {
	m.Status = "error"
	m.Note = err.Error()
	return m
}
