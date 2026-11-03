// Package mcpserver is a reference MCP Events server in front of Outpost's
// /mcp/* REST routes.
package mcpserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Principal is the identity behind a bearer token.
type Principal struct {
	Tenant        string
	AllowedTopics []string
}

type Config struct {
	OutpostURL string // API base including /api/v1
	APIKey     string // admin key
	Principals map[string]Principal
	// DefaultTenant makes any other non-empty bearer token a principal of
	// this tenant. Empty = unknown tokens are Forbidden.
	DefaultTenant string
	HTTPClient    *http.Client // nil = 40 s timeout
	// Header is set on every Outpost request after Authorization and
	// Content-Type, so it can replace them.
	Header http.Header
	// Logf receives one line per request.
	Logf func(format string, args ...any)
}

// New returns the MCP endpoint handler.
func New(cfg Config) http.Handler {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 40 * time.Second}
	}
	return &server{cfg: cfg}
}

type server struct{ cfg Config }

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Logf == nil {
		s.serve(w, r)
		return
	}
	rec := &recorder{ResponseWriter: w}
	s.serve(rec, r)
	s.cfg.Logf("%s %s -> %s", rec.method, mask(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")), rec.outcome())
}

type recorder struct {
	http.ResponseWriter
	method string
	status int
	body   []byte
}

func (r *recorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return r.ResponseWriter.Write(b)
}

func (r *recorder) outcome() string {
	var m struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(r.body, &m) == nil && m.Error != nil {
		return fmt.Sprintf("error %d %s", m.Error.Code, m.Error.Message)
	}
	if r.status == 0 || r.status == http.StatusOK || r.status == http.StatusAccepted {
		return "ok"
	}
	return fmt.Sprintf("http %d", r.status)
}

func (s *server) principal(token string) (Principal, bool) {
	if p, ok := s.cfg.Principals[token]; ok {
		return p, true
	}
	if token != "" && s.cfg.DefaultTenant != "" {
		return Principal{Tenant: s.cfg.DefaultTenant}, true
	}
	return Principal{}, false
}

type rpcRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (s *server) serve(w http.ResponseWriter, r *http.Request) {
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		rpcFail(w, http.StatusBadRequest, nil, -32700, "ParseError", nil)
		return
	}
	if rec, ok := w.(*recorder); ok {
		rec.method = req.Method
	}
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if req.Method == "initialize" {
		rpcResult(w, req.ID, `{"protocolVersion":"2026-07-28","serverInfo":{"name":"endpoints","version":"1"},
			"capabilities":{"extensions":{"io.modelcontextprotocol/events":{}}}}`)
		return
	}

	if req.Method == "server/discover" {
		rpcResult(w, req.ID, `{"resultType":"complete","ttlMs":0,"cacheScope":"public","supportedVersions":["2026-07-28"],
			"capabilities":{"extensions":{"io.modelcontextprotocol/events":{}}}}`)
		return
	}

	principal := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	user, known := s.principal(principal)
	if !known {
		rpcFail(w, http.StatusOK, req.ID, -32024, "Forbidden", nil)
		return
	}

	switch req.Method {
	case "events/list":
		var params struct {
			Cursor string `json:"cursor"`
		}
		_ = json.Unmarshal(req.Params, &params)
		q := url.Values{}
		if len(user.AllowedTopics) > 0 {
			q.Set("topics", strings.Join(user.AllowedTopics, ","))
		}
		if params.Cursor != "" {
			q.Set("cursor", params.Cursor)
		}
		query := ""
		if len(q) > 0 {
			query = "?" + q.Encode()
		}
		path := "/mcp/events" + query
		status, body := s.outpost(http.MethodGet, path, nil)
		s.forward(w, req.ID, http.MethodGet, path, status, body)
	case "events/subscribe", "events/unsubscribe":
		var params struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(req.Params, &params)
		if r.Header.Get("Mcp-Method") != req.Method || r.Header.Get("Mcp-Name") != params.Name {
			rpcFail(w, http.StatusBadRequest, req.ID, -32020, "HeaderMismatch", nil)
			return
		}
		action := strings.TrimPrefix(req.Method, "events/")
		body := map[string]any{"principal": principal, "params": req.Params}
		if action == "subscribe" && len(user.AllowedTopics) > 0 {
			body["allowed_topics"] = user.AllowedTopics
		}
		path := "/tenants/" + user.Tenant + "/mcp/" + action
		status, out := s.outpost(http.MethodPost, path, body)
		s.forward(w, req.ID, http.MethodPost, path, status, out)
	default:
		rpcFail(w, http.StatusOK, req.ID, -32601, "MethodNotFound", nil)
	}
}

func (s *server) outpost(method, path string, body any) (int, []byte) {
	var in io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		in = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, s.cfg.OutpostURL+path, in)
	if err != nil {
		s.logf("outpost %s %s -> %v", method, path, err)
		return 0, []byte(err.Error())
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	for k, vs := range s.cfg.Header {
		req.Header.Del(k)
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		s.logf("outpost %s %s -> %v", method, path, err)
		return 0, []byte(err.Error())
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func (s *server) logf(format string, args ...any) {
	if s.cfg.Logf != nil {
		s.cfg.Logf(format, args...)
	}
}

func clip(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "..."
	}
	return string(b)
}

// forward answers with Outpost's result, or rethrows its mcp_error.
func (s *server) forward(w http.ResponseWriter, id json.RawMessage, method, path string, status int, body []byte) {
	var failure struct {
		Error *json.RawMessage `json:"mcp_error"`
	}
	switch {
	case status == http.StatusOK:
		rpcResult(w, id, string(body))
	case json.Unmarshal(body, &failure) == nil && failure.Error != nil:
		writeRPC(w, http.StatusOK, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":%s}`, id, *failure.Error))
	default:
		if status != 0 {
			s.logf("outpost %s %s -> http %d: %s", method, path, status, clip(body, 512))
		}
		rpcFail(w, http.StatusOK, id, -32603, "InternalError", nil)
	}
}

func rpcResult(w http.ResponseWriter, id json.RawMessage, result string) {
	writeRPC(w, http.StatusOK, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, id, result))
}

func rpcFail(w http.ResponseWriter, status int, id json.RawMessage, code int, message string, data map[string]any) {
	if id == nil {
		id = json.RawMessage("null")
	}
	obj := map[string]any{"code": code, "message": message}
	if data != nil {
		obj["data"] = data
	}
	e, _ := json.Marshal(obj)
	writeRPC(w, status, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":%s}`, id, e))
}

func writeRPC(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// mask keeps the start of a bearer token so log lines tell principals apart
// without exposing the credential.
func mask(token string) string {
	if token == "" {
		return "-"
	}
	return token[:min(4, len(token)/2)] + "..."
}
