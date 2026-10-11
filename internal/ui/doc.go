// Package ui is the Bubble Tea-based terminal UI for tuify.
//
// # Architecture
//
// The package is a shell + screens + submodels composition:
//
//   - The shell (app.go, app_update.go, app_keys.go, app_view.go,
//     app_handlers.go, app_commands.go, app_tickers.go) owns the Model,
//     the view stack, the event loop, and every Spotify/clipboard/device
//     side effect.
//   - Screens are individual views living on the view stack — homeView,
//     playlistView, trackView, podcastView, episodeView, searchView.
//     Each owns its local state (cursor, fetched items, filter query)
//     and renders itself. Screens never touch Model directly.
//   - Submodels are long-lived state owned by Model that transcend the
//     view stack: nowPlayingModel (playback + marquee scroll),
//     visualizerModel (viz pane + async image/lyrics loaders),
//     deviceSelectorModel.
//
// # On-demand tick chains
//
// The loading spinner and the now-playing label marquee tick only while
// something on screen uses them (a list's loading row does not count
// while help, the visualizer, mini mode or the device overlay hides it). Each tick handler stops rescheduling once
// idle, and Model.Update calls resumeTickers after every message to
// restart a chain the new state needs, so loading and resize paths never
// have to start a tick themselves. A new spinning UI element must be
// covered by needsSpinner (app_tickers.go) or its spinner will not move.
//
// # View → shell communication
//
// Views emit intent messages (see app_intents.go) rather than mutating
// Model directly. The shell's Update switch interprets each intent by
// constructing the target view or dispatching the corresponding command,
// so the view → shell dependency is strictly one-way. Concretely:
//
//   - User presses Enter on a playlist → playlistView.onEnter emits
//     openTracksIntent{id, name} → shell creates trackView and pushes.
//   - User selects a track in trackView → onEnter emits playItemIntent
//     → shell dispatches withDevice-wrapped Spotify Play call.
//
// # Capability interfaces
//
// The shell dispatches work via small capability interfaces rather than
// type-asserting against concrete view types (see common.go):
//
//   - view (Update/View/SetSize/breadcrumb) — every screen. Screens that
//     load data also have an Init, which the shell calls when it pushes
//     them (lazyList provides it for the paged lists).
//   - listProvider, searchableListProvider — for shared key handling
//   - syncableView — for "sync selection to playing track"; the view is
//     told the playing context and pages for the item only when that
//     context is its own
//   - enterable — for Enter-key activation
//   - scrollable, clickable — for mouse wheel / click dispatch
//   - nearEndLoader — for paged views: after the shell moves the cursor
//     itself (wheel, half page) it asks for the next page right away
//   - closer — for views with fetches in flight; the shell calls close
//     when it pops the view, cancelling them
//   - backable — for views that consume "go back" internally
//   - searchAware — for views hosting a search-input mode
//   - inputSearcher — for views that own their search input session
//     (the API search view); the shell opens it on "/" and routes keys
//     through the session it returns
//
// Adding a new screen means implementing the capabilities it cares about;
// no edits to handleMouse/handleBack/handleKeyMsg are needed. The methods
// mirroring bubbletea (Init/Update/View) and bubbles (SetSize) keep their
// exported names; the rest are package-internal and lowercase. List each
// new screen's capabilities in the compile-time checks at the end of
// common.go: the shell finds them by type assertion, so a missing method
// would otherwise only show up as a key or click that does nothing.
//
// # Rendering
//
// Every frame that shows the current view's list is wrapped in
// bubblezone.Scan so mouse clicks can be resolved back to zone-marked items
// (lists mark each row by list id and row index, since a playlist can hold
// the same track twice; the home view marks each menu tab by name). The
// list delegate (zoneListDelegate in styles.go) does the per-row marking
// transparently and clickRow resolves a click back to the row. Help,
// visualizer, mini mode and the device overlay carry no marks and skip the
// scan; handleMouse drops pointer events while they are up so the previous
// list frame's zones can't be hit, and the Enter and "/" keys are ignored
// for the same reason.
//
// # Optimistic playback
//
// Play/pause and shuffle flip in the bar before Spotify replies (see
// pendingFlip in nowplaying.go). A failed reply reverts its own flip and
// no other. A successful one leaves the flip up until a poll agrees, for
// at most flipSettleWindow: a poll that still disagrees after that is the
// state, since another client changed it or the device ignored the
// command.
//
// # Visualizer fetches
//
// Album art and lyrics come from third parties (Spotify's image CDN,
// lrclib.net, genius.com), so visualizerModel fetches them only while the
// pane is open. A track change with the pane closed resets the
// visualizers and records the track; opening the pane fetches what is
// not cached, and a fetch already in flight for the same image or track
// is left to finish rather than restarted. An item that is not a track
// or episode (an ad, a local file), or nothing playing at all, puts the
// pane in its "No track" state; playback coming back sets it up again,
// from the caches when it is the same track.
//
// The image and lyrics loaders (asyncLoader in visualizer_cache.go) run
// each fetch on a goroutine that sends exactly one result on that
// operation's own 1-slot channel. advance drains the channel on the next
// visualizer tick, inside Update, so Update stays the only mutator: the
// goroutine captures only values (client, URL, track ID, channel), never
// the model. A tea.Cmd is not used because a fetch must be cancellable
// when the track changes (the loader holds its context's cancel func)
// and a late result from a cancelled fetch must land nowhere: it goes to
// an orphaned channel nobody reads, and results are also matched against
// the current URL or track ID before they are applied.
//
// # Lifetime
//
// NewModel takes a root context from bootstrap.Run that cancels on app
// exit. It collects the ModelOptions first, then constructs
// nowPlayingModel and visualizerModel once, passing that context (and the
// audio source) to their constructors; the home view fetches nothing and
// takes only its size and the vim-mode flag. Every fetching view
// constructor gets the context too, so long-running operations (polls,
// HTTP fetches, image/lyrics downloads) cancel cleanly at shutdown rather
// than running to their per-op timeout.
package ui
