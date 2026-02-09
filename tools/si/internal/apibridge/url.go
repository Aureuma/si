package apibridge

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

func ResolveURL(baseURL string, path string, params map[string]string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("request path is required")
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		u, err := url.Parse(path)
		if err != nil {
			return "", err
		}
		addQuery(u, params)
		return u.String(), nil
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	rel, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	u := base.ResolveReference(rel)
	addQuery(u, params)
	return u.String(), nil
}

// JoinURL appends path segments to baseURL without discarding any existing base path.
//
// This differs from ResolveURL's RFC3986 ResolveReference semantics when the input path
// begins with "/": JoinURL treats it as a path segment to append. This is useful for
// APIs that use base paths like "/upload" where callers want "/upload/<resource>".
func JoinURL(baseURL string, path string, params map[string]string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("request path is required")
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return ResolveURL(baseURL, path, params)
	}
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimLeft(path, "/")
	base.Path = strings.TrimRight(base.Path, "/") + "/" + trimmed
	addQuery(base, params)
	return base.String(), nil
}

func StripQuery(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.RawQuery = ""
	return u.String()
}

func addQuery(u *url.URL, params map[string]string) {
	if u == nil || len(params) == 0 {
		return
	}
	q := u.Query()
	keys := make([]string, 0, len(params))
	for key := range params {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		q.Set(key, strings.TrimSpace(params[key]))
	}
	u.RawQuery = q.Encode()
}
