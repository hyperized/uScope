package theme_test

import (
	"errors"
	"image/color"
	"math"
	"testing"

	"github.com/hyperized/uScope/internal/theme"
)

// wantAlpha is the alpha every palette colour must carry. The framebuffer has
// no alpha channel, so anything translucent would be silently flattened, and
// that is worth catching in a test rather than on the panel.
const wantAlpha = 0xFF

// Field names, named once so goconst does not flag their repetition across
// nightColours, allFields and the distinctness tables below.
const (
	nameField   = "Field"
	nameInk     = "Ink"
	nameMuted   = "Muted"
	nameAccent  = "Accent"
	nameRule    = "Rule"
	nameAltLow  = "AltLow"
	nameAltMid  = "AltMid"
	nameAltHigh = "AltHigh"
	nameBand    = "Band"
	nameBandInk = "BandInk"
)

// namedColour pairs a palette field with the name a failure should report.
type namedColour struct {
	name string
	col  color.RGBA
}

// nightColours lists every field of theme.Night once, so the opacity and
// distinctness checks below can both table-drive off the same list.
func nightColours() []namedColour {
	return []namedColour{
		{name: nameField, col: theme.Night.Field},
		{name: nameInk, col: theme.Night.Ink},
		{name: nameMuted, col: theme.Night.Muted},
		{name: nameAccent, col: theme.Night.Accent},
		{name: nameRule, col: theme.Night.Rule},
		{name: nameAltLow, col: theme.Night.AltLow},
		{name: nameAltMid, col: theme.Night.AltMid},
		{name: nameAltHigh, col: theme.Night.AltHigh},
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
		{name: nameField, col: theme.Night.Field},
		{name: nameInk, col: theme.Night.Ink},
		{name: nameMuted, col: theme.Night.Muted},
		{name: nameAccent, col: theme.Night.Accent},
		{name: nameRule, col: theme.Night.Rule},
	}

	assertPairwiseDistinct(t, core)
}

// TestNightAltitudeBandsAreDistinct checks the three altitude bands. They are
// unused until the radar slice, but a scope that cannot tell a low aircraft
// from a high one by colour has lost the one channel it has for altitude.
func TestNightAltitudeBandsAreDistinct(t *testing.T) {
	t.Parallel()

	bands := []namedColour{
		{name: nameAltLow, col: theme.Night.AltLow},
		{name: nameAltMid, col: theme.Night.AltMid},
		{name: nameAltHigh, col: theme.Night.AltHigh},
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

// allFields lists every field of a palette, named, so the two tests below can
// table-drive off either theme without repeating the field list twice.
func allFields(pal theme.Palette) []namedColour {
	return []namedColour{
		{name: nameField, col: pal.Field},
		{name: nameInk, col: pal.Ink},
		{name: nameMuted, col: pal.Muted},
		{name: nameAccent, col: pal.Accent},
		{name: nameRule, col: pal.Rule},
		{name: nameAltLow, col: pal.AltLow},
		{name: nameAltMid, col: pal.AltMid},
		{name: nameAltHigh, col: pal.AltHigh},
		{name: nameBand, col: pal.Band},
		{name: nameBandInk, col: pal.BandInk},
	}
}

// namedPalette pairs a palette with the name a failure should report.
type namedPalette struct {
	name string
	pal  theme.Palette
}

// bothPalettes is Night and Paper together, for the tests that hold both to
// the same rule.
func bothPalettes() []namedPalette {
	return []namedPalette{
		{name: "Night", pal: theme.Night},
		{name: "Paper", pal: theme.Paper},
	}
}

// TestPalettesAreFullyOpaque checks every field of both palettes, including
// Band and BandInk, which the older Night-only opacity test above predates.
func TestPalettesAreFullyOpaque(t *testing.T) {
	t.Parallel()

	for _, palette := range bothPalettes() {
		for _, field := range allFields(palette.pal) {
			t.Run(palette.name+" "+field.name, func(t *testing.T) {
				t.Parallel()

				if got := field.col.A; got != wantAlpha {
					t.Errorf("%s.%s.A = %#x, want %#x", palette.name, field.name, got, wantAlpha)
				}
			})
		}
	}
}

// WCAG relative luminance: each channel is linearised from its sRGB encoding
// before the three are weighted and summed, which is what makes the result
// track how bright a colour actually looks rather than its raw byte value.
const (
	channelMax = 255.0

	gammaThreshold     = 0.03928
	gammaLinearDivisor = 12.92
	gammaOffset        = 0.055
	gammaScale         = 1.055
	gammaExponent      = 2.4

	wcagRedWeight   = 0.2126
	wcagGreenWeight = 0.7152
	wcagBlueWeight  = 0.0722

	// minLuminanceGap is how far apart Ink and Field, and BandInk and Band,
	// have to sit: below this a theme would not read as text on a background
	// on the panel.
	minLuminanceGap = 0.5
)

// relativeLuminance is the WCAG relative luminance of a colour, in [0, 1].
func relativeLuminance(col color.RGBA) float64 {
	return wcagRedWeight*linearise(col.R) + wcagGreenWeight*linearise(col.G) + wcagBlueWeight*linearise(col.B)
}

// linearise undoes one channel's sRGB gamma, per the WCAG formula.
func linearise(channel uint8) float64 {
	srgb := float64(channel) / channelMax

	if srgb <= gammaThreshold {
		return srgb / gammaLinearDivisor
	}

	return math.Pow((srgb+gammaOffset)/gammaScale, gammaExponent)
}

// TestContrastPairsAreLegible checks the one relationship both themes have to
// hold for their header band to be readable: BandInk against Band has to
// contrast the way Ink against Field already does.
func TestContrastPairsAreLegible(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		lighter color.RGBA
		darker  color.RGBA
	}{
		{name: "Night Ink vs Field", lighter: theme.Night.Ink, darker: theme.Night.Field},
		{name: "Night BandInk vs Band", lighter: theme.Night.BandInk, darker: theme.Night.Band},
		{name: "Paper Field vs Ink", lighter: theme.Paper.Field, darker: theme.Paper.Ink},
		{name: "Paper BandInk vs Band", lighter: theme.Paper.BandInk, darker: theme.Paper.Band},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gap := math.Abs(relativeLuminance(testCase.lighter) - relativeLuminance(testCase.darker))
			if gap < minLuminanceGap {
				t.Errorf("relative luminance gap = %.3f, want at least %.1f", gap, minLuminanceGap)
			}
		})
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		text    string
		want    theme.Kind
		wantErr bool
	}{
		{name: "night", text: "night", want: theme.KindNight},
		{name: "paper", text: "paper", want: theme.KindPaper},
		{name: "empty string is rejected", text: "", wantErr: true},
		{
			// --theme is an allow list, not free text, so the exact spelling
			// is what is accepted rather than a case-insensitive match of it.
			name: "wrong case is rejected", text: "Night", wantErr: true,
		},
		{name: "unknown value is rejected", text: "sepia", wantErr: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := theme.Parse(testCase.text)

			if testCase.wantErr {
				if !errors.Is(err, theme.ErrUnknown) {
					t.Fatalf("Parse(%q) error = %v, want ErrUnknown", testCase.text, err)
				}

				if got != theme.KindNight {
					t.Errorf("Parse(%q) = %v on error, want %v", testCase.text, got, theme.KindNight)
				}

				return
			}

			if err != nil {
				t.Fatalf("Parse(%q) = %v, want no error", testCase.text, err)
			}

			if got != testCase.want {
				t.Errorf("Parse(%q) = %v, want %v", testCase.text, got, testCase.want)
			}
		})
	}
}

func TestKindPalette(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		kind theme.Kind
		want theme.Palette
	}{
		{name: "night resolves to Night", kind: theme.KindNight, want: theme.Night},
		{name: "paper resolves to Paper", kind: theme.KindPaper, want: theme.Paper},
		{name: "the zero value resolves to Night", kind: theme.Kind(""), want: theme.Night},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.kind.Palette(); got != testCase.want {
				t.Errorf("%v.Palette() = %v, want %v", testCase.kind, got, testCase.want)
			}
		})
	}
}

func TestKindNext(t *testing.T) {
	t.Parallel()

	t.Run("night moves to paper", func(t *testing.T) {
		t.Parallel()

		if got := theme.KindNight.Next(); got != theme.KindPaper {
			t.Errorf("KindNight.Next() = %v, want %v", got, theme.KindPaper)
		}
	})

	t.Run("paper moves back to night, completing the cycle", func(t *testing.T) {
		t.Parallel()

		if got := theme.KindPaper.Next(); got != theme.KindNight {
			t.Errorf("KindPaper.Next() = %v, want %v", got, theme.KindNight)
		}
	})

	t.Run("the zero value cycles the same way night does", func(t *testing.T) {
		t.Parallel()

		if got := theme.Kind("").Next(); got != theme.KindPaper {
			t.Errorf(`Kind("").Next() = %v, want %v`, got, theme.KindPaper)
		}
	})
}
