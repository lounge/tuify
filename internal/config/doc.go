// Package config manages the tuify config file. It resolves the
// platform-appropriate directory (respecting XDG_CONFIG_HOME on
// Unix-likes), loads and validates the on-disk JSON, and writes updates
// with safe permissions.
//
// The zero-value Config is not usable — callers construct a Config via
// Load (existing file) or first-time setup in bootstrap, then call
// Validate before handing it off. Dir is exported so other packages
// (auth, librespot cache, debug log) can place their files next to
// config.json without re-implementing the platform-choice logic.
package config
