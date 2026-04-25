package theme

import (
	"strings"
	"testing"
)

// resetPalette snapshots the current palette and restores it on test
// cleanup. Tests that call Apply must use this so global mutations don't
// leak between cases.
func resetPalette(t *testing.T) Theme {
	t.Helper()
	saved := Default()
	t.Cleanup(func() { Apply(saved) })
	return saved
}

func TestValidate_AcceptsEmpty(t *testing.T) {
	if err := Validate(Theme{}); err != nil {
		t.Fatalf("empty theme: %v", err)
	}
}

func TestValidate_AcceptsValidHex(t *testing.T) {
	cases := []string{
		"#000",
		"#FFF",
		"#abc",
		"#000000",
		"#874BFD",
		"#aBcDeF",
	}
	for _, hex := range cases {
		t.Run(hex, func(t *testing.T) {
			err := Validate(Theme{Primary: Variant{Light: hex, Dark: hex}})
			if err != nil {
				t.Errorf("Validate(%q): unexpected error: %v", hex, err)
			}
		})
	}
}

func TestValidate_RejectsInvalidHex(t *testing.T) {
	cases := []struct {
		name, hex string
	}{
		{"missing hash", "874BFD"},
		{"too short", "#12"},
		{"4 digit", "#1234"},
		{"5 digit", "#12345"},
		{"7 digit", "#1234567"},
		{"non-hex chars", "#GGGGGG"},
		{"trailing space", "#874BFD "},
		{"named color", "red"},
		{"ansi numeric", "120"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(Theme{Primary: Variant{Light: tc.hex}})
			if err == nil {
				t.Fatalf("Validate(%q): expected error, got nil", tc.hex)
			}
			if !strings.Contains(err.Error(), tc.hex) {
				t.Errorf("error should quote bad value %q, got: %v", tc.hex, err)
			}
			if !strings.Contains(err.Error(), "theme.primary.light") {
				t.Errorf("error should name the JSON path, got: %v", err)
			}
		})
	}
}

func TestValidate_AggregatesMultipleErrors(t *testing.T) {
	t1 := Theme{
		Primary:   Variant{Light: "bad1", Dark: "#874BFD"},
		Secondary: Variant{Dark: "bad2"},
		Tip:       Variant{Light: "bad3"},
	}
	err := Validate(t1)
	if err == nil {
		t.Fatal("expected aggregated error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"theme.primary.light",
		"theme.secondary.dark",
		"theme.tip.light",
		`"bad1"`, `"bad2"`, `"bad3"`,
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q, got:\n%v", want, err)
		}
	}
}

func TestApply_EmptyPreservesDefaults(t *testing.T) {
	saved := resetPalette(t)
	Apply(Theme{})
	if got := Default(); got != saved {
		t.Errorf("empty Apply mutated palette\n got: %+v\nwant: %+v", got, saved)
	}
}

func TestApply_PartialOverrideTouchesOnlyTargets(t *testing.T) {
	saved := resetPalette(t)

	Apply(Theme{
		Primary: Variant{Dark: "#ff0000"},
	})

	got := Default()
	want := saved
	want.Primary.Dark = "#ff0000"

	if got != want {
		t.Errorf("partial Apply mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestApply_EmptyHexPreservesPerMode(t *testing.T) {
	saved := resetPalette(t)

	// Override only Primary.Light; Primary.Dark is empty and must keep
	// its existing value.
	Apply(Theme{Primary: Variant{Light: "#123456"}})

	if Primary.Light != "#123456" {
		t.Errorf("Primary.Light: got %q, want %q", Primary.Light, "#123456")
	}
	if Primary.Dark != saved.Primary.Dark {
		t.Errorf("Primary.Dark: got %q, want %q (unchanged)", Primary.Dark, saved.Primary.Dark)
	}
}

func TestDefault_CapturesCurrentState(t *testing.T) {
	resetPalette(t)

	// Apply a known mutation, then confirm Default() reflects it.
	Apply(Theme{Tip: Variant{Light: "#abcdef", Dark: "#123456"}})

	d := Default()
	if d.Tip.Light != "#abcdef" || d.Tip.Dark != "#123456" {
		t.Errorf("Default() did not reflect Apply: got %+v", d.Tip)
	}
}

func TestDefault_ApplyRoundTrip(t *testing.T) {
	resetPalette(t)

	// Default() of a fresh palette, applied, then re-read, must equal
	// itself — Apply must be a no-op on its own snapshot.
	d := Default()
	Apply(d)
	if got := Default(); got != d {
		t.Errorf("Default → Apply round-trip drifted\n got: %+v\nwant: %+v", got, d)
	}
}
