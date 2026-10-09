// Package config manages the tuify config file. Dir resolves the config
// directory: $XDG_CONFIG_HOME/tuify when that is set, otherwise
// ~/.config/tuify, on every OS including macOS and Windows. The package
// loads and validates the on-disk JSON and writes updates with safe
// permissions.
//
// The zero-value Config is not usable — callers construct a Config via
// Load (existing file) or first-time setup in bootstrap, then call
// Validate before handing it off. Dir is exported so other packages
// (auth, librespot cache, debug log) can place their files next to
// config.json without re-implementing the path logic.
//
// WriteFileAtomic is the one way config.json and token.json are written:
// via a synced temp file renamed into place, with 0600 permissions, so a
// crash mid-write can't leave a truncated file behind. Append-style files
// in the same directory (debug.log, the librespot cache) use plain os
// calls, since a torn write there costs nothing.
package config
