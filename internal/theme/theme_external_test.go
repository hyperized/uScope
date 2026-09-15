package theme_test

import (
	"image/color"
	"testing"

	"github.com/hyperized/uScope/internal/theme"
)

// wantAlpha is the alpha every palette colour must carry. The framebuffer has
// no alpha channel, so anything translucent would be silently flattened, and
// that is worth catching in a test rather than on the panel.
const wantAlpha = 0xFF

// namedColour pairs a palette field with the name a failure should report.
type namedColour struct {
	name string
	col  color.RGBA
}

// nightColours lists every field of theme.Night once, so the opacity and
// distinctness checks below can both table-drive off the same list.
func nightColours() []namedColour {
	return []namedColour{
		{name: "Field", col: theme.Night.Field},
		{name: "Ink", col: theme.Night.Ink},
		{name: "Muted", col: theme.Night.Muted},
		{name: "Accent", col: theme.Night.Accent},
		{name: "Rule", col: theme.Night.Rule},
		{name: "AltLow", col: theme.Night.AltLow},
		{name: "AltMid", col: theme.Night.AltMid},
		{name: "AltHigh", col: theme.Night.AltHigh},
	}
}

func TestNightIsFullyOpaque(t *testing.T) {
	t.Parallel()

	for _, testCase := range nightColours() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.col.A; got != wantAlpha {
				t.Errorf("%s.A = %#x, want %#x", testCase.name, got, wantAlpha)
			}
		})
	}
}

// TestNightCoreColoursAreDistinct checks the five colours a reader has to be
// able to tell apart at a glance: the field, the ink, the muted chrome, the
// one accent, and the rule hairlines. Two of these landing on the same value
// would be invisible in the code but obvious on the panel.
func TestNightCoreColoursAreDistinct(t *testing.T) {
	t.Parallel()

	core := []namedColour{
		{name: "Field", col: theme.Night.Field},
		{name: "Ink", col: theme.Night.Ink},
		{name: "Muted", col: theme.Night.Muted},
		{name: "Accent", col: theme.Night.Accent},
		{name: "Rule", col: theme.Night.Rule},
	}

	assertPairwiseDistinct(t, core)
}

// TestNightAltitudeBandsAreDistinct checks the three altitude bands. They are
// unused until the radar slice, but a scope that cannot tell a low aircraft
// from a high one by colour has lost the one channel it has for altitude.
func TestNightAltitudeBandsAreDistinct(t *testing.T) {
	t.Parallel()

	bands := []namedColour{
		{name: "AltLow", col: theme.Night.AltLow},
		{name: "AltMid", col: theme.Night.AltMid},
		{name: "AltHigh", col: theme.Night.AltHigh},
	}

	assertPairwiseDistinct(t, bands)
}

// assertPairwiseDistinct fails the test if any two colours in the list are
// equal, one subtest per pair.
func assertPairwiseDistinct(t *testing.T, colours []namedColour) {
	t.Helper()

	for first := range colours {
		for second := first + 1; second < len(colours); second++ {
			left, right := colours[first], colours[second]

			t.Run(left.name+" vs "+right.name, func(t *testing.T) {
				t.Parallel()

				if left.col == right.col {
					t.Errorf("%s and %s are the same colour %v, want them distinct", left.name, right.name, left.col)
				}
			})
		}
	}
}

// luminance is a rough brightness for a palette colour: not the perceptual
// formula, just the sum of the three channels, which is enough to order
// three colours from darkest to lightest.
func luminance(col color.RGBA) int {
	return int(col.R) + int(col.G) + int(col.B)
}

// TestNightInkIsLighterThanField checks the one relationship that makes the
// palette readable at all: the ink has to sit above the field, and the muted
// chrome has to sit between them. A theme where the "ink" is darker than the
// background it sits on would not render as text, it would render as nothing.
func TestNightInkIsLighterThanField(t *testing.T) {
	t.Parallel()

	field := luminance(theme.Night.Field)
	muted := luminance(theme.Night.Muted)
	ink := luminance(theme.Night.Ink)

	if field >= muted {
		t.Errorf("Field luminance %d >= Muted luminance %d, want Field darker", field, muted)
	}

	if muted >= ink {
		t.Errorf("Muted luminance %d >= Ink luminance %d, want Muted darker than Ink", muted, ink)
	}
}
