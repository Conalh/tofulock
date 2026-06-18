package registry

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseAddress(t *testing.T) {
	cases := []struct {
		in                           string
		host, ns, name, prov, subdir string
		wantErr                      bool
	}{
		{"terraform-aws-modules/vpc/aws", DefaultHost, "terraform-aws-modules", "vpc", "aws", "", false},
		{"app.terraform.io/corp/vpc/aws", "app.terraform.io", "corp", "vpc", "aws", "", false},
		{"terraform-aws-modules/iam/aws//modules/iam-role", DefaultHost, "terraform-aws-modules", "iam", "aws", "modules/iam-role", false},
		{"too/few", "", "", "", "", "", true},
		{"way/too/many/parts/here", "", "", "", "", "", true},
	}
	for _, c := range cases {
		a, err := ParseAddress(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseAddress(%q) expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseAddress(%q): %v", c.in, err)
			continue
		}
		if a.Host != c.host || a.Namespace != c.ns || a.Name != c.name || a.Provider != c.prov || a.Subdir != c.subdir {
			t.Errorf("ParseAddress(%q) = %+v", c.in, a)
		}
	}
}

func TestPickVersionAndDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/modules/ns/name/aws/versions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"modules":[{"versions":[
				{"version":"5.7.0"},{"version":"5.8.1"},{"version":"6.0.0-rc1"},{"version":"4.2.0"}
			]}]}`))
		case "/v1/modules/ns/name/aws/5.8.1/download":
			w.Header().Set("X-Terraform-Get", "git::https://github.com/ns/name.git?ref=v5.8.1")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	base := srv.URL + "/v1/modules/"
	addr := Address{Host: "example", Namespace: "ns", Name: "name", Provider: "aws"}

	// ~> 5.0 should select 5.8.1 (highest 5.x, prereleases skipped).
	ver, err := pickVersion(base, addr, "~> 5.0")
	if err != nil {
		t.Fatalf("pickVersion: %v", err)
	}
	if ver != "5.8.1" {
		t.Fatalf("pickVersion = %q, want 5.8.1", ver)
	}

	src, err := downloadSource(base, addr, ver)
	if err != nil {
		t.Fatalf("downloadSource: %v", err)
	}
	if src != "git::https://github.com/ns/name.git?ref=v5.8.1" {
		t.Fatalf("downloadSource = %q", src)
	}
}

func TestDownloadSourceJSONBody(t *testing.T) {
	// registry.opentofu.org returns the source in a JSON body, not the
	// X-Terraform-Get header.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"location":"git::https://github.com/ns/name?ref=3ffbd46fb1c7733e1b34d8666893280454e27436"}`))
	}))
	defer srv.Close()
	addr := Address{Host: "example", Namespace: "ns", Name: "name", Provider: "aws"}
	src, err := downloadSource(srv.URL+"/", addr, "6.6.1")
	if err != nil {
		t.Fatalf("downloadSource: %v", err)
	}
	if src != "git::https://github.com/ns/name?ref=3ffbd46fb1c7733e1b34d8666893280454e27436" {
		t.Fatalf("downloadSource = %q", src)
	}
}

func TestPickVersionNoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"1.0.0"}]}]}`))
	}))
	defer srv.Close()
	addr := Address{Host: "example", Namespace: "ns", Name: "name", Provider: "aws"}
	if _, err := pickVersion(srv.URL+"/", addr, ">= 5.0"); err == nil {
		t.Error("expected no-match error for >= 5.0 against 1.0.0")
	}
}

// TestPickVersionPrerelease pins the rule that a prerelease is selectable only
// when the constraint itself references one. An exact pin or a range that
// mentions a prerelease may match; a plain stable-only constraint must not.
func TestPickVersionPrerelease(t *testing.T) {
	versions := `{"modules":[{"versions":[
		{"version":"5.8.1"},{"version":"6.6.1-rc1"},{"version":"6.6.1"}
	]}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(versions))
	}))
	defer srv.Close()
	addr := Address{Host: "example", Namespace: "ns", Name: "name", Provider: "aws"}

	// Exact prerelease pin selects the prerelease.
	if v, err := pickVersion(srv.URL+"/", addr, "6.6.1-rc1"); err != nil || v != "6.6.1-rc1" {
		t.Errorf("exact prerelease pin = %q, %v; want 6.6.1-rc1", v, err)
	}
	// A stable-only constraint (~> 5.0) ignores the prerelease and picks 5.8.1.
	if v, err := pickVersion(srv.URL+"/", addr, "~> 5.0"); err != nil || v != "5.8.1" {
		t.Errorf("~> 5.0 = %q, %v; want 5.8.1 (prerelease skipped)", v, err)
	}
	// No constraint defaults to the highest stable version (6.6.1, not -rc1).
	if v, err := pickVersion(srv.URL+"/", addr, ""); err != nil || v != "6.6.1" {
		t.Errorf("no constraint = %q, %v; want 6.6.1 (latest stable)", v, err)
	}
}
