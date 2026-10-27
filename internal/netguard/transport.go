package netguard

import (
	"errors"
	"net/http"
)

// WrapTransport returns a RoundTripper that checks each request's target URL
// with CheckURL before it goes to base. It is for a base that connects
// through a forward proxy, which resolves the target itself and whose own
// address is not guarded. The request is never modified.
func (g *Guard) WrapTransport(base http.RoundTripper) http.RoundTripper {
	return &guardTransport{base: base, guard: g}
}

type guardTransport struct {
	base  http.RoundTripper
	guard *Guard
}

func (t *guardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	closeBody := func() {
		if req.Body != nil {
			_ = req.Body.Close()
		}
	}
	if req.URL == nil {
		closeBody()
		return nil, errors.New("netguard: nil Request.URL")
	}
	if err := t.guard.CheckURL(req.Context(), req.URL); err != nil {
		closeBody()
		return nil, err
	}
	return t.base.RoundTrip(req)
}

func (t *guardTransport) CloseIdleConnections() {
	if c, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}
