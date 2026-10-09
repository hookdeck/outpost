// Package mcpevents holds the MCP Events domain logic shared by the mcp
// destination provider, the /mcp HTTP endpoints and the background worker:
// subscription identity, callback URL normalization, params parsing, TTL
// grants, mcp_error shapes, Standard Webhooks signing, envelopes, callback
// verification, terminated notifications and asynchronous operator events.
//
// It has no HTTP framework dependency; outbound requests go through an
// injected *http.Client (the SSRF-guarded client in production).
package mcpevents
