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
package theme

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Palette — exported, mutable. Reassigned by Apply at startup.
var (
	Primary       = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#58f796"}
	Secondary     = lipgloss.AdaptiveColor{Light: "#6232CC", Dark: "#b48eff"}
	Muted         = lipgloss.AdaptiveColor{Light: "#9B9B9B", Dark: "#626262"}
	Subtle        = lipgloss.AdaptiveColor{Light: "#6C6C6C", Dark: "#8a8a8a"}
	Dim           = lipgloss.AdaptiveColor{Light: "#BCBCBC", Dark: "#444444"}
	Error         = lipgloss.AdaptiveColor{Light: "#FF0000", Dark: "#ff0087"}
	Text          = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#dddddd"}
	TextDim       = lipgloss.AdaptiveColor{Light: "#A49FA5", Dark: "#777777"}
	Tip           = lipgloss.AdaptiveColor{Light: "#D4A017", Dark: "#FFD866"}
	OnPrimary     = lipgloss.AdaptiveColor{Light: "#000000", Dark: "#000000"}
	GradientStart = lipgloss.AdaptiveColor{Light: "#e4d4f7", Dark: "#110a24"}
	GradientEnd   = lipgloss.AdaptiveColor{Light: "#f8f5fc", Dark: "#040208"}
)

// Variant is a {light, dark} hex pair for one palette role. Empty string
// fields preserve the existing palette default at Apply time, so users
// can override one mode without clobbering the other.
type Variant struct {
	Light string `json:"light,omitempty"`
	Dark  string `json:"dark,omitempty"`
}

// Theme is the user-overridable palette. Zero-valued entries preserve
// package defaults. omitzero (Go 1.24+) is used on the outer struct
// fields because omitempty is a no-op on struct values — it would emit
// the role even when fully empty.
type Theme struct {
	Primary       Variant `json:"primary,omitzero"`
	Secondary     Variant `json:"secondary,omitzero"`
	Muted         Variant `json:"muted,omitzero"`
	Subtle        Variant `json:"subtle,omitzero"`
	Dim           Variant `json:"dim,omitzero"`
	Error         Variant `json:"error,omitzero"`
	Text          Variant `json:"text,omitzero"`
	TextDim       Variant `json:"text_dim,omitzero"`
	Tip           Variant `json:"tip,omitzero"`
	OnPrimary     Variant `json:"on_primary,omitzero"`
	GradientStart Variant `json:"gradient_start,omitzero"`
	GradientEnd   Variant `json:"gradient_end,omitzero"`
}

// Default returns a Theme populated with the current palette defaults.
// Used by the first-time setup flow so a freshly written config.json
// shows the user every overridable role with its current value.
func Default() Theme {
	return Theme{
		Primary:       toVariant(Primary),
		Secondary:     toVariant(Secondary),
		Muted:         toVariant(Muted),
		Subtle:        toVariant(Subtle),
		Dim:           toVariant(Dim),
		Error:         toVariant(Error),
		Text:          toVariant(Text),
		TextDim:       toVariant(TextDim),
		Tip:           toVariant(Tip),
		OnPrimary:     toVariant(OnPrimary),
		GradientStart: toVariant(GradientStart),
		GradientEnd:   toVariant(GradientEnd),
	}
}

// Apply reassigns palette package vars from t. Empty hex strings preserve
// the existing value, so users can override one role (or one mode of a
// role) without touching the others.
func Apply(t Theme) {
	apply(&Primary, t.Primary)
	apply(&Secondary, t.Secondary)
	apply(&Muted, t.Muted)
	apply(&Subtle, t.Subtle)
	apply(&Dim, t.Dim)
	apply(&Error, t.Error)
	apply(&Text, t.Text)
	apply(&TextDim, t.TextDim)
	apply(&Tip, t.Tip)
	apply(&OnPrimary, t.OnPrimary)
	apply(&GradientStart, t.GradientStart)
	apply(&GradientEnd, t.GradientEnd)
}

func apply(target *lipgloss.AdaptiveColor, v Variant) {
	if v.Light != "" {
		target.Light = v.Light
	}
	if v.Dark != "" {
		target.Dark = v.Dark
	}
}

func toVariant(c lipgloss.AdaptiveColor) Variant {
	return Variant{Light: c.Light, Dark: c.Dark}
}

// hexColorRE matches "#" followed by 3 or 6 hex digits — the two forms
// lipgloss/termenv parses as RGB. Empty strings are validated separately
// so they can preserve the existing default.
var hexColorRE = regexp.MustCompile(`^#([0-9A-Fa-f]{3}|[0-9A-Fa-f]{6})$`)

// Validate checks that every non-empty hex value in t is well-formed.
// On failure the error names every offending path (e.g. "theme.primary.light")
// and the offending value, so the user can spot fixes without searching.
func Validate(t Theme) error {
	var errs []string
	check := func(role string, v Variant) {
		if v.Light != "" && !hexColorRE.MatchString(v.Light) {
			errs = append(errs, fmt.Sprintf("theme.%s.light = %q (expected #RGB or #RRGGBB)", role, v.Light))
		}
		if v.Dark != "" && !hexColorRE.MatchString(v.Dark) {
			errs = append(errs, fmt.Sprintf("theme.%s.dark = %q (expected #RGB or #RRGGBB)", role, v.Dark))
		}
	}
	check("primary", t.Primary)
	check("secondary", t.Secondary)
	check("muted", t.Muted)
	check("subtle", t.Subtle)
	check("dim", t.Dim)
	check("error", t.Error)
	check("text", t.Text)
	check("text_dim", t.TextDim)
	check("tip", t.Tip)
	check("on_primary", t.OnPrimary)
	check("gradient_start", t.GradientStart)
	check("gradient_end", t.GradientEnd)
	if len(errs) > 0 {
		return fmt.Errorf("invalid theme color(s):\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}
