package ui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
	"github.com/lounge/tuify/internal/testutil"
)

// npStep is one poll reply fed to handlePlayerState, followed by a check
// of the model (and the Cmd it returned) after it was applied.
type npStep struct {
	msg   playerStateMsg
	check func(t *testing.T, np *nowPlayingModel, resume *episodeResumeMsg)
}

// pstate builds a PlayerState for a 5-minute item.
func pstate(uri string, playing bool, progressMs int) *spotify.PlayerState {
	return &spotify.PlayerState{
		TrackURI:      uri,
		TrackName:     "name " + uri,
		ArtistName:    "artist",
		Playing:       playing,
		ProgressMs:    progressMs,
		DurationMs:    300_000,
		VolumePercent: 100,
	}
}

func withDevice(s *spotify.PlayerState, device string) *spotify.PlayerState {
	s.DeviceName = device
	return s
}

func withShuffle(s *spotify.PlayerState, on bool) *spotify.PlayerState {
	s.Shuffling = on
	return s
}

func withContext(s *spotify.PlayerState, contextURI string) *spotify.PlayerState {
	s.ContextURI = contextURI
	return s
}

// runResume executes the Cmd returned by handlePlayerState and reports the
// episodeResumeMsg it produced, if any.
func runResume(t *testing.T, cmd tea.Cmd) *episodeResumeMsg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg, ok := cmd().(episodeResumeMsg)
	if !ok {
		t.Fatalf("handlePlayerState returned a Cmd producing %T, want episodeResumeMsg", cmd())
	}
	return &msg
}

func wantNoResume(t *testing.T, resume *episodeResumeMsg) {
	t.Helper()
	if resume != nil {
		t.Errorf("unexpected episodeResumeMsg{posMs: %d}", resume.posMs)
	}
}

func wantPlayback(t *testing.T, np *nowPlayingModel, playing bool, progressMs int) {
	t.Helper()
	if np.playing != playing {
		t.Errorf("playing = %v, want %v", np.playing, playing)
	}
	if np.progressMs != progressMs {
		t.Errorf("progressMs = %d, want %d", np.progressMs, progressMs)
	}
}

func wantOverride(t *testing.T, np *nowPlayingModel, want bool) {
	t.Helper()
	if np.deviceOverridden != want {
		t.Errorf("deviceOverridden = %v, want %v", np.deviceOverridden, want)
	}
	if got := np.client.DeviceOverridden.Load(); got != want {
		t.Errorf("client.DeviceOverridden = %v, want %v (must mirror the model)", got, want)
	}
}

func TestHandlePlayerState_Sequences(t *testing.T) {
	const (
		trackX   = "spotify:track:x"
		trackY   = "spotify:track:y"
		episodeA = "spotify:episode:a"
		episodeB = "spotify:episode:b"
	)

	tests := []struct {
		name      string
		preferred string
		setup     func(np *nowPlayingModel)
		steps     []npStep
	}{
		{
			name: "pending pause ignores stale playing=true until confirmed",
			setup: func(np *nowPlayingModel) {
				// Playing X, then the user pressed space: optimistic pause.
				np.trackURI, np.hasTrack, np.progressMs = trackX, true, 4000
				np.playing, np.playPauseFlip.seq = false, 1
			},
			steps: []npStep{
				{msg: playerStateMsg{state: pstate(trackX, true, 5000)}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					wantPlayback(t, np, false, 4000)
					if np.playPauseFlip.seq == 0 {
						t.Error("stale playing=true cleared playPauseFlip.seq")
					}
					wantNoResume(t, r)
				}},
				{msg: playerStateMsg{state: pstate(trackX, false, 5100)}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					wantPlayback(t, np, false, 5100)
					if np.playPauseFlip.seq != 0 {
						t.Error("confirming state did not clear playPauseFlip.seq")
					}
				}},
				{msg: playerStateMsg{state: pstate(trackX, true, 9000)}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					// Once confirmed, an external resume is accepted.
					wantPlayback(t, np, true, 9000)
				}},
			},
		},
		{
			name: "pending pause holds within its settle window",
			setup: func(np *nowPlayingModel) {
				np.trackURI, np.hasTrack, np.progressMs = trackX, true, 4000
				np.playing, np.playPauseFlip.seq = false, 1
				np.playPauseFlip.settleBy = time.Now().Add(time.Minute) // Spotify accepted it just now
			},
			steps: []npStep{
				{msg: playerStateMsg{state: pstate(trackX, true, 5000)}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					wantPlayback(t, np, false, 4000)
					if np.playPauseFlip.seq == 0 {
						t.Error("a poll inside the settle window cleared playPauseFlip.seq")
					}
				}},
			},
		},
		{
			// Another client resumed before a poll confirmed the pause, or
			// the device ignored it. Waiting for "paused" left the UI paused
			// for good, with space sending Resume to a playing player.
			name: "settled pending pause yields to the polled state",
			setup: func(np *nowPlayingModel) {
				np.trackURI, np.hasTrack, np.progressMs = trackX, true, 4000
				np.playing, np.playPauseFlip.seq = false, 1
				np.playPauseFlip.settleBy = time.Now().Add(-time.Millisecond)
			},
			steps: []npStep{
				{msg: playerStateMsg{state: pstate(trackX, true, 9000)}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					wantPlayback(t, np, true, 9000)
					if np.playPauseFlip.seq != 0 {
						t.Error("settled flip still pending after a contradicting poll")
					}
				}},
			},
		},
		{
			name: "pending pause is dropped when the track changes",
			setup: func(np *nowPlayingModel) {
				np.trackURI, np.hasTrack, np.progressMs = trackX, true, 4000
				np.playing, np.playPauseFlip.seq = false, 1
			},
			steps: []npStep{
				{msg: playerStateMsg{state: pstate(trackY, true, 700)}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					wantPlayback(t, np, true, 700)
					if np.playPauseFlip.seq != 0 {
						t.Error("track change must drop the stale pending play/pause")
					}
				}},
			},
		},
		{
			name: "pending shuffle reconciles on confirmation",
			setup: func(np *nowPlayingModel) {
				np.trackURI, np.hasTrack, np.playing = trackX, true, true
				np.shuffling, np.shuffleFlip.seq = true, 1 // user pressed r
			},
			steps: []npStep{
				{msg: playerStateMsg{state: withShuffle(pstate(trackX, true, 1000), false)}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					if !np.shuffling || np.shuffleFlip.seq == 0 {
						t.Errorf("stale shuffle=false applied: shuffling=%v pending=%v", np.shuffling, np.shuffleFlip.seq)
					}
				}},
				{msg: playerStateMsg{state: withShuffle(pstate(trackX, true, 2000), true)}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					if !np.shuffling || np.shuffleFlip.seq != 0 {
						t.Errorf("confirmation not reconciled: shuffling=%v pending=%v", np.shuffling, np.shuffleFlip.seq)
					}
				}},
				{msg: playerStateMsg{state: withShuffle(pstate(trackX, true, 3000), false)}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					if np.shuffling {
						t.Error("external shuffle-off after reconciliation was not applied")
					}
				}},
			},
		},
		{
			name: "settled pending shuffle yields to the polled state",
			setup: func(np *nowPlayingModel) {
				np.trackURI, np.hasTrack, np.playing = trackX, true, true
				np.shuffling, np.shuffleFlip.seq = true, 1
				np.shuffleFlip.settleBy = time.Now().Add(-time.Millisecond)
			},
			steps: []npStep{
				{msg: playerStateMsg{state: withShuffle(pstate(trackX, true, 1000), false)}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					if np.shuffling || np.shuffleFlip.seq != 0 {
						t.Errorf("settled shuffle kept against the poll: shuffling=%v pending=%v", np.shuffling, np.shuffleFlip.seq)
					}
				}},
			},
		},
		{
			name: "episode A to B to A resumes the cached position",
			steps: []npStep{
				{msg: playerStateMsg{state: pstate(episodeA, true, 120_000)}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					wantPlayback(t, np, true, 120_000)
					wantNoResume(t, r)
				}},
				{msg: playerStateMsg{state: pstate(episodeB, true, 0)}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					if got := np.progressCache[episodeA]; got != 120_000 {
						t.Errorf("progressCache[A] = %d, want 120000", got)
					}
					wantPlayback(t, np, true, 0)
					wantNoResume(t, r)
				}},
				{msg: playerStateMsg{state: pstate(episodeA, true, 1000)}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					wantPlayback(t, np, true, 120_000)
					if np.resumeUntilMs != 120_000 {
						t.Errorf("resumeUntilMs = %d, want 120000", np.resumeUntilMs)
					}
					if r == nil || r.posMs != 120_000 {
						t.Errorf("resume = %v, want episodeResumeMsg{posMs: 120000}", r)
					}
				}},
				{msg: playerStateMsg{state: pstate(episodeA, true, 2000)}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					// Spotify hasn't applied the seek yet: keep the cached position.
					wantPlayback(t, np, true, 120_000)
					if np.resumeUntilMs != 120_000 {
						t.Errorf("resumeUntilMs = %d, want it held at 120000", np.resumeUntilMs)
					}
					wantNoResume(t, r)
				}},
				{msg: playerStateMsg{state: pstate(episodeA, true, 121_000)}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					wantPlayback(t, np, true, 121_000)
					if np.resumeUntilMs != 0 {
						t.Errorf("resumeUntilMs = %d, want 0 once the API caught up", np.resumeUntilMs)
					}
				}},
			},
		},
		{
			name: "no resume when the API already reports real progress",
			setup: func(np *nowPlayingModel) {
				np.progressCache[episodeA] = 60_000
			},
			steps: []npStep{
				{msg: playerStateMsg{state: pstate(episodeA, true, episodeResumeThresholdMs)}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					wantPlayback(t, np, true, episodeResumeThresholdMs)
					wantNoResume(t, r)
				}},
			},
		},
		{
			name:      "external device sets override and returning to preferred clears it",
			preferred: "tuify",
			steps: []npStep{
				{msg: playerStateMsg{state: withDevice(pstate(trackX, true, 0), "tuify")}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					wantOverride(t, np, false)
				}},
				{msg: playerStateMsg{state: withDevice(pstate(trackX, true, 1000), "Phone")}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					wantOverride(t, np, true)
				}},
				{msg: playerStateMsg{state: withDevice(pstate(trackX, true, 2000), "Laptop")}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					wantOverride(t, np, true)
				}},
				{msg: playerStateMsg{state: withDevice(pstate(trackX, true, 3000), "tuify")}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					wantOverride(t, np, false)
					if np.deviceName != "tuify" {
						t.Errorf("deviceName = %q, want tuify", np.deviceName)
					}
				}},
			},
		},
		{
			name:      "first poll on an external device sets override",
			preferred: "tuify",
			steps: []npStep{
				{msg: playerStateMsg{state: withDevice(pstate(trackX, true, 0), "Phone")}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					wantOverride(t, np, true)
				}},
			},
		},
		{
			name: "context follows the state and clears for a contextless item",
			steps: []npStep{
				{msg: playerStateMsg{state: withContext(pstate(trackX, true, 0), "spotify:playlist:p")}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					if np.contextURI != "spotify:playlist:p" {
						t.Errorf("contextURI = %q, want the playlist", np.contextURI)
					}
				}},
				// A queue from search plays with no context. Keeping the
				// playlist would re-establish playback inside it.
				{msg: playerStateMsg{state: pstate(trackY, true, 0)}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					if np.contextURI != "" {
						t.Errorf("contextURI = %q after a contextless item, want empty", np.contextURI)
					}
				}},
			},
		},
		{
			name: "nil state clears hasTrack",
			steps: []npStep{
				{msg: playerStateMsg{state: pstate(trackX, true, 1000)}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					if !np.hasTrack {
						t.Error("hasTrack = false after a state")
					}
				}},
				{msg: playerStateMsg{}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					if np.hasTrack {
						t.Error("nil state must clear hasTrack")
					}
				}},
			},
		},
		{
			name: "skipped and error replies change nothing",
			steps: []npStep{
				{msg: playerStateMsg{state: pstate(trackX, true, 1000)}},
				{msg: playerStateMsg{skipped: true}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					if !np.hasTrack || np.trackURI != trackX {
						t.Errorf("skipped reply changed state: hasTrack=%v uri=%q", np.hasTrack, np.trackURI)
					}
					wantPlayback(t, np, true, 1000)
				}},
				{msg: playerStateMsg{err: errors.New("boom")}, check: func(t *testing.T, np *nowPlayingModel, r *episodeResumeMsg) {
					if !np.hasTrack || np.trackURI != trackX {
						t.Errorf("error reply changed state: hasTrack=%v uri=%q", np.hasTrack, np.trackURI)
					}
					wantPlayback(t, np, true, 1000)
				}},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			np := newNowPlaying(t.Context(), spotify.New(&http.Client{}, spotify.WithPreferredDevice(tc.preferred)))
			if tc.setup != nil {
				tc.setup(np)
			}
			for i, step := range tc.steps {
				step.msg.seq = uint64(i + 1)
				resume := runResume(t, np.handlePlayerState(step.msg))
				if np.appliedPollSeq != step.msg.seq {
					t.Errorf("step %d: appliedPollSeq = %d, want %d", i, np.appliedPollSeq, step.msg.seq)
				}
				if step.check != nil {
					t.Run(fmt.Sprintf("step%d", i), func(t *testing.T) { step.check(t, np, resume) })
				}
			}
		})
	}
}

// newRateLimitedClient returns a client whose shared cooldown has been
// armed by one real 429 through the stubbed transport.
func newRateLimitedClient(t *testing.T) *spotify.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	client := spotify.New(&http.Client{Transport: &testutil.RewriteTransport{
		Base:   srv.Client().Transport,
		Target: srv.URL,
	}})
	np := &nowPlayingModel{client: client, ctx: context.Background()}
	np.pollState()()
	if !client.IsRateLimited() {
		t.Fatal("a 429 did not arm the cooldown")
	}
	return client
}

func TestPollInterval_Priority(t *testing.T) {
	limited := newRateLimitedClient(t)
	recent := time.Now
	stale := func() time.Time { return time.Now().Add(-time.Minute) }

	tests := []struct {
		name     string
		client   *spotify.Client
		np       nowPlayingModel
		action   func() time.Time
		want     time.Duration
		cooldown bool
	}{
		{
			name:     "cooldown beats everything",
			client:   limited,
			np:       nowPlayingModel{hasTrack: true, playing: true, progressMs: 299_000, durationMs: 300_000},
			action:   recent,
			cooldown: true,
		},
		{
			name:   "no track beats recent action",
			np:     nowPlayingModel{hasTrack: false, playing: true},
			action: recent,
			want:   10 * time.Second,
		},
		{
			name:   "recent action beats paused and near end",
			np:     nowPlayingModel{hasTrack: true, playing: false, progressMs: 299_000, durationMs: 300_000},
			action: recent,
			want:   5 * time.Second,
		},
		{
			name:   "zero lastUserAction counts as idle",
			np:     nowPlayingModel{hasTrack: true, playing: false},
			action: func() time.Time { return time.Time{} },
			want:   15 * time.Second,
		},
		{
			name:   "paused beats near end",
			np:     nowPlayingModel{hasTrack: true, playing: false, progressMs: 299_000, durationMs: 300_000},
			action: stale,
			want:   15 * time.Second,
		},
		{
			name:   "near end",
			np:     nowPlayingModel{hasTrack: true, playing: true, progressMs: 300_000 - nearEndThresholdMs + 1, durationMs: 300_000},
			action: stale,
			want:   3 * time.Second,
		},
		{
			name:   "exactly at the near-end threshold is not near end",
			np:     nowPlayingModel{hasTrack: true, playing: true, progressMs: 300_000 - nearEndThresholdMs, durationMs: 300_000},
			action: stale,
			want:   10 * time.Second,
		},
		{
			name:   "default while playing mid-track",
			np:     nowPlayingModel{hasTrack: true, playing: true, progressMs: 60_000, durationMs: 300_000},
			action: stale,
			want:   10 * time.Second,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			np := tc.np
			np.client = tc.client
			if np.client == nil {
				np.client = &spotify.Client{}
			}
			np.lastUserAction = tc.action()
			got := np.pollInterval()
			if tc.cooldown {
				// wait + 1s: past the deadline, and longer than any
				// non-cooldown interval since the minimum backoff is 30s.
				wait := np.client.RateLimitWait()
				if got <= wait || got > wait+2*time.Second || got <= 15*time.Second {
					t.Errorf("pollInterval = %v, want just past the cooldown (%v)", got, wait)
				}
				return
			}
			if got != tc.want {
				t.Errorf("pollInterval = %v, want %v", got, tc.want)
			}
		})
	}
}
