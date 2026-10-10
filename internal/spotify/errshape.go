package spotify

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"io"
	"log"
	"net/http"
)

// maxErrorBodyBytes bounds how much of an error response errorShapeTransport
// reads to decide whether it is Spotify's JSON. A real Spotify error is a
// few hundred bytes; an HTML gateway page is read up to here and dropped.
const maxErrorBodyBytes = 64 << 10

// errorShapeTransport makes every non-2xx response carry Spotify's error
// JSON, {"error":{"status":N,"message":"…"}}. Gateways in front of the API
// answer with an HTML page or an empty body, which the zmb3 SDK reports as
// a plain error without the status and which the raw REST path would keep
// as the error body. Such a body is replaced by the shape with the real
// status and a generic message, so wrapSDKErr and doWithRetry both see
// the status and neither carries the page. A body that already has the
// shape passes through untouched; one whose status is missing or
// disagrees with the response is rewritten to the response's status with
// its own message kept.
type errorShapeTransport struct {
	base http.RoundTripper
}

func newErrorShapeTransport(base http.RoundTripper) *errorShapeTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &errorShapeTransport{base: base}
}

func (t *errorShapeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp == nil || (resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return resp, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if shaped, ok := shapeErrorBody(body, resp.StatusCode); ok {
		if len(body) > 0 {
			// The page is dropped from the error; keep a trace of it here,
			// since nothing else will log it.
			// The request is not named: the caller's error carries the
			// endpoint, and this line only keeps what the error drops.
			log.Printf("[spotify] HTTP %d with a non-JSON body (%d bytes): %q",
				resp.StatusCode, len(body), truncateForLog(body))
		}
		body = shaped
		resp.Header.Set("Content-Type", "application/json")
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	return resp, nil
}

// spotifyErrorBody is the shape of every error response from the Web API.
type spotifyErrorBody struct {
	Error struct {
		Status  int    `json:"status"`
		Message string `json:"message"`
	} `json:"error"`
}

// shapeErrorBody returns the Spotify error JSON a non-2xx body with the
// given status should have carried, and false when body already does.
func shapeErrorBody(body []byte, status int) ([]byte, bool) {
	var parsed spotifyErrorBody
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error.Status == status {
		return nil, false
	}
	var shaped spotifyErrorBody
	shaped.Error.Status = status
	shaped.Error.Message = parsed.Error.Message
	if shaped.Error.Message == "" {
		shaped.Error.Message = fmt.Sprintf("HTTP %d %s", status, http.StatusText(status))
	}
	out, err := json.Marshal(shaped)
	if err != nil {
		// Only a message with invalid UTF-8 can fail here; the generic
		// text never does.
		shaped.Error.Message = fmt.Sprintf("HTTP %d %s", status, http.StatusText(status))
		out, _ = json.Marshal(shaped)
	}
	return out, true
}
