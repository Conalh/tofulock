// Package registry resolves Terraform/OpenTofu module-registry addresses to a
// concrete version and download source via the Module Registry Protocol.
package registry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/go-version"
)

// DefaultHost is the registry used when a module address omits an explicit
// host. Terraform and OpenTofu ship different defaults (registry.terraform.io
// vs registry.opentofu.org); the CLI overrides this to match the tool in use.
var DefaultHost = "registry.terraform.io"

const userAgent = "tofulock"

var httpClient = &http.Client{Timeout: 30 * time.Second}

// Address is a parsed registry module address:
// [<host>/]<namespace>/<name>/<provider>[//<subdir>].
type Address struct {
	Host      string
	Namespace string
	Name      string
	Provider  string
	Subdir    string
}

func (a Address) modulePath() string {
	return a.Namespace + "/" + a.Name + "/" + a.Provider
}

// ParseAddress parses a registry module address.
func ParseAddress(source string) (Address, error) {
	s := strings.TrimSpace(source)
	var subdir string
	if i := strings.Index(s, "//"); i >= 0 {
		subdir = strings.Trim(s[i+2:], "/")
		s = s[:i]
	}
	s = strings.Trim(s, "/")
	parts := strings.Split(s, "/")
	addr := Address{Host: DefaultHost, Subdir: subdir}
	switch len(parts) {
	case 3:
		addr.Namespace, addr.Name, addr.Provider = parts[0], parts[1], parts[2]
	case 4:
		addr.Host, addr.Namespace, addr.Name, addr.Provider = parts[0], parts[1], parts[2], parts[3]
	default:
		return Address{}, fmt.Errorf("not a registry address: %q", source)
	}
	return addr, nil
}

// Resolution is the outcome of resolving a registry address + constraint.
type Resolution struct {
	Version string // the exact version selected
	Source  string // the X-Terraform-Get download source (often a git:: source)
}

// Resolve performs service discovery, selects the highest version satisfying
// constraint, and fetches that version's download source.
func Resolve(a Address, constraint string) (Resolution, error) {
	base, err := discover(a.Host)
	if err != nil {
		return Resolution{}, err
	}
	ver, err := pickVersion(base, a, constraint)
	if err != nil {
		return Resolution{}, err
	}
	src, err := downloadSource(base, a, ver)
	if err != nil {
		return Resolution{}, err
	}
	return Resolution{Version: ver, Source: src}, nil
}

// discover returns the modules.v1 base URL for host, falling back to the
// conventional /v1/modules/ path if service discovery is unavailable.
func discover(host string) (string, error) {
	fallback := "https://" + host + "/v1/modules/"
	resp, err := httpGet("https://" + host + "/.well-known/terraform.json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fallback, nil
	}
	var disc struct {
		Modules string `json:"modules.v1"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&disc); err != nil || disc.Modules == "" {
		return fallback, nil
	}
	return absBase(host, disc.Modules), nil
}

func absBase(host, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return strings.TrimRight(ref, "/") + "/"
	}
	return "https://" + host + "/" + strings.Trim(ref, "/") + "/"
}

func pickVersion(base string, a Address, constraint string) (string, error) {
	resp, err := httpGet(base + a.modulePath() + "/versions")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry %s: versions returned HTTP %d", a.Host, resp.StatusCode)
	}
	var data struct {
		Modules []struct {
			Versions []struct {
				Version string `json:"version"`
			} `json:"versions"`
		} `json:"modules"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("registry %s: decoding versions: %w", a.Host, err)
	}
	if len(data.Modules) == 0 {
		return "", fmt.Errorf("module %s not found in registry %s", a.modulePath(), a.Host)
	}

	var cons version.Constraints
	if c := strings.TrimSpace(constraint); c != "" {
		cons, err = version.NewConstraint(c)
		if err != nil {
			return "", fmt.Errorf("invalid version constraint %q: %w", constraint, err)
		}
	}

	var best *version.Version
	for _, v := range data.Modules[0].Versions {
		ver, err := version.NewVersion(v.Version)
		if err != nil {
			continue
		}
		if cons != nil {
			// go-version's constraint check already filters prereleases unless
			// the constraint itself references one (see its prereleaseCheck), so
			// an exact pin like "6.6.1-rc1" or a constraint like ">= 6.6.1-rc1"
			// can match a prerelease, while "~> 5.0" still selects only stable
			// versions. We must not second-guess that here.
			if !cons.Check(ver) {
				continue
			}
		} else if ver.Prerelease() != "" {
			// No constraint: default to the highest stable version, matching
			// Terraform/OpenTofu's default resolution behavior.
			continue
		}
		if best == nil || ver.GreaterThan(best) {
			best = ver
		}
	}
	if best == nil {
		return "", fmt.Errorf("no version of %s satisfies %q", a.modulePath(), constraint)
	}
	return best.Original(), nil
}

func downloadSource(base string, a Address, ver string) (string, error) {
	resp, err := httpGet(base + a.modulePath() + "/" + ver + "/download")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if loc := resp.Header.Get("X-Terraform-Get"); loc != "" {
		return resolveGet(a.Host, loc), nil
	}
	// Modern registries (e.g. registry.opentofu.org) return HTTP 200 with the
	// source in a JSON body instead of the X-Terraform-Get header.
	if resp.StatusCode == http.StatusOK {
		var body struct {
			Location string `json:"location"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err == nil && body.Location != "" {
			return resolveGet(a.Host, body.Location), nil
		}
	}
	return "", fmt.Errorf("registry %s: no download source for %s %s (HTTP %d)",
		a.Host, a.modulePath(), ver, resp.StatusCode)
}

// resolveGet turns a (possibly relative) X-Terraform-Get value into an
// absolute source string.
func resolveGet(host, loc string) string {
	if strings.Contains(loc, "://") || strings.Contains(loc, "::") {
		return loc
	}
	if strings.HasPrefix(loc, "/") {
		return "https://" + host + loc
	}
	return loc
}

func httpGet(url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	return httpClient.Do(req)
}
