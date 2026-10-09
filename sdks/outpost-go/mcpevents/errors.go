package mcpevents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
)

// ErrorCodes are the JSON-RPC codes for the MCP Events error kinds.
//
// Outpost fills the code itself in every mcp_error it returns (its
// MCP_ERROR_CODES setting). These codes only cover the errors the handlers
// raise locally (a missing principal, a topic outside the allowlist,
// malformed params), so pick the profile matching the Outpost setting.
type ErrorCodes struct {
	InvalidParams         int64
	NotFound              int64
	Forbidden             int64
	ResourceExhausted     int64
	Unsupported           int64
	CallbackEndpointError int64
	InternalError         int64
}

var (
	// CodesSketch is the MCP Events design sketch numbering, which ChatGPT
	// follows. It is the Outpost default.
	CodesSketch = ErrorCodes{
		InvalidParams:         -32602,
		NotFound:              -32011,
		Forbidden:             -32012,
		ResourceExhausted:     -32013,
		Unsupported:           -32014,
		CallbackEndpointError: -32015,
		InternalError:         -32603,
	}
	// CodesSEP3415 is the provisional SEP-3415 numbering.
	CodesSEP3415 = ErrorCodes{
		InvalidParams:         -32602,
		NotFound:              -32023,
		Forbidden:             -32024,
		ResourceExhausted:     -32025,
		Unsupported:           -32026,
		CallbackEndpointError: -32027,
		InternalError:         -32603,
	}
)

// CodeProfile returns the codes for an MCP_ERROR_CODES value: "sketch" or
// "sep-3415".
func CodeProfile(name string) (ErrorCodes, bool) {
	switch name {
	case "sketch":
		return CodesSketch, true
	case "sep-3415":
		return CodesSEP3415, true
	}
	return ErrorCodes{}, false
}

// MCPError is an mcp_error Outpost returned with HTTP 422. Code, Message and
// Data are the JSON-RPC error to send back to the MCP client.
type MCPError struct {
	StatusCode int
	Kind       string
	Code       int64
	Message    string
	// Data is the raw JSON of mcp_error.data, nil when absent.
	Data json.RawMessage
}

func (e *MCPError) Error() string {
	return fmt.Sprintf("outpost: mcp_error %s (%d): %s", e.Kind, e.Code, e.Message)
}

// RequestError is any other failure of an MCP endpoint call: a non-422
// status, a network error, a timeout or a malformed response. Body holds at
// most the first KiB of the response, for logs. Never forward it to MCP
// clients.
type RequestError struct {
	// StatusCode is 0 when no response was received.
	StatusCode int
	Body       string
	Err        error
	msg        string
}

func (e *RequestError) Error() string {
	if e.Err != nil {
		return e.msg + ": " + e.Err.Error()
	}
	return e.msg
}

func (e *RequestError) Unwrap() error { return e.Err }

// RPCError is a JSON-RPC error returned by the handlers, to send back as is.
// Return one from a resolver to answer with your own error; any other error
// from them becomes a generic internal error.
type RPCError struct {
	Code    int64           `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return e.Message }

// MCPErrorProvider is implemented by errors that carry an Outpost mcp_error,
// for example a wrapper around a generated SDK error type.
type MCPErrorProvider interface {
	OutpostMCPError() *MCPError
}

// maxDuckBody bounds the body parsed out of an HTTP-shaped error; real
// mcp_error bodies are a few hundred bytes.
const maxDuckBody = 64 << 10

// MCPErrorFrom returns the mcp_error an error carries, by duck typing: an
// *MCPError or MCPErrorProvider anywhere in the chain, or an HTTP error
// struct with exported StatusCode 422 and a JSON Body string (the shape of
// the generated apierrors.APIError).
func MCPErrorFrom(err error) (*MCPError, bool) {
	var mcpErr *MCPError
	if errors.As(err, &mcpErr) && mcpErr != nil {
		return mcpErr, true
	}
	var provider MCPErrorProvider
	if errors.As(err, &provider) {
		if m := provider.OutpostMCPError(); m != nil {
			return m, true
		}
	}
	for e := err; e != nil; e = errors.Unwrap(e) {
		if m := mcpErrorFromHTTPFields(e); m != nil {
			return m, true
		}
	}
	return nil, false
}

func mcpErrorFromHTTPFields(err error) *MCPError {
	v := reflect.ValueOf(err)
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	status, ok := exportedField(v, "StatusCode")
	if !ok || (status.Kind() != reflect.Int && status.Kind() != reflect.Int64) || status.Int() != 422 {
		return nil
	}
	body, ok := exportedField(v, "Body")
	if !ok || body.Kind() != reflect.String || body.Len() > maxDuckBody {
		return nil
	}
	return parseMCPErrorBody([]byte(body.String()), 422)
}

func exportedField(v reflect.Value, name string) (reflect.Value, bool) {
	f, ok := v.Type().FieldByName(name)
	if !ok || !f.IsExported() || len(f.Index) != 1 {
		return reflect.Value{}, false
	}
	return v.Field(f.Index[0]), true
}

// parseMCPErrorBody parses {"mcp_error": {...}}; nil unless well formed: an
// integer code and a string message.
func parseMCPErrorBody(body []byte, status int) *MCPError {
	var envelope struct {
		MCPError *struct {
			Kind    json.RawMessage `json:"kind"`
			Code    json.RawMessage `json:"code"`
			Message json.RawMessage `json:"message"`
			Data    json.RawMessage `json:"data"`
		} `json:"mcp_error"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.MCPError == nil {
		return nil
	}
	raw := envelope.MCPError
	code, err := strconv.ParseInt(string(raw.Code), 10, 64)
	if err != nil {
		return nil
	}
	var message string
	if len(raw.Message) == 0 || raw.Message[0] != '"' || json.Unmarshal(raw.Message, &message) != nil {
		return nil
	}
	var kind string
	_ = json.Unmarshal(raw.Kind, &kind)
	out := &MCPError{StatusCode: status, Kind: kind, Code: code, Message: message}
	if data := bytes.TrimSpace(raw.Data); len(data) > 0 && !bytes.Equal(data, []byte("null")) {
		out.Data = data
	}
	return out
}
