// Package testutil holds test-only helpers shared across the other
// internal packages. Most notably RewriteTransport, which rewrites
// outbound HTTP requests to a local httptest.Server so tests can stub
// Spotify API responses without hitting the real endpoint.
//
// Import this package only from _test.go files.
package testutil
