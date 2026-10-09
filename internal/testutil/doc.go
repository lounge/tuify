// Package testutil holds test-only helpers shared across the other
// internal packages:
//
//   - RewriteTransport rewrites outbound HTTP requests to a local
//     httptest.Server so tests can stub Spotify API responses without
//     hitting the real endpoint. It serves loopback servers from
//     httptest.NewServer and the in-memory ones from httptest.NewTestServer
//     alike; the latter also run inside testing/synctest bubbles.
//   - RunWithLeakCheck wraps testing.M.Run and fails the package when the
//     runtime's goroutineleak profile reports goroutines that can never
//     resume. Every package that starts goroutines calls it from TestMain,
//     which turns "every background goroutine unwinds at shutdown" from a
//     documented invariant into a test failure.
//
// Import this package only from _test.go files.
package testutil
