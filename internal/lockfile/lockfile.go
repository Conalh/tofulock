// Package lockfile defines tofulock's deterministic module lockfile and its
// read/write helpers.
package lockfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// FileName is the lockfile written into a module directory.
const FileName = ".tofulock.lock.json"

// SchemaVersion is the lockfile schema version.
const SchemaVersion = 1

// File is the on-disk lockfile. Output is sorted and timestamp-free so the
// file is byte-stable across runs and produces clean diffs in code review.
type File struct {
	Version int      `json:"version"`
	Tool    string   `json:"tool"`
	Modules []Module `json:"modules"`
}

// Module is one locked (or deliberately skipped) module call.
type Module struct {
	Name           string `json:"name"`
	Source         string `json:"source"`
	Type           string `json:"type"` // git | registry | local | archive | other
	Constraint     string `json:"constraint,omitempty"`
	Version        string `json:"version,omitempty"` // resolved exact version (registry sources)
	CloneURL       string `json:"clone_url,omitempty"`
	Subdir         string `json:"subdir,omitempty"`
	ResolvedCommit string `json:"resolved_commit,omitempty"`
	Digest         string `json:"digest,omitempty"`
	Status         string `json:"status"` // locked | skipped | error
	Note           string `json:"note,omitempty"`
}

// Path returns the lockfile path inside dir.
func Path(dir string) string { return filepath.Join(dir, FileName) }

// Write serializes f into dir/FileName with stable ordering.
func Write(dir string, f *File) error {
	f.Version = SchemaVersion
	f.Tool = "tofulock"
	sort.Slice(f.Modules, func(i, j int) bool { return f.Modules[i].Name < f.Modules[j].Name })
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(dir), append(b, '\n'), 0o644)
}

// Read loads and parses dir/FileName.
func Read(dir string) (*File, error) {
	b, err := os.ReadFile(Path(dir))
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// ReadRaw loads dir/FileName and returns both the parsed file and the exact
// on-disk bytes. Callers that need a digest over the lockfile (e.g. attestation
// signing/verification) must use this instead of separate Read + ReadFile
// calls, which would race against a concurrent writer (TOCTOU) and could bind
// a signature to bytes that don't match the parsed modules.
func ReadRaw(dir string) (*File, []byte, error) {
	raw, err := os.ReadFile(Path(dir))
	if err != nil {
		return nil, nil, err
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, nil, err
	}
	return &f, raw, nil
}
