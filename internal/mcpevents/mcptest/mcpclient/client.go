package mcpclient

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const (
	DefaultProtocolVersion = "2026-07-28"

	HeaderSessionID       = "Mcp-Session-Id"
	HeaderMethod          = "Mcp-Method"
	HeaderName            = "Mcp-Name"
	HeaderProtocolVersion = "MCP-Protocol-Version"

	ExtensionID = "io.modelcontextprotocol/events"
)

var TTLNull = json.RawMessage("null")

func NewSecret() string {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return "whsec_" + base64.StdEncoding.EncodeToString(key)
}

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("mcp error %d: %s %s", e.Code, e.Message, e.Data)
}

func (e *RPCError) DataMap() map[string]any {
	var m map[string]any
	_ = json.Unmarshal(e.Data, &m)
	return m
}

type Response struct {
	Status int
	Header http.Header
	Raw    []byte
	Result json.RawMessage
	Error  *RPCError
}

func (r *Response) Decode(v any) error {
	if r.Error != nil {
		return r.Error
	}
	if r.Status < 200 || r.Status > 299 {
		return fmt.Errorf("http status %d: %s", r.Status, r.Raw)
	}
	return json.Unmarshal(r.Result, v)
}

type Client struct {
	URL             string
	Principal       string
	ProtocolVersion string
	HTTP            *http.Client

	sessionID string
	nextID    atomic.Int64
}

type Option func(*Client)

func WithoutPrincipal() Option { return func(c *Client) { c.Principal = "" } }

func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.HTTP = h } }

func WithProtocolVersion(v string) Option { return func(c *Client) { c.ProtocolVersion = v } }

func New(url, principal string, opts ...Option) *Client {
	c := &Client{
		URL:             url,
		Principal:       principal,
		ProtocolVersion: DefaultProtocolVersion,
		HTTP:            &http.Client{Timeout: 30 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func (c *Client) SessionID() string { return c.sessionID }

type callConfig struct {
	name     string
	setName  bool
	omitName bool
}

type CallOption func(*callConfig)

func NameHeader(v string) CallOption {
	return func(c *callConfig) { c.name, c.setName = v, true }
}

func OmitNameHeader() CallOption { return func(c *callConfig) { c.omitName = true } }

type InitializeResult struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    Capabilities    `json:"capabilities"`
	ServerInfo      json.RawMessage `json:"serverInfo"`
}

type Capabilities struct {
	Events     json.RawMessage            `json:"events"`
	Extensions map[string]json.RawMessage `json:"extensions"`
}

func (c *Client) Initialize(ctx context.Context) (InitializeResult, *Response, error) {
	var res InitializeResult
	resp, err := c.Call(ctx, "initialize", map[string]any{
		"protocolVersion": c.ProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "outpost-e2e", "version": "0"},
	})
	if err != nil {
		return res, resp, err
	}
	if err := resp.Decode(&res); err != nil {
		return res, resp, err
	}
	if id := resp.Header.Get(HeaderSessionID); id != "" {
		c.sessionID = id
	}
	if _, err := c.notify(ctx, "notifications/initialized"); err != nil {
		return res, resp, err
	}
	return res, resp, nil
}

func (c *Client) Call(ctx context.Context, method string, params any, opts ...CallOption) (*Response, error) {
	id := c.nextID.Add(1)
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	var cfg callConfig
	for _, o := range opts {
		o(&cfg)
	}
	hdr := http.Header{HeaderMethod: {method}}
	switch {
	case cfg.omitName:
	case cfg.setName:
		hdr.Set(HeaderName, cfg.name)
	default:
		if name := paramsName(params); name != "" {
			hdr.Set(HeaderName, name)
		}
	}
	resp, err := c.post(ctx, body, hdr)
	if err != nil {
		return resp, err
	}
	if err := parseReply(resp, id); err != nil {
		return resp, err
	}
	return resp, nil
}

func (c *Client) notify(ctx context.Context, method string) (*Response, error) {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method})
	resp, err := c.post(ctx, body, http.Header{HeaderMethod: {method}})
	if err != nil {
		return resp, err
	}
	if resp.Status < 200 || resp.Status > 299 {
		return resp, fmt.Errorf("%s: http status %d: %s", method, resp.Status, resp.Raw)
	}
	return resp, nil
}

func (c *Client) post(ctx context.Context, body []byte, extra http.Header) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.Principal != "" {
		req.Header.Set("Authorization", "Bearer "+c.Principal)
	}
	if c.sessionID != "" {
		req.Header.Set(HeaderSessionID, c.sessionID)
		req.Header.Set(HeaderProtocolVersion, c.ProtocolVersion)
	}
	for k, vs := range extra {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	httpResp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}
	return &Response{Status: httpResp.StatusCode, Header: httpResp.Header, Raw: raw}, nil
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
}

// parseReply fills Result and Error from a JSON or SSE body. A non-2xx
// response without a matching reply is not an error: the caller checks Status.
func parseReply(resp *Response, id int64) error {
	want := fmt.Sprint(id)
	var candidates [][]byte
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		sc := bufio.NewScanner(bytes.NewReader(resp.Raw))
		sc.Buffer(nil, 1<<24)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data:"); ok {
				candidates = append(candidates, []byte(strings.TrimSpace(data)))
			}
		}
	} else if len(bytes.TrimSpace(resp.Raw)) > 0 {
		candidates = [][]byte{resp.Raw}
	}
	for _, c := range candidates {
		var m rpcMessage
		if json.Unmarshal(c, &m) != nil || string(m.ID) != want {
			continue
		}
		resp.Result, resp.Error = m.Result, m.Error
		return nil
	}
	if resp.Status >= 200 && resp.Status <= 299 {
		return errors.New("no JSON-RPC reply in response: " + string(resp.Raw))
	}
	return nil
}

func paramsName(params any) string {
	switch p := params.(type) {
	case SubscribeParams:
		return p.Name
	case UnsubscribeParams:
		return p.Name
	case map[string]any:
		s, _ := p["name"].(string)
		return s
	}
	return ""
}
