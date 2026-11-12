package mcpclient

import (
	"context"
	"encoding/json"
)

type ListResult struct {
	ResultType string  `json:"resultType"`
	Events     []Event `json:"events"`
	NextCursor *string `json:"nextCursor"`
}

type Event struct {
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	InputSchema   map[string]any `json:"inputSchema"`
	PayloadSchema map[string]any `json:"payloadSchema"`
}

func (c *Client) EventsList(ctx context.Context, opts ...CallOption) (*Response, error) {
	return c.Call(ctx, "events/list", map[string]any{}, opts...)
}

type SubscribeParams struct {
	Name      string
	Arguments map[string]any
	Mode      string
	// OmitMode leaves delivery.mode out of the request.
	OmitMode bool
	URL      string
	Secret   string
	TTLMs    any
}

func (p SubscribeParams) MarshalJSON() ([]byte, error) {
	mode := p.Mode
	if mode == "" {
		mode = "webhook"
	}
	delivery := map[string]any{"mode": mode, "url": p.URL, "secret": p.Secret}
	if p.OmitMode {
		delete(delivery, "mode")
	}
	m := map[string]any{
		"name":     p.Name,
		"delivery": delivery,
	}
	if p.Arguments != nil {
		m["arguments"] = p.Arguments
	}
	if p.TTLMs != nil {
		m["ttlMs"] = p.TTLMs
	}
	return json.Marshal(m)
}

type SubscribeResult struct {
	ResultType     string          `json:"resultType"`
	ID             string          `json:"id"`
	RefreshBefore  *string         `json:"refreshBefore"`
	Cursor         *string         `json:"cursor"`
	Truncated      bool            `json:"truncated"`
	DeliveryStatus *DeliveryStatus `json:"deliveryStatus"`
}

type DeliveryStatus struct {
	Active    bool    `json:"active"`
	LastError *string `json:"lastError"`
}

func (c *Client) Subscribe(ctx context.Context, p SubscribeParams, opts ...CallOption) (*Response, error) {
	return c.Call(ctx, "events/subscribe", p, opts...)
}

type UnsubscribeParams struct {
	Name      string
	Arguments map[string]any
	URL       string
}

func (p UnsubscribeParams) MarshalJSON() ([]byte, error) {
	m := map[string]any{
		"name":     p.Name,
		"delivery": map[string]any{"url": p.URL},
	}
	if p.Arguments != nil {
		m["arguments"] = p.Arguments
	}
	return json.Marshal(m)
}

func (c *Client) Unsubscribe(ctx context.Context, p UnsubscribeParams, opts ...CallOption) (*Response, error) {
	return c.Call(ctx, "events/unsubscribe", p, opts...)
}
