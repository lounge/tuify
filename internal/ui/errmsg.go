package ui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"github.com/lounge/tuify/internal/spotify"
	"golang.org/x/oauth2"
)

// userMessage turns an error from a Spotify call into short text for the
// status banner and list error rows. Raw error strings can carry a JSON
// response body or a full request URL, which is noise to the user; the
// details still go to debug.log from the handler that received the error.
//
// Errors that are not Spotify API or network errors are returned as-is,
// because the ones that reach the UI (e.g. FindDevice's "no Spotify devices
// found") are already written for the user.
//
// Pure: safe to call from View.
func userMessage(err error) string {
	if err == nil {
		return ""
	}
	if apiErr, ok := errors.AsType[*spotify.APIError](err); ok {
		switch s := apiErr.Status; {
		case s == http.StatusUnauthorized:
			return "Spotify session expired, restart tuify to log in"
		case s == http.StatusForbidden:
			return "Spotify refused this action (Premium or playback restriction)"
		case s == http.StatusNotFound:
			return "Not found on Spotify (is a device active?)"
		case s == http.StatusTooManyRequests:
			if rle, ok := errors.AsType[*spotify.RateLimitedError](err); ok {
				return "Rate limited by Spotify until " + rle.Until.Format("15:04:05")
			}
			return "Rate limited by Spotify, try again shortly"
		case s >= 500:
			return "Spotify is having trouble, try again shortly"
		default:
			return fmt.Sprintf("Spotify request failed (HTTP %d)", s)
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Request timed out"
	}
	// A token refresh rejected by accounts.spotify.com. It reaches us
	// wrapped in *url.Error like a network failure, so it must be matched
	// before the transport checks below. Covers auth.ErrTokenRevoked, which
	// wraps the RetrieveError.
	if _, ok := errors.AsType[*oauth2.RetrieveError](err); ok {
		return "Spotify login failed, restart tuify to log in"
	}
	if errors.Is(err, context.Canceled) {
		return "Request cancelled"
	}
	// Match concrete network failures rather than the net.Error interface:
	// *url.Error implements net.Error, so every failed HTTP call (TLS,
	// auth refresh, cancellation) would otherwise read as a network error.
	if ne, ok := errors.AsType[net.Error](err); ok && ne.Timeout() {
		return "Request timed out"
	}
	if _, ok := errors.AsType[*net.OpError](err); ok {
		return "Network error, check your connection"
	}
	if _, ok := errors.AsType[*net.DNSError](err); ok {
		return "Network error, check your connection"
	}
	if _, ok := errors.AsType[*url.Error](err); ok {
		// Anything else that failed below HTTP (TLS, proxy, malformed
		// response). Its text is a raw URL plus a Go error; keep that in
		// the log only.
		return "Could not reach Spotify"
	}
	return err.Error()
}
