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
	HTTPClient *http.Client // nil = 30 s timeout
}

// New returns the MCP endpoint handler.
func New(cfg Config) http.Handler {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &server{cfg: cfg}
}

type server struct{ cfg Config }

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.serve(w, r) }

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
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if req.Method == "initialize" {
		rpcResult(w, req.ID, `{"protocolVersion":"2026-07-28","serverInfo":{"name":"endpoints","version":"1"},
			"capabilities":{"events":{},"extensions":{"io.modelcontextprotocol/events":{}}}}`)
		return
	}

	principal := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	user, known := s.cfg.Principals[principal]
	if !known {
		rpcFail(w, http.StatusOK, req.ID, -32024, "Forbidden", nil)
		return
	}

	switch req.Method {
	case "events/list":
		query := ""
		if len(user.AllowedTopics) > 0 {
			query = "?topics=" + url.QueryEscape(strings.Join(user.AllowedTopics, ","))
		}
		status, body := s.outpost(http.MethodGet, "/mcp/events"+query, nil)
		forward(w, req.ID, status, body)
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
		status, out := s.outpost(http.MethodPost, "/tenants/"+user.Tenant+"/mcp/"+action, body)
		forward(w, req.ID, status, out)
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
	req, _ := http.NewRequest(method, s.cfg.OutpostURL+path, in)
	req.Header.Set("Authorization", "Bearer "+s.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		return 0, []byte(err.Error())
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// forward answers with Outpost's result, or rethrows its mcp_error.
func forward(w http.ResponseWriter, id json.RawMessage, status int, body []byte) {
	var failure struct {
		Error *json.RawMessage `json:"mcp_error"`
	}
	switch {
	case status == http.StatusOK:
		rpcResult(w, id, string(body))
	case json.Unmarshal(body, &failure) == nil && failure.Error != nil:
		writeRPC(w, http.StatusOK, fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"error":%s}`, id, *failure.Error))
	default:
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
