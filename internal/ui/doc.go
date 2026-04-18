// Package ui is the Bubble Tea-based terminal UI for tuify.
//
// # Architecture
//
// The package is a shell + screens + submodels composition:
//
//   - The shell (app.go, app_update.go, app_keys.go, app_view.go,
//     app_handlers.go, app_commands.go) owns the Model, the view stack,
//     the event loop, and every Spotify/clipboard/device side effect.
//   - Screens are individual views living on the view stack — homeView,
//     playlistView, trackView, podcastView, episodeView, searchView.
//     Each owns its local state (cursor, fetched items, filter query)
//     and renders itself. Screens never touch Model directly.
//   - Submodels are long-lived state owned by Model that transcend the
//     view stack: nowPlayingModel (playback + marquee scroll),
//     visualizerModel (viz pane + async image/lyrics loaders),
//     deviceSelectorModel.
//
// # View → shell communication
//
// Views emit intent messages (see app_intents.go) rather than mutating
// Model directly. The shell's Update switch interprets each intent by
// constructing the target view or dispatching the corresponding command,
// so the view → shell dependency is strictly one-way. Concretely:
//
//   - User presses Enter on a playlist → playlistView.OnEnter emits
//     openTracksIntent{id, name} → shell creates trackView and pushes.
//   - User selects a track in trackView → OnEnter emits playItemIntent
//     → shell dispatches withDevice-wrapped Spotify Play call.
//
// # Capability interfaces
//
// The shell dispatches work via small capability interfaces rather than
// type-asserting against concrete view types (see common.go):
//
//   - view (Init/Update/View/SetSize/Breadcrumb) — every screen
//   - listProvider, searchableListProvider — for shared key handling
//   - syncableView — for "sync selection to playing track"
//   - enterable — for Enter-key activation
//   - scrollable, clickable — for mouse wheel / click dispatch
//   - backable — for views that consume "go back" internally
//   - searchAware — for views hosting a search-input mode
//
// Adding a new screen means implementing the capabilities it cares about;
// no edits to handleMouse/handleBack/handleKeyMsg are needed.
//
// # Rendering
//
// Every rendered frame is wrapped in bubblezone.Scan so mouse clicks can
// be resolved back to zone-marked items (lists mark each row by Spotify
// URI; the home view marks each menu tab by name). The list delegate
// (zoneListDelegate in styles.go) does the per-row marking transparently.
//
// # Lifetime
//
// NewModel takes a root context from bootstrap.Run that cancels on app
// exit. That context is propagated to nowPlayingModel, visualizerModel,
// and every view constructor so long-running operations (polls, HTTP
// fetches, image/lyrics downloads) cancel cleanly at shutdown rather
// than running to their per-op timeout.
package ui
