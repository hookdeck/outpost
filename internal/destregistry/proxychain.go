package destregistry

import (
	"fmt"
	"net/url"
	"strings"
)

// ParseProxyURL parses the proxy config value: one or more forward proxy
// URLs separated by whitespace, nearest hop first. An unencoded space is never valid inside a URL, so a
// single-proxy value parses as a one-element chain unchanged. Empty or
// whitespace-only input yields nil (no proxy).
//
// Every hop must be an absolute http or https URL with a host. Errors name
// the offending hop by index and never echo the value, which may carry
// credentials.
func ParseProxyURL(s string) ([]*url.URL, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil, nil
	}
	hops := make([]*url.URL, 0, len(fields))
	for i, raw := range fields {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("proxy hop %d: invalid URL", i)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("proxy hop %d: scheme must be http or https", i)
		}
		if u.Host == "" || u.Hostname() == "" {
			return nil, fmt.Errorf("proxy hop %d: missing host", i)
		}
		hops = append(hops, u)
	}
	return hops, nil
}

// RedactedProxyURL renders a proxy hop as scheme://host[:port] for logs and
// error messages, dropping userinfo, path and query.
func RedactedProxyURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
