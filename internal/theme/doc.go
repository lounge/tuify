// Package theme owns the application color palette and the bridge from
// user config (a Theme struct deserialized from config.json) into the
// mutable lipgloss colors used by package ui. The visualizers package
// still has hardcoded colors today; migrating those to read from this
// palette is a separate piece of work.
//
// Lipgloss styles capture their colors by value at construction time, so
// callers must construct/rebuild any styles that reference these vars
// AFTER Apply has run. The ui.RebuildStyles function in package ui takes
// care of that for the main UI palette.
//
// Default returns the current palette as a Theme so first-time setup can
// write every overridable role to config.json. Validate checks every
// non-empty hex value before Apply, naming each offending path so users
// can fix typos without grepping the source.
package theme
