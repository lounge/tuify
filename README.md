# Tuify

A terminal-based Spotify client written in Go. Browse playlists, search for music and podcasts, control playback, and enjoy visualizers — all from your terminal.

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)

## Features

- **Playback Control** — Play, pause, skip, previous, shuffle, seek, and device selection
- **Playlists** — Browse and play your Spotify playlists
- **Podcasts** — Browse saved shows and episodes
- **Search** — Multi-type search with prefix shortcuts:
  - `t:` Track search (default)
  - `a:` Artist → Album → Track drill-down
  - `l:` Album → Track drill-down
  - `e:` Episode search
  - `s:` Show → Episode drill-down
- **Now Playing** — Live progress bar, track info, shuffle state
- **Visualizers** — Oscillogram and starfield animations synced to playback

## Prerequisites

- Go 1.26+
- A Spotify account
- A [Spotify Developer App](https://developer.spotify.com/dashboard) with redirect URI set to `http://127.0.0.1:4444/callback`

## Install

```bash
go install github.com/fred/tuify@latest
```

Or build from source:

```bash
git clone https://github.com/fred/tuify.git
cd tuify
go build
```

## Setup

On first run, Tuify will prompt you for your Spotify Client ID:

1. Go to https://developer.spotify.com/dashboard and create an app
2. Set the redirect URI to `http://127.0.0.1:4444/callback`
3. Copy your Client ID and paste it when prompted
4. A browser window will open to authorize with Spotify

Configuration is stored in `~/.config/tuify/` (or `$XDG_CONFIG_HOME/tuify/`).

## Usage

```bash
./tuify
```

Navigate with arrow keys, Enter to select, Escape/Backspace to go back. Use the tab bar on the home screen to switch between Search, Playlists, and Podcasts.

## Project Structure

```
tuify/
├── main.go                  # Entry point and auth flow
├── internal/
│   ├── auth/                # OAuth2 PKCE authentication
│   ├── config/              # Configuration management
│   ├── spotify/             # Spotify API client wrapper
│   └── ui/
│       ├── app.go           # Main app model and routing
│       ├── search.go        # Search view
│       ├── home.go          # Home screen tabs
│       ├── nowplaying.go    # Now-playing bar
│       ├── playlist.go      # Playlist browsing
│       ├── track.go         # Track view
│       ├── podcast.go       # Podcast browsing
│       ├── episode.go       # Episode view
│       ├── progressbar.go   # Progress bar
│       ├── visualizer.go    # Visualizer controller
│       ├── styles.go        # Styling
│       ├── common.go        # Shared helpers
│       └── visualizers/     # Oscillogram & starfield
└── go.mod
```

## Built With

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [Bubbles](https://github.com/charmbracelet/bubbles) — TUI components
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — Terminal styling
- [zmb3/spotify](https://github.com/zmb3/spotify) — Spotify Web API client

## License

MIT
