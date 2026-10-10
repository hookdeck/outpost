package mcpevents

import (
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

// MaxCallbackURLBytes caps callback URLs, before and after normalization.
const MaxCallbackURLBytes = 2048

// FieldDeliveryURL is the invalid_params field for callback URL failures.
const FieldDeliveryURL = "delivery.url"

// NormalizeCallbackURL parses an absolute http or https callback URL and
// returns it in normalized form, both parsed and serialized (u.String() equals
// the string). Subscription IDs, the verification cache and the verification
// rate limits are keyed on the string, so every spelling of one endpoint must
// normalize the same way:
//   - scheme lowercased; only http and https (https-only is the address
//     guard's decision, since allowlisted loopback may use http);
//   - host: one trailing dot removed, IDNA ToASCII (UTS #46 lookup profile:
//     lowercase, NFC, STD3 rules); a host that ends in a numeric label must
//     be a dotted-decimal IPv4 address; IP literals in netip's canonical
//     form, IPv4-mapped IPv6 unmapped, IPv6 bracketed, zones rejected;
//   - port: decimal, 1..65535, leading zeros dropped, the scheme's default
//     port removed;
//   - path: dot segments removed and characters outside the WHATWG path set
//     percent-encoded (plus "|"), as WHATWG URL does; empty path is "/";
//   - query: kept, with the WHATWG special-query set percent-encoded;
//   - userinfo, fragments, backslashes, whitespace and control characters are
//     rejected; so is anything over MaxCallbackURLBytes.
//
// Failures are invalid_params {field: "delivery.url", reason: invalid_url |
// https_required}.
func NormalizeCallbackURL(raw string) (*url.URL, string, error) {
	if raw == "" || len(raw) > MaxCallbackURLBytes {
		return nil, "", InvalidParams(FieldDeliveryURL, ReasonInvalidURL)
	}
	for i := 0; i < len(raw); i++ {
		// Whitespace and controls (WHATWG strips or drops some, other
		// parsers keep or reject them) and backslashes (a path separator
		// for WHATWG, not for net/url) are parser differentials; '#'
		// starts a fragment, which a callback URL can't use.
		if c := raw[i]; c <= ' ' || c == 0x7f || c == '\\' || c == '#' {
			return nil, "", InvalidParams(FieldDeliveryURL, ReasonInvalidURL)
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, "", InvalidParams(FieldDeliveryURL, ReasonInvalidURL)
	}
	switch u.Scheme {
	case "http", "https":
	case "":
		return nil, "", InvalidParams(FieldDeliveryURL, ReasonInvalidURL)
	default:
		return nil, "", InvalidParams(FieldDeliveryURL, ReasonHTTPSRequired)
	}
	if u.Opaque != "" || u.Host == "" || u.User != nil {
		return nil, "", InvalidParams(FieldDeliveryURL, ReasonInvalidURL)
	}

	host, ok := normalizeHost(u)
	if !ok {
		return nil, "", InvalidParams(FieldDeliveryURL, ReasonInvalidURL)
	}

	var b strings.Builder
	b.Grow(len(raw) + 16)
	b.WriteString(u.Scheme)
	b.WriteString("://")
	b.WriteString(host)
	if p := u.Port(); p != "" {
		port, err := strconv.ParseUint(p, 10, 16)
		if err != nil || port == 0 {
			return nil, "", InvalidParams(FieldDeliveryURL, ReasonInvalidURL)
		}
		if port != defaultPort(u.Scheme) {
			b.WriteByte(':')
			b.WriteString(strconv.FormatUint(port, 10))
		}
	}
	// The path as written: net/url keeps it in RawPath unless re-escaping
	// the decoded Path reproduces it exactly.
	rawPath := u.RawPath
	if rawPath == "" {
		rawPath = u.EscapedPath()
	}
	writePath(&b, rawPath)
	if u.ForceQuery || u.RawQuery != "" {
		b.WriteByte('?')
		percentEncode(&b, u.RawQuery, inQuerySet)
	}
	s := b.String()
	if len(s) > MaxCallbackURLBytes {
		return nil, "", InvalidParams(FieldDeliveryURL, ReasonInvalidURL)
	}
	out, err := url.Parse(s)
	if err != nil || out.String() != s {
		// The serialization above is built to round-trip; never hand out a
		// URL whose request target differs from its key.
		return nil, "", InvalidParams(FieldDeliveryURL, ReasonInvalidURL)
	}
	return out, s, nil
}

func defaultPort(scheme string) uint64 {
	if scheme == "https" {
		return 443
	}
	return 80
}

// HostPort returns u's host and port with the scheme default filled in, the
// form the per-host limiters key on ("example.com:443", "[::1]:8443").
func HostPort(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = strconv.FormatUint(defaultPort(u.Scheme), 10)
	}
	return net.JoinHostPort(u.Hostname(), port)
}

func normalizeHost(u *url.URL) (string, bool) {
	hostname := u.Hostname()
	if strings.HasPrefix(u.Host, "[") {
		addr, err := netip.ParseAddr(hostname)
		if err != nil || !addr.Is6() || addr.Zone() != "" {
			return "", false
		}
		addr = addr.Unmap()
		if addr.Is4() {
			return addr.String(), true
		}
		return "[" + addr.String() + "]", true
	}
	ascii, err := idna.Lookup.ToASCII(hostname)
	if err != nil {
		return "", false
	}
	// After mapping, so a full-width trailing dot counts too.
	ascii = strings.TrimSuffix(ascii, ".")
	if ascii == "" || len(ascii) > 253 {
		return "", false
	}
	if endsInNumber(ascii) {
		// WHATWG parses these as IPv4 in its lenient forms (127.1, 0x7f.1,
		// 2130706433, octal 010); accept only the canonical dotted quad.
		addr, err := netip.ParseAddr(ascii)
		if err != nil || !addr.Is4() {
			return "", false
		}
		return addr.String(), true
	}
	for label := range strings.SplitSeq(ascii, ".") {
		if label == "" || len(label) > 63 {
			return "", false
		}
	}
	return ascii, true
}

// endsInNumber reports whether the last label is all digits or 0x-hex, the
// WHATWG rule that sends a host to the IPv4 parser.
func endsInNumber(host string) bool {
	last := host[strings.LastIndexByte(host, '.')+1:]
	if last == "" {
		return false
	}
	if len(last) >= 2 && last[0] == '0' && (last[1] == 'x' || last[1] == 'X') {
		for i := 2; i < len(last); i++ {
			if !isHex(last[i]) {
				return false
			}
		}
		return true
	}
	for i := 0; i < len(last); i++ {
		if last[i] < '0' || last[i] > '9' {
			return false
		}
	}
	return true
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// writePath writes rawPath with dot segments resolved the WHATWG way ("." and
// ".." including their %2e spellings; a trailing one leaves a trailing slash)
// and percent-encoded.
func writePath(b *strings.Builder, rawPath string) {
	if rawPath == "" || rawPath == "/" {
		b.WriteByte('/')
		return
	}
	segments := strings.Split(strings.TrimPrefix(rawPath, "/"), "/")
	out := make([]string, 0, len(segments))
	for i, seg := range segments {
		last := i == len(segments)-1
		switch {
		case isDoubleDot(seg):
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
			if last {
				out = append(out, "")
			}
		case isSingleDot(seg):
			if last {
				out = append(out, "")
			}
		default:
			out = append(out, seg)
		}
	}
	for _, seg := range out {
		b.WriteByte('/')
		percentEncode(b, seg, inPathSet)
	}
}

func isSingleDot(s string) bool {
	return s == "." || strings.EqualFold(s, "%2e")
}

func isDoubleDot(s string) bool {
	switch strings.ToLower(s) {
	case "..", ".%2e", "%2e.", "%2e%2e":
		return true
	}
	return false
}

// inPathSet is the WHATWG path percent-encode set, plus "|", which net/url
// would otherwise re-escape (and with it decode %2F) when serializing.
func inPathSet(c byte) bool {
	switch c {
	case '"', '#', '<', '>', '?', '^', '`', '{', '}', '|':
		return true
	}
	return c <= ' ' || c >= 0x7f
}

// inQuerySet is the WHATWG special-query percent-encode set.
func inQuerySet(c byte) bool {
	switch c {
	case '"', '#', '<', '>', '\'':
		return true
	}
	return c <= ' ' || c >= 0x7f
}

func percentEncode(b *strings.Builder, s string, in func(byte) bool) {
	const upperHex = "0123456789ABCDEF"
	for i := 0; i < len(s); i++ {
		c := s[i]
		if in(c) {
			b.WriteByte('%')
			b.WriteByte(upperHex[c>>4])
			b.WriteByte(upperHex[c&0xF])
			continue
		}
		b.WriteByte(c)
	}
}
