// Package resolve classifies module sources and resolves git sources to a
// concrete commit SHA (which is itself a content hash) without downloading
// the module tree.
package resolve

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Conalh/tofulock/internal/util"
)

// gitTimeout bounds one ls-remote call so a stalled remote fails the run
// instead of hanging CI until the job-level timeout.
const gitTimeout = 60 * time.Second

// Kind is the category of a module source address.
type Kind string

const (
	KindGit      Kind = "git"
	KindRegistry Kind = "registry"
	KindLocal    Kind = "local"
	KindArchive  Kind = "archive"
	KindOther    Kind = "other"
)

// Classify maps a module `source` string to a Kind, following Terraform's
// source-address conventions (forced getters, detector shorthands, registry
// addresses, and local paths).
func Classify(source string) Kind {
	s := strings.TrimSpace(source)
	switch {
	case strings.HasPrefix(s, "./"), strings.HasPrefix(s, "../"),
		strings.HasPrefix(s, ".\\"), strings.HasPrefix(s, "..\\"):
		return KindLocal
	case filepath.IsAbs(s):
		return KindLocal
	}
	if i := strings.Index(s, "::"); i >= 0 {
		switch s[:i] {
		case "git":
			return KindGit
		case "s3", "gcs", "http", "https":
			return KindArchive
		default:
			return KindOther
		}
	}
	if strings.HasPrefix(s, "git@") || strings.Contains(s, ".git") {
		return KindGit
	}
	for _, host := range []string{"github.com/", "bitbucket.org/", "gitlab.com/"} {
		if strings.HasPrefix(s, host) {
			return KindGit
		}
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return KindArchive
	}
	if looksLikeRegistry(s) {
		return KindRegistry
	}
	return KindOther
}

// looksLikeRegistry reports whether s matches the Terraform Registry address
// shape: [<host>/]<namespace>/<name>/<provider>, optionally with a //subdir.
func looksLikeRegistry(s string) bool {
	if strings.Contains(s, "://") || strings.Contains(s, "::") {
		return false
	}
	if i := strings.Index(s, "//"); i >= 0 {
		s = s[:i]
	}
	s = strings.Trim(s, "/")
	parts := strings.Split(s, "/")
	if len(parts) != 3 && len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
	}
	return true
}

// GitSource is a git module source decomposed into its clone URL, optional
// repository subdirectory, and ref (tag/branch/commit) from the ?ref= query.
type GitSource struct {
	CloneURL string
	Subdir   string
	Ref      string
}

// ParseGit decomposes a git module source string. It strips a leading
// "git::" forced getter, extracts the ?ref= query, splits the "//subdir"
// component, and normalizes detector shorthands (github.com/...) to https.
func ParseGit(source string) (GitSource, error) {
	s := strings.TrimSpace(source)
	if s == "" {
		return GitSource{}, fmt.Errorf("empty git source")
	}
	s = strings.TrimPrefix(s, "git::")

	var ref string
	if i := strings.LastIndex(s, "?"); i >= 0 {
		ref = util.QueryGet(s[i+1:], "ref")
		s = s[:i]
	}

	base, sub := splitSubdir(s)

	for _, host := range []string{"github.com/", "bitbucket.org/", "gitlab.com/"} {
		if strings.HasPrefix(base, host) {
			base = "https://" + base
			break
		}
	}
	if base == "" {
		return GitSource{}, fmt.Errorf("could not parse git source %q", source)
	}
	if err := validateCloneURL(base); err != nil {
		return GitSource{}, err
	}
	return GitSource{CloneURL: base, Subdir: sub, Ref: ref}, nil
}

// SanitizeSource removes HTTP(S) userinfo and SSH URL passwords before a module
// source is copied into a lockfile, error report, or attestation. Credentials
// belong in a credential helper or SSH agent, never in provenance artifacts.
func SanitizeSource(source string) string {
	s := strings.TrimSpace(source)
	prefix := ""
	if strings.HasPrefix(s, "git::") {
		prefix = "git::"
		s = strings.TrimPrefix(s, prefix)
	}
	u, err := url.Parse(s)
	if err != nil || u.User == nil {
		return source
	}
	switch u.Scheme {
	case "http", "https":
		u.User = nil
	case "ssh":
		if _, hasPassword := u.User.Password(); !hasPassword {
			return source
		}
		u.User = url.User(u.User.Username())
	default:
		return source
	}
	return prefix + u.String()
}

// validateCloneURL limits Git resolution to authenticated HTTPS and SSH
// transports. In particular, it rejects option-shaped repositories and Git's
// ext/file helpers before an untrusted Terraform source reaches `git`.
func validateCloneURL(cloneURL string) error {
	if cloneURL == "" || strings.HasPrefix(cloneURL, "-") || strings.ContainsAny(cloneURL, "\x00\r\n") {
		return fmt.Errorf("unsafe git clone URL")
	}
	if isSCPStyleSSH(cloneURL) {
		return nil
	}
	u, err := url.Parse(cloneURL)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("git clone URL must use HTTPS or SSH")
	}
	switch u.Scheme {
	case "https":
		if u.User != nil {
			return fmt.Errorf("git HTTPS credentials must use a credential helper, not URL userinfo")
		}
	case "ssh":
		if u.User != nil {
			if _, hasPassword := u.User.Password(); hasPassword || !isSafeSSHIdentifier(u.User.Username()) {
				return fmt.Errorf("git SSH URLs must use a safe username and an SSH agent, not a password")
			}
		}
	default:
		return fmt.Errorf("git clone URL must use HTTPS or SSH")
	}
	return nil
}

func isSCPStyleSSH(s string) bool {
	at := strings.IndexByte(s, '@')
	if at <= 0 || strings.ContainsAny(s, "\t\r\n ") {
		return false
	}
	colonOffset := strings.IndexByte(s[at+1:], ':')
	if colonOffset <= 0 {
		return false
	}
	colon := at + 1 + colonOffset
	return colon+1 < len(s) &&
		isSafeSSHIdentifier(s[:at]) &&
		isSafeSSHHost(s[at+1:colon])
}

func isSafeSSHIdentifier(s string) bool {
	return s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-", r)
	}) == -1
}

func isSafeSSHHost(s string) bool {
	return isSafeSSHIdentifier(s) && !strings.HasPrefix(s, "-")
}

// splitSubdir separates a "//subdir" suffix from a URL, ignoring the "://"
// scheme separator.
func splitSubdir(s string) (base, sub string) {
	scheme := ""
	if i := strings.Index(s, "://"); i >= 0 {
		scheme, s = s[:i+3], s[i+3:]
	}
	if j := strings.Index(s, "//"); j >= 0 {
		return scheme + s[:j], strings.Trim(s[j+2:], "/")
	}
	return scheme + s, ""
}

// GitCommit resolves ref against the remote and returns the full commit SHA,
// using `git ls-remote` so no module content is downloaded. An empty ref
// resolves the remote HEAD; a 40-hex ref is treated as already pinned.
func GitCommit(cloneURL, ref string) (string, error) {
	if err := validateCloneURL(cloneURL); err != nil {
		return "", err
	}
	if isHex40(ref) {
		return strings.ToLower(ref), nil
	}
	query := ref
	if query == "" {
		query = "HEAD"
	}
	args := []string{"ls-remote", "--", cloneURL,
		query,
		"refs/tags/" + ref,
		"refs/heads/" + ref,
		query + "^{}",
		"refs/tags/" + ref + "^{}",
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("git ls-remote %s: timed out after %s", cloneURL, gitTimeout)
	}
	if err != nil {
		return "", fmt.Errorf("git ls-remote %s: %w", cloneURL, gitErr(err))
	}

	refs := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 2 {
			refs[fields[1]] = fields[0]
		}
	}
	if len(refs) == 0 {
		return "", fmt.Errorf("ref %q not found in %s", ref, cloneURL)
	}
	// Resolve in a fixed precedence order so the same ref always pins to the
	// same commit across runs (map iteration is random, and a peeled annotated
	// tag like refs/tags/v1^{} must win over its annotated-tag object, but only
	// for *this* ref — never a different tag that also happened to be returned).
	peeledTag := "refs/tags/" + ref + "^{}"
	for _, key := range []string{
		peeledTag,
		"refs/tags/" + ref,
		"refs/heads/" + ref,
		query,
		query + "^{}",
		"HEAD",
	} {
		if sha, ok := refs[key]; ok {
			return sha, nil
		}
	}
	// Last resort: a single unpeeled ref we didn't name explicitly. Deterministic
	// because there's exactly one entry.
	if len(refs) == 1 {
		for _, sha := range refs {
			return sha, nil
		}
	}
	return "", fmt.Errorf("ref %q not found in %s", ref, cloneURL)
}

func isHex40(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

func gitErr(err error) error {
	if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
		return fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
	}
	return err
}
