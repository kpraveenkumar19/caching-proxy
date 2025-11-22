package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
)

// Entry represents a cached HTTP response.
type Entry struct {
	Status int
	Header http.Header
	Body   []byte
}

// Cache defines the interface for a response cache.
type Cache interface {
	Get(key string) (*Entry, bool, error)
	Set(key string, e *Entry) error
	Delete(key string) error
	Clear() (int, error) // returns number of entries removed
}

// BuildCacheKey returns a deterministic key for a request based on method, full URL, and Accept.
// Avoid caching when Authorization header is present (handled by caller, not here).
func BuildCacheKey(originBase string, r *http.Request) (string, error) {
	base, err := url.Parse(originBase)
	if err != nil {
		return "", err
	}
	// Construct full URL as seen by origin
	u := *base
	u.Path = singleJoiningSlash(base.Path, r.URL.Path)
	u.RawQuery = r.URL.RawQuery

	accept := r.Header.Get("Accept")
	keyMaterial := strings.Join([]string{r.Method, u.String(), accept}, "\n")
	h := sha256.Sum256([]byte(keyMaterial))
	return hex.EncodeToString(h[:]), nil
}

// ShardPath returns a safe relative file path to store the key's payload on disk.
// Example: ab/cd/abcdef... where first two bytes form first dir, next two for second, etc.
func ShardPath(cacheDir, key string) string {
	parts := []string{}
	if len(key) >= 2 {
		parts = append(parts, key[0:2])
	}
	if len(key) >= 4 {
		parts = append(parts, key[2:4])
	}
	return filepath.Join(append([]string{cacheDir}, append(parts, key)...)...)
}

// singleJoiningSlash mirrors net/http/httputil path join behavior.
func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bs := strings.HasPrefix(b, "/")
	switch {
	case aslash && bs:
		return a + b[1:]
	case !aslash && !bs:
		return a + "/" + b
	}
	return a + b
}

// CloneHeaders returns a deep copy of headers with sorted keys (for deterministic storage if needed).
func CloneHeaders(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for k, vv := range h {
		cl := make([]string, len(vv))
		copy(cl, vv)
		// Normalize order for determinism
		sort.Strings(cl)
		out[k] = cl
	}
	return out
}
