// Package util holds tiny shared helpers that don't belong to any single
// resolver package. Kept deliberately small so import graphs stay flat.
package util

import "strings"

// QueryGet parses a URL-style query string ("key=value&key2=value2") and
// returns the value for the first occurrence of key, or "" if key is absent.
// It avoids pulling in net/url for the small subset of source-address syntax
// tofulock needs (module ?ref=... and tfr://...?version=...).
func QueryGet(query, key string) string {
	for _, kv := range strings.Split(query, "&") {
		if i := strings.Index(kv, "="); i >= 0 && kv[:i] == key {
			return kv[i+1:]
		}
	}
	return ""
}
