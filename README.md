# Tuify

A terminal-based Spotify client. Browse playlists, search for music and podcasts, control playback — **Spotify without all the noise.**

![CI](https://github.com/lounge/tuify/actions/workflows/ci.yml/badge.svg)
![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![Windows](https://img.shields.io/badge/Windows-0078D6?logo=windows&logoColor=white)
![macOS](https://img.shields.io/badge/macOS-000000?logo=apple&logoColor=white)
![Linux](https://img.shields.io/badge/Linux-FCC624?logo=linux&logoColor=black)

![Tuify screenshot](img/recording_2.gif)
![Tuify visualizers](img/visualizers_2.gif)

## Features

- **Playback Control** — Play, pause, skip, previous, shuffle, seek
- **Playlists** — Browse and play your Spotify playlists
- **Podcasts** — Browse saved shows and episodes
- **Search** — Find tracks, episodes, artists, albums, and shows
- **Now Playing** — Live progress bar, track info, shuffle state
- **Visualizers** — Album art, spectrum analyzer, starfield, oscillogram, and Milkdrop-style presets
- **Lyrics** — Fetches and displays lyrics from Genius.com
- **Dark & Light Terminals** — Adaptive color palette that adjusts automatically

## Requirements

- A **Spotify Premium** account
- A [Spotify Developer App](https://developer.spotify.com/dashboard)

## Install

### macOS / Linux

```bash
brew install lounge/tap/tuify
```

### Windows

Install [Scoop](https://github.com/ScoopInstaller/scoop?tab=readme-ov-file#installation), then:

```powershell
scoop bucket add lounge https://github.com/lounge/scoop-bucket
scoop install tuify
```

### Direct download

Pre-built binaries for all platforms are available on the [Releases](https://github.com/lounge/tuify/releases) page.

## Getting Started

1. Go to https://developer.spotify.com/dashboard and create an app
2. Set the redirect URI to `http://127.0.0.1:4444/callback`
3. Check the **Web API** checkbox
4. Copy your **Client ID**
5. Run `tuify` — it will ask for your Client ID on first launch
6. A browser window will open to authorize with Spotify

## Keybindings

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
| `c` | Copy track link |
| `/` | Search |
| `v` | Toggle visualizer |
| `←` / `→` | Cycle visualizers |
| `h` | Show help overlay |
| `q` | Quit |

### Search Shortcuts

Type a prefix in the search bar to filter by type:

| Prefix | Searches |
|--------|----------|
| `t:` | Tracks (default) |
| `e:` | Episodes |
| `a:` | Artists |
| `l:` | Albums |
| `s:` | Shows |

### Vim Mode

Enable vim-style keybindings by setting `"vim_mode": true` in your config file.

| Key | Action |
|-----|--------|
| `h` / `l` | Go back / select |
| `j` / `k` | Cursor down / up |
| `g` / `G` | Jump to first / last item |
| `Ctrl+d` / `Ctrl+u` | Half-page down / up |
| `,` / `.` | Seek backward / forward |
| `?` | Show help overlay |

## Visualizers

| Visualizer | Requires Librespot (subprocess) |
|------------|--------------------|
| Album Art | No |
| Lyrics | No |
| Spectrum | Yes |
| Starfield | Yes |
| Oscillogram | Yes |
| Milkdrop Spiral | Yes |
| Milkdrop Tunnel | Yes |
| Milkdrop Kaleidoscope | Yes |
| Milkdrop Ripple | Yes |

Album Art and Lyrics work out of the box. The audio-reactive visualizers require [librespot](https://github.com/librespot-org/librespot) — see the section below.

## Librespot (Optional)

[Librespot](https://github.com/librespot-org/librespot) is an open-source Spotify Connect client. Installing it unlocks direct audio streaming and all audio-reactive visualizers.

### Setup

1. Install [librespot](https://github.com/librespot-org/librespot) and make sure it's in your `PATH`
2. Set `"enable_librespot": true` in `~/.config/tuify/config.json`
3. Restart tuify — it will connect as a Spotify device automatically

If the connection drops, tuify detects the failure, restarts librespot, and transfers playback back automatically.

### Librespot Config

Add these to `~/.config/tuify/config.json`:

| Option | Default | Description |
|--------|---------|-------------|
| `enable_librespot` | `true` | Enable librespot integration |
| `librespot_path` | `"librespot"` | Path to librespot binary |
| `device_name` | `"tuify"` | Spotify Connect device name |
| `bitrate` | `320` | Audio bitrate (96, 160, or 320 kbps) |
| `audio_backend` | `"subprocess"` | Audio backend (see below) |
| `spotify_username` | `""` | Spotify username for direct auth |

### Audio Backends

Only `"subprocess"` enables audio-reactive visualizers.

| Backend | Description |
|---------|-------------|
| **subprocess** | Audio piped through tuify for playback and visualizers. **Recommended.** |
| **rodio** | Cross-platform audio output. Librespot's default. |
| **pipe** | Outputs raw PCM to stdout. |
| **alsa** | Direct ALSA output (Linux). |
| **pulseaudio** | Audio via PulseAudio (Linux). |

Other backends (jackaudio, portaudio, gstreamer, sdl) require enabling cargo features when building librespot. See the [librespot Audio Backends wiki](https://github.com/librespot-org/librespot/wiki/Audio-Backends).

## Configuration

All configuration is stored in `~/.config/tuify/` (or `$XDG_CONFIG_HOME/tuify/`).

| Option | Default | Description |
|--------|---------|-------------|
| `client_id` | `""` | Spotify Developer App Client ID |
| `redirect_url` | `"http://127.0.0.1:4444/callback"` | OAuth callback URL (must match your Spotify app settings) |
| `vim_mode` | `false` | Enable vim-style keybindings |

## Logs

Tuify writes a debug log to `~/.config/tuify/debug.log` on each run. The log is overwritten every time you start tuify. Check this file if something isn't working as expected.

---

## Development

### Build from source

Requires Go 1.26+. On Linux, also install `libasound2-dev`.

```bash
git clone https://github.com/lounge/tuify.git
cd tuify
go build
go test ./...
```

### Architecture

| Package | Description |
|---------|-------------|
| `internal/ui` | TUI views, components, and visualizers ([Bubble Tea](https://github.com/charmbracelet/bubbletea) + [Lip Gloss](https://github.com/charmbracelet/lipgloss)) |
| `internal/spotify` | Spotify Web API client ([zmb3/spotify](https://github.com/zmb3/spotify)) |
| `internal/audio` | Real-time audio pipeline — FFT, binary protocol, receiver ([oto](https://github.com/ebitengine/oto)) |
| `internal/librespot` | [Librespot](https://github.com/librespot-org/librespot) subprocess lifecycle |
| `internal/lyrics` | Genius.com lyrics scraping |
| `internal/auth` | OAuth2 PKCE authentication |
| `internal/config` | Configuration management |

## TODO

- make the milkdrop vizs match the ui colors more
- Check light mode colors. they are good enough...
- Maybe themes? Probably not :)
- Make it work when connected to external devices (Sonos) - doesn't work for some stupid reason... (https://github.com/spotify/web-api/issues/1337).

## License

MIT
