package httpclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	MethodGET    = "GET"
	MethodPOST   = "POST"
	MethodPUT    = "PUT"
	MethodPATCH  = "PATCH"
	MethodDELETE = "DELETE"
)

type Request struct {
	Method  string
	Path    string
	Body    map[string]interface{}
	Headers map[string]string
}

func (r *Request) ToHTTPRequest(baseURL string) (*http.Request, error) {
	var bodyReader io.Reader
	if r.Body != nil {
		jsonBody, err := json.Marshal(r.Body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(jsonBody)
	}
	request, err := http.NewRequest(r.Method, fmt.Sprintf("%s%s", baseURL, r.Path), bodyReader)
	if err != nil {
		return nil, err
	}
	for k, v := range r.Headers {
		request.Header.Set(k, v)
	}
	return request, nil
}

type Response struct {
	StatusCode int
	Body       map[string]interface{}
}

func (r *Response) FromHTTPResponse(resp *http.Response) error {
	r.StatusCode = resp.StatusCode
	if resp.Body != nil {
		defer resp.Body.Close()
		json.NewDecoder(resp.Body).Decode(&r.Body)
	}
	return nil
}

type Client interface {
	Do(req Request) (Response, error)
}

type client struct {
	client  *http.Client
	baseURL string
	apiKey  string
}

func (c *client) Do(req Request) (Response, error) {
	httpReq, err := req.ToHTTPRequest(c.baseURL)
	if err != nil {
		return Response{}, err
	}
	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return Response{}, err
	}
	resp := Response{}
	if err := resp.FromHTTPResponse(httpResp); err != nil {
		return Response{}, err
	}
	return resp, nil
}

func New(baseURL string, apiKey string) Client {
	return &client{
		client:  http.DefaultClient,
		baseURL: baseURL,
		apiKey:  apiKey,
	}
}
