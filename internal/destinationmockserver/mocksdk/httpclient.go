package mocksdk

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

type httpClient struct {
	baseURL *url.URL
	client  *http.Client
}

func (c *httpClient) get(ctx context.Context, path string) (*http.Response, error) {
	rel := &url.URL{Path: path}
	u := c.baseURL.ResolveReference(rel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	return c.client.Do(req)
}

func (c *httpClient) post(ctx context.Context, path string, body interface{}) (*http.Response, error) {
	return c.doRequest(ctx, http.MethodPost, path, body)
}

func (c *httpClient) put(ctx context.Context, path string, body interface{}) (*http.Response, error) {
	return c.doRequest(ctx, http.MethodPut, path, body)
}

func (c *httpClient) patch(ctx context.Context, path string, body interface{}) (*http.Response, error) {
	return c.doRequest(ctx, http.MethodPatch, path, body)
}

func (c *httpClient) delete(ctx context.Context, path string) (*http.Response, error) {
	rel := &url.URL{Path: path}
	u := c.baseURL.ResolveReference(rel)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
	if err != nil {
		return nil, err
	}

	return c.client.Do(req)
}

func (c *httpClient) doRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	rel := &url.URL{Path: path}
	u := c.baseURL.ResolveReference(rel)

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), &buf)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.client.Do(req)
}
