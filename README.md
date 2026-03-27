# Tuify

A terminal-based Spotify client written in Go. Browse playlists, search for music and podcasts, control playback — **Spotify without all the noise.**

 Optional [librespot](https://github.com/librespot-org/librespot) integration for direct audio streaming and real-time audio-reactive visualizers.

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![Windows](https://img.shields.io/badge/Windows-0078D6?logo=windows&logoColor=white)
![macOS](https://img.shields.io/badge/macOS-000000?logo=apple&logoColor=white)
![Linux](https://img.shields.io/badge/Linux-FCC624?logo=linux&logoColor=black)

![Tuify screenshot](img/recording.gif)
![Tuify visualizers](img/visualizers.gif)

## Features

- **Playback Control** — Play, pause, skip, previous, shuffle, seek
- **Playlists** — Browse and play your Spotify playlists
- **Podcasts** — Browse saved shows and episodes
- **Search** — Multi-type search with prefix shortcuts:
  - `t:` Track search (default)
  - `e:` Episode search
  - `a:` Artist → Album → Track drill-down
  - `l:` Album → Track drill-down
  - `s:` Show → Episode drill-down
- **Now Playing** — Live progress bar, track info, shuffle state
- **Episode Resume** — Remembers playback position when switching away from episodes and resumes where you left off
- **Librespot Integration** — Optional embedded Spotify Connect player via [librespot](https://github.com/librespot-org/librespot), streaming audio directly through tuify.
- **Audio-Reactive Visualizers** — Album art, spectrum analyzer, starfield, oscillogram, and four Milkdrop-style presets — all driven by real-time FFT audio analysis when librespot is enabled (only with **subprocess** backend)
- **Lyrics** — Fetches and displays lyrics from Genius.com (best-effort match, not always exact; disabled for podcasts)
- **Help Overlay** — Press `h` (or `?` in vim mode) to view all keybindings

## Prerequisites

- Go 1.26+
- A Premium Spotify account
- A [Spotify Developer App](https://developer.spotify.com/dashboard)
- (Optional) [librespot](https://github.com/librespot-org/librespot) — for direct audio streaming and audio-reactive visualizers

## Install

```bash
go install github.com/lounge/tuify@latest
```

Or build from source:

```bash
git clone https://github.com/lounge/tuify.git
cd tuify
go build
```

## Usage

```bash
./tuify
```

## Testing

```bash
go test ./...
```

## Setup

On first run, Tuify will prompt you for your Spotify Client ID:

1. Go to https://developer.spotify.com/dashboard and create an app
2. Set the redirect URI to `http://127.0.0.1:4444/callback` (or a custom URL — see config below)
3. Check Web API checkbox
3. Copy your Client ID and paste it when prompted
4. A browser window will open to authorize with Spotify

Configuration, auth tokens, and debug logs are stored in `~/.config/tuify/` (or `$XDG_CONFIG_HOME/tuify/`).

### General Config Options

| Option | Default | Description |
|--------|---------|-------------|
| `client_id` | `""` | Spotify Developer App Client ID |
| `redirect_url` | `"http://127.0.0.1:4444/callback"` | OAuth callback URL (must match your Spotify app settings) |

### Librespot Setup

To enable librespot integration:

1. Install [librespot](https://github.com/librespot-org/librespot) and ensure it's available in your `PATH` (or set `librespot_path` in the config)
2. Set `enable_librespot` to `true` in `~/.config/tuify/config.json`

Librespot config options in `config.json`:

| Option | Default | Description |
|--------|---------|-------------|
| `enable_librespot` | `true` | Enable librespot integration |
| `librespot_path` | `"librespot"` | **Optional** Path to librespot binary |
| `device_name` | `"tuify"` | **Optional** Spotify Connect device name |
| `bitrate` | `320` | **Optional** Audio bitrate (96, 160, or 320 kbps) |
| `audio_backend` | `"subprocess"` | **Optional** Librespot audio backend (see below) |
| `spotify_username` | `""` | **Optional** Optional Spotify username for direct auth |

When enabled, tuify launches librespot with `--initial-volume 60`, `--volume-ctrl fixed`, `--disable-audio-cache`, and `--cache ~/.config/tuify/librespot` (for credential persistence across restarts).

Librespot automatically connects as the active Spotify device on startup. If the connection drops, tuify detects the failure, kills and restarts librespot, and transfers playback back automatically.

#### Audio Backends

The `audio_backend` option controls how librespot outputs audio. Only `"subprocess"` enables audio-reactive visualizers.

By default, librespot is compiled with only **rodio**, **pipe**, and **subprocess** backends. Other backends require enabling cargo features when building librespot, along with their system dependencies. See the [librespot Audio Backends wiki](https://github.com/librespot-org/librespot/wiki/Audio-Backends) for details.

| Backend | Cargo feature | System dependency | Description |
|---------|--------------|-------------------|-------------|
| **subprocess** | *(always included)* | Audio dev libs (e.g. `libasound2-dev` on Linux) | Audio is piped through tuify for playback and real-time FFT analysis. Enables all audio-reactive visualizers. Select "tuify" in "Connect to a device" in Spotify client. |
| **rodio** | *(default)* | None (uses ALSA on Linux, CoreAudio on macOS) | Cross-platform audio output. Librespot's default. |
| **pipe** | *(always included)* | None | Outputs raw PCM to stdout. Useful for piping audio to other tools. |
| **alsa** | `alsa-backend` | `libasound2-dev` (Debian) / `alsa-lib-devel` (Fedora) | Direct ALSA output, bypassing PulseAudio. Lower latency on Linux. |
| **pulseaudio** | `pulseaudio-backend` | `libpulse-dev` (Debian) / `pulseaudio-libs-devel` (Fedora) | Audio output via PulseAudio. |
| **jackaudio** | `jackaudio-backend` | JACK dev libraries | Output via JACK Audio Connection Kit. For pro audio / low-latency setups. |
| **rodiojack** | `rodiojack-backend` | JACK dev libraries | Rodio audio output routed through JACK. |
| **portaudio** | `portaudio-backend` | PortAudio dev libraries | Cross-platform audio via the PortAudio library. |
| **gstreamer** | `gstreamer-backend` | GStreamer dev libraries | Audio output via the GStreamer multimedia framework. |
| **sdl** | `sdl-backend` | SDL2 dev libraries | Audio output via SDL2. |

### Keybindings

| Key | Action |
|-----|--------|
| `Enter` | Select / play |
| `Esc` | Go back |
| `Space` | Play / pause |
| `n` | Next track |
| `p` | Previous track |
| `a` / `d` | Seek backward / forward |
| `r` | Toggle shuffle |
| `s` | Stop |
| `/` | Search |
| `v` | Toggle visualizer |
| `←` / `→` | Cycle visualizers (all 9 with librespot; album art + lyrics without) |
| `h` | Show help overlay |
| `q` | Quit |

### Vim Mode

Enable vim-style keybindings by setting `"vim_mode": true` in your config:

| Option | Default | Description |
|--------|---------|-------------|
| `vim_mode` | `false` | Enable vim-style keybindings |

All standard keybindings continue to work. Vim mode adds:

| Key | Action |
|-----|--------|
| `h` | Go back (same as `Esc`) |
| `l` | Select / drill down (same as `Enter`) |
| `j` / `k` | Cursor down / up |
| `g` / `G` | Jump to first / last item |
| `Ctrl+d` / `Ctrl+u` | Half-page down / up |
| `,` / `.` | Seek backward / forward |
| `?` | Show help overlay |

### Visualizers

| Visualizer | Description | Requires Librespot (subprocess) |
|------------|-------------|--------------------|
| Album Art | Displays track artwork as ASCII art | No |
| Lyrics | Displays lyrics fetched from Genius.com | No |
| Spectrum | Frequency spectrum analyzer with colored bars and peak indicators | Yes |
| Starfield | 3D starfield reacting to bass and intensity | Yes |
| Oscillogram | Mirrored waveform display with smooth attack/decay | Yes |
| Milkdrop Spiral | Feedback warp visualizer — rotating spiral driven by bass | Yes |
| Milkdrop Tunnel | Feedback warp visualizer — infinite rushing tunnel | Yes |
| Milkdrop Kaleidoscope | Feedback warp visualizer — mirror-symmetric sectors that morph with bass | Yes |
| Milkdrop Ripple | Feedback warp visualizer — expanding concentric ripples | Yes |

## Project Structure

```
tuify/
├── main.go                  # Entry point, librespot + audio pipeline setup
├── internal/
│   ├── auth/
│   │   └── auth.go          # OAuth2 PKCE authentication and token persistence
│   ├── audio/               # Real-time audio pipeline
│   │   ├── receiver.go      # Unix socket/TCP receiver for frequency data
│   │   ├── worker.go        # Audio playback + FFT analysis subprocess
│   │   ├── fft.go           # FFT → 64 logarithmic frequency bands
│   │   ├── protocol.go      # Binary frame encoding/decoding
│   │   └── types.go         # AudioFrame, frequency band definitions
│   ├── config/
│   │   └── config.go        # Configuration management
│   ├── lyrics/
│   │   └── genius.go        # Genius.com lyrics search and scraping
│   ├── librespot/
│   │   └── process.go       # Librespot subprocess lifecycle, broken session detection, auto-restart
│   ├── spotify/             # Spotify API client wrapper
│   │   ├── client.go        # API methods and type converters
│   │   ├── client_test.go   # Converter tests
│   │   └── api_test.go      # API tests with HTTP mocking
│   └── ui/
│       ├── app.go           # Main app model and routing
│       ├── search.go        # Search view with drill-down
│       ├── home.go          # Home screen tabs
│       ├── nowplaying.go    # Now-playing bar
│       ├── playlist.go      # Playlist browsing
│       ├── track.go         # Track view
│       ├── podcast.go       # Podcast browsing
│       ├── episode.go       # Episode view
│       ├── progressbar.go   # Gradient progress bar
│       ├── visualizer.go    # Visualizer controller
│       ├── styles.go        # Colors and styling
│       ├── common.go        # Shared view interface and types
│       ├── lazylist.go      # Paginated list with lazy loading and local search
│       └── visualizers/
│           ├── common.go        # Shared visualizer utilities
│           ├── albumart.go      # Album art display
│           ├── lyrics.go        # Lyrics display
│           ├── spectrum.go      # Spectrum analyzer (audio-reactive)
│           ├── oscillogram.go   # Waveform display (audio-reactive)
│           ├── starfield.go     # 3D starfield (audio-reactive)
│           ├── milkdrop_base.go # Milkdrop feedback warp engine
│           ├── milkdrop_spiral.go       # Spiral warp preset
│           ├── milkdrop_tunnel.go       # Tunnel warp preset
│           ├── milkdrop_kaleidoscope.go # Kaleidoscope warp preset
│           └── milkdrop_ripple.go       # Ripple warp preset
└── go.mod
```

## Tested On

- Windows 11
- macOS
- Linux (Ubuntu)

## TODO

- Copy track link?
- Check light mode colors
- Maybe themes? Probably not :)

- Make it work when connected to external devices (Sonos) - doesn't work for some stupid reason... (https://github.com/spotify/web-api/issues/1337).

## Built With

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI framework
- [Bubbles](https://github.com/charmbracelet/bubbles) — TUI components
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — Terminal styling
- [zmb3/spotify](https://github.com/zmb3/spotify) — Spotify Web API client
- [librespot](https://github.com/librespot-org/librespot) — Open-source Spotify Connect client
- [oto](https://github.com/ebitengine/oto) — Cross-platform audio playback

## License

MIT
