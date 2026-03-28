package testutil

import "net/http"

// RewriteTransport redirects all requests to a test server URL.
type RewriteTransport struct {
	Base   http.RoundTripper
	Target string
}

func (t *RewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.URL.Scheme = "http"
	r.URL.Host = t.Target[len("http://"):]
	return t.Base.RoundTrip(r)
}
