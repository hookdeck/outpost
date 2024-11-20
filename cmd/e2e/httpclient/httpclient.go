package httpclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"

	"github.com/google/go-cmp/cmp"
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

type ResponseBody = interface{}

type Response struct {
	StatusCode int          `json:"statusCode"`
	Body       ResponseBody `json:"body"`
}

func (r *Response) FromHTTPResponse(resp *http.Response) error {
	r.StatusCode = resp.StatusCode
	if resp.Body != nil {
		defer resp.Body.Close()
		json.NewDecoder(resp.Body).Decode(&r.Body)
	}
	return nil
}

func (r *Response) MatchBody(body ResponseBody) bool {
	return r.doMatchBody(r.Body, body)
}

func (r *Response) doMatchBody(mainBody ResponseBody, toMatchedBody ResponseBody) bool {
	mainBodyTyped, ok := mainBody.(map[string]interface{})
	if !ok {
		return cmp.Equal(mainBody, toMatchedBody)
	}

	toMatchedBodyTyped, ok := toMatchedBody.(map[string]interface{})
	if !ok {
		return cmp.Equal(mainBody, toMatchedBody)
	}

	for key, subValue := range toMatchedBodyTyped {
		fullValue, ok := mainBodyTyped[key]
		if !ok {
			return false
		}
		log.Println(key, subValue, fullValue)

		switch subValueTyped := subValue.(type) {
		case map[string]interface{}:
			fullValueTyped, ok := fullValue.(map[string]interface{})
			if !ok {
				return false
			}
			if !r.doMatchBody(fullValueTyped, subValueTyped) {
				return false
			}
		default:
			if isSlice(subValue) && isSlice(fullValue) {
				subValueTyped := convertToInterfaceSlice(subValue)
				fullValueTyped := convertToInterfaceSlice(fullValue)
				for i, subValue := range subValueTyped {
					if !r.doMatchBody(fullValueTyped[i], subValue) {
						return false
					}
				}
			} else {
				if !cmp.Equal(fullValue, subValue) {
					return false
				}
			}
		}
	}
	return true
}

func isSlice(value interface{}) bool {
	v := reflect.ValueOf(value)
	return v.Kind() == reflect.Slice
}

func convertToInterfaceSlice(slice interface{}) []interface{} {
	v := reflect.ValueOf(slice)
	if v.Kind() != reflect.Slice {
		return nil
	}
	result := make([]interface{}, v.Len())
	for i := 0; i < v.Len(); i++ {
		result[i] = v.Index(i).Interface()
	}
	return result
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
