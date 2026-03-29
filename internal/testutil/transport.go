package testutil

import (
	"net/http"
	"net/url"
)

// RewriteTransport redirects all requests to a test server URL.
type RewriteTransport struct {
	Base   http.RoundTripper
	Target string
}

func (t *RewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	target, _ := url.Parse(t.Target)
	r.URL.Scheme = target.Scheme
	r.URL.Host = target.Host
	return t.Base.RoundTrip(r)
}
