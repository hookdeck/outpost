// Package rediskey escapes client-supplied IDs for use in Redis keys.
package rediskey

import "strings"

var escaper = strings.NewReplacer("%", "%25", ":", "%3A", "{", "%7B", "}", "%7D")

// Escape percent-encodes the characters of id that have a meaning in a key:
// ":" separates segments, "{...}" is a hash tag, and "%" is the escape
// character. An ID without them is returned unchanged.
func Escape(id string) string {
	if !strings.ContainsAny(id, "%:{}") {
		return id
	}
	return escaper.Replace(id)
}

// IDs escapes each ID and joins them with ":". Different ID lists of the same
// length never produce the same result.
func IDs(ids ...string) string {
	escaped := make([]string, len(ids))
	for i, id := range ids {
		escaped[i] = Escape(id)
	}
	return strings.Join(escaped, ":")
}

// Tag escapes id and wraps it in "{...}", the hash tag that keeps every key
// carrying it on the same Redis Cluster slot.
func Tag(id string) string {
	return "{" + Escape(id) + "}"
}
