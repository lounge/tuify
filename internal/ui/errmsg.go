package ui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/lounge/tuify/internal/spotify"
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
	if _, ok := errors.AsType[net.Error](err); ok {
		return "Network error, check your connection"
	}
	return err.Error()
}
