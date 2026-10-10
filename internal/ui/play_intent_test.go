package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lounge/tuify/internal/spotify"
)

// The librespot reconnect handler reads the play intent to decide whether
// to resume after a dropped session, so the UI must record the user's own
// play/pause actions and playback seen running, but never take a paused
// poll as intent: a broken session reports exactly that.

func applyMsg(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	return updated.(Model)
}

func TestPlayIntent_FollowsUserActions(t *testing.T) {
	tests := []struct {
		name    string
		playing bool // state before the key
		key     tea.KeyMsg
		want    spotify.PlayIntent
	}{
		{"space while playing pauses", true, runeKey(" "), spotify.PlayIntentPaused},
		{"space while paused plays", false, runeKey(" "), spotify.PlayIntentPlaying},
		{"next plays", false, runeKey("n"), spotify.PlayIntentPlaying},
		{"previous plays", false, runeKey("p"), spotify.PlayIntentPlaying},
		{"stop pauses", true, runeKey("s"), spotify.PlayIntentPaused},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newIntentTestModel()
			m.nowPlaying.hasTrack, m.nowPlaying.playing = true, tc.playing
			m, _ = pressKeys(t, m, tc.key)
			if got := m.client.PlayIntent(); got != tc.want {
				t.Errorf("intent = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPlayIntent_PlayingAnItemPlays(t *testing.T) {
	for name, msg := range map[string]tea.Msg{
		"item":  playItemIntent{itemURI: "spotify:track:1"},
		"queue": playQueueIntent{uris: []string{"spotify:track:1"}},
	} {
		t.Run(name, func(t *testing.T) {
			m := newIntentTestModel()
			m.client.SetPlayIntent(false)
			m = applyMsg(t, m, msg)
			if got := m.client.PlayIntent(); got != spotify.PlayIntentPlaying {
				t.Errorf("intent = %v, want playing", got)
			}
		})
	}
}

func TestPlayIntent_PollsOnlyRecordPlaying(t *testing.T) {
	m := newIntentTestModel()
	if got := m.client.PlayIntent(); got != spotify.PlayIntentUnknown {
		t.Fatalf("fresh intent = %v, want unknown", got)
	}
	m = applyMsg(t, m, playerStateMsg{state: pstate(intentTrack, true, 1000)})
	if got := m.client.PlayIntent(); got != spotify.PlayIntentPlaying {
		t.Fatalf("after a playing poll intent = %v, want playing", got)
	}
	// A dropped session reports paused; the intent must survive it, and a
	// poll with nothing playing at all as well.
	m = applyMsg(t, m, playerStateMsg{state: pstate(intentTrack, false, 2000)})
	m = applyMsg(t, m, playerStateMsg{state: nil})
	if got := m.client.PlayIntent(); got != spotify.PlayIntentPlaying {
		t.Errorf("after paused and empty polls intent = %v, want still playing", got)
	}
}

// While a pause is pending, a poll taken before it landed still reports
// playing; that must not undo the pause the user just asked for.
func TestPlayIntent_PendingPauseIgnoresStalePlayingPoll(t *testing.T) {
	m := newIntentTestModel()
	// Same track as the poll: a track change would accept the fresh state.
	m.nowPlaying.trackURI = intentTrack
	m.nowPlaying.hasTrack, m.nowPlaying.playing = true, true
	m, _ = pressKeys(t, m, runeKey(" "))
	m = applyMsg(t, m, playerStateMsg{state: pstate(intentTrack, true, 1000)})
	if got := m.client.PlayIntent(); got != spotify.PlayIntentPaused {
		t.Errorf("intent = %v, want paused while the pause is pending", got)
	}
}

func TestPlayIntent_FailedPauseRestoresPlaying(t *testing.T) {
	m := newIntentTestModel()
	m.nowPlaying.hasTrack, m.nowPlaying.playing = true, true
	m, _ = pressKeys(t, m, runeKey(" "))
	updated, _ := m.handlePlaybackResult(playbackResultMsg{op: opPlayPause, err: errTest, flip: m.nowPlaying.playPausePending})
	if got := updated.(Model).client.PlayIntent(); got != spotify.PlayIntentPlaying {
		t.Errorf("intent = %v after the pause failed, want playing", got)
	}
}

const intentTrack = "spotify:track:intent"
