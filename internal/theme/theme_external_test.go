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

// Field names, named once so goconst does not flag their repetition across the
// palette field lists and distinctness tables below.
const (
	nameField   = "Field"
	nameInk     = "Ink"
	nameMuted   = "Muted"
	nameAccent  = "Accent"
	nameData    = "Data"
	nameOK      = "OK"
	nameCaution = "Caution"
	nameWarn    = "Warn"
	nameKey     = "Key"
	nameRule    = "Rule"
	nameShore   = "Shore"
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

// fieldAccessor names one field of Palette together with a function that
// reads it, so the tests below can walk the whole struct once instead of
// repeating its field list per test.
type fieldAccessor struct {
	name string
	get  func(theme.Palette) color.RGBA
}

// paletteFields lists every field of Palette, in struct order. It is the one
// place that enumerates the struct, so a field added there and forgotten here
// shows up as a gap in coverage rather than as a silently untested colour.
func paletteFields() []fieldAccessor {
	return []fieldAccessor{
		{name: nameField, get: func(p theme.Palette) color.RGBA { return p.Field }},
		{name: nameInk, get: func(p theme.Palette) color.RGBA { return p.Ink }},
		{name: nameMuted, get: func(p theme.Palette) color.RGBA { return p.Muted }},
		{name: nameAccent, get: func(p theme.Palette) color.RGBA { return p.Accent }},
		{name: nameData, get: func(p theme.Palette) color.RGBA { return p.Data }},
		{name: nameOK, get: func(p theme.Palette) color.RGBA { return p.OK }},
		{name: nameCaution, get: func(p theme.Palette) color.RGBA { return p.Caution }},
		{name: nameWarn, get: func(p theme.Palette) color.RGBA { return p.Warn }},
		{name: nameKey, get: func(p theme.Palette) color.RGBA { return p.Key }},
		{name: nameRule, get: func(p theme.Palette) color.RGBA { return p.Rule }},
		{name: nameShore, get: func(p theme.Palette) color.RGBA { return p.Shore }},
		{name: nameAltLow, get: func(p theme.Palette) color.RGBA { return p.AltLow }},
		{name: nameAltMid, get: func(p theme.Palette) color.RGBA { return p.AltMid }},
		{name: nameAltHigh, get: func(p theme.Palette) color.RGBA { return p.AltHigh }},
		{name: nameBand, get: func(p theme.Palette) color.RGBA { return p.Band }},
		{name: nameBandInk, get: func(p theme.Palette) color.RGBA { return p.BandInk }},
	}
}

// allFields lists every field of a palette, named, so the opacity check below
// can table-drive off either theme without repeating the field list itself.
func allFields(pal theme.Palette) []namedColour {
	fields := paletteFields()
	colours := make([]namedColour, 0, len(fields))

	for _, field := range fields {
		colours = append(colours, namedColour{name: field.name, col: field.get(pal)})
	}

	return colours
}

// namedPalette pairs a palette with the name a failure should report.
type namedPalette struct {
	name string
	pal  theme.Palette
}

// bothPalettes is Night and Day together, for the tests that hold both to the
// same rule.
func bothPalettes() []namedPalette {
	return []namedPalette{
		{name: "Night", pal: theme.Night},
		{name: "Day", pal: theme.Day},
	}
}

// TestPalettesAreFullyOpaque checks every field of both palettes, including
// the six fields (Data, OK, Caution, Warn, Key, Shore) the older Night-only
// opacity test above predates.
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

// hueFields lists the colours a reader has to be able to tell apart at a
// glance: the field and ink that frame the whole picture, the muted chrome,
// and the four status hues plus the one accent that all have to read as
// distinct marks rather than shades of each other.
func hueFields(pal theme.Palette) []namedColour {
	return []namedColour{
		{name: nameField, col: pal.Field},
		{name: nameInk, col: pal.Ink},
		{name: nameMuted, col: pal.Muted},
		{name: nameAccent, col: pal.Accent},
		{name: nameData, col: pal.Data},
		{name: nameOK, col: pal.OK},
		{name: nameCaution, col: pal.Caution},
		{name: nameWarn, col: pal.Warn},
	}
}

// TestSemanticHuesAreDistinct checks the hues whose whole job is to be told
// apart at a glance. Two of the status colours landing on the same value, or
// on the accent, ink, muted chrome or field they sit against, would be
// invisible in the struct literal but would erase a distinction the operator
// relies on to read the panel at a glance.
func TestSemanticHuesAreDistinct(t *testing.T) {
	t.Parallel()

	for _, palette := range bothPalettes() {
		t.Run(palette.name, func(t *testing.T) {
			t.Parallel()

			assertPairwiseDistinct(t, hueFields(palette.pal))
		})
	}
}

// TestKeyIsDistinctFromFieldAndInk checks the softkey box against the two
// colours it sits between. Landing on Field would make the box invisible
// against the frame it is a step off; landing on Ink would make it read as
// more type rather than as hardware under the picture.
func TestKeyIsDistinctFromFieldAndInk(t *testing.T) {
	t.Parallel()

	for _, palette := range bothPalettes() {
		t.Run(palette.name, func(t *testing.T) {
			t.Parallel()

			assertPairwiseDistinct(t, []namedColour{
				{name: nameField, col: palette.pal.Field},
				{name: nameInk, col: palette.pal.Ink},
				{name: nameKey, col: palette.pal.Key},
			})
		})
	}
}

// TestIntentionalColourRepeats asserts the equalities the palette carries by
// design, so that a future edit which splits one of these pairs into its own
// colour fails here instead of silently adding a shade to the picture. See
// the doc comments on AltLow, AltMid and BandInk in theme.go for why each
// pair is meant to match.
func TestIntentionalColourRepeats(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		got  color.RGBA
		want color.RGBA
	}{
		{name: "Night AltLow repeats OK", got: theme.Night.AltLow, want: theme.Night.OK},
		{name: "Night AltMid repeats Ink", got: theme.Night.AltMid, want: theme.Night.Ink},
		{name: "Day AltLow repeats OK", got: theme.Day.AltLow, want: theme.Day.OK},
		{name: "Day AltMid repeats Ink", got: theme.Day.AltMid, want: theme.Day.Ink},
		{
			// Night's band text and its ink are the same white for the same
			// reason AltMid is: text is text, wherever in the picture it sits.
			// Combined with the AltMid case above this ties BandInk, AltMid
			// and Ink to a single white.
			name: "Night BandInk repeats Ink",
			got:  theme.Night.BandInk,
			want: theme.Night.Ink,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if testCase.got != testCase.want {
				// This equality is deliberate; see the doc comment above.
				t.Errorf("%s: got %v, want %v", testCase.name, testCase.got, testCase.want)
			}
		})
	}
}

// TestNightAndDayDiffer walks every field of the two palettes and checks that
// each one differs, except BandInk, which both themes fix at white on
// purpose. This is the test that would catch a copy-paste leaving one theme
// half built from the other's colours.
func TestNightAndDayDiffer(t *testing.T) {
	t.Parallel()

	for _, field := range paletteFields() {
		if field.name == nameBandInk {
			continue
		}

		t.Run(field.name, func(t *testing.T) {
			t.Parallel()

			night := field.get(theme.Night)
			day := field.get(theme.Day)

			if night == day {
				t.Errorf("Night.%s == Day.%s == %v, want them to differ", field.name, field.name, night)
			}
		})
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

	// minLuminanceGap is the floor for the pairs TestContrastPairsAreLegible
	// checks: half of the full relative-luminance range from black to white.
	// That is a real minimum a text-on-background pair has to clear, not a
	// number picked to match what these two palettes happen to measure: their
	// narrowest pair, Day's Field against Ink, sits at about 0.87, so both
	// themes clear 0.5 with more than a third of the whole range to spare.
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
		{name: "Day Field vs Ink", lighter: theme.Day.Field, darker: theme.Day.Ink},
		{name: "Day BandInk vs Band", lighter: theme.Day.BandInk, darker: theme.Day.Band},
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
		{name: "day", text: "day", want: theme.KindDay},
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
		{name: "day resolves to Day", kind: theme.KindDay, want: theme.Day},
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

	t.Run("night moves to day", func(t *testing.T) {
		t.Parallel()

		if got := theme.KindNight.Next(); got != theme.KindDay {
			t.Errorf("KindNight.Next() = %v, want %v", got, theme.KindDay)
		}
	})

	t.Run("day moves back to night, completing the cycle", func(t *testing.T) {
		t.Parallel()

		if got := theme.KindDay.Next(); got != theme.KindNight {
			t.Errorf("KindDay.Next() = %v, want %v", got, theme.KindNight)
		}
	})

	t.Run("the zero value cycles the same way night does", func(t *testing.T) {
		t.Parallel()

		if got := theme.Kind("").Next(); got != theme.KindDay {
			t.Errorf(`Kind("").Next() = %v, want %v`, got, theme.KindDay)
		}
	})
}

// TestPaletteLight checks that Light answers true for exactly one value,
// theme.Day, and false for everything else, including a palette that only
// differs from Day in a single channel. That last case is deliberate, not an
// edge case Light happens to miss: a scene calls Light to decide whether a
// colour it was not handed by the palette needs to be lifted off a light
// field or a dark one, and the only two answers that question ever gets in
// the running program are theme.Night and theme.Day. A palette that is
// merely close to Day is not one of those two, so reading it as dark is the
// same "anything that is not Day reads as night" rule Kind.Palette and
// Kind.Next already apply, and it is what keeps a test-built palette from
// silently drawing as if it were the light theme.
func TestPaletteLight(t *testing.T) {
	t.Parallel()

	almostDay := theme.Day
	almostDay.Rule = color.RGBA{}

	for _, testCase := range []struct {
		name string
		pal  theme.Palette
		want bool
	}{
		{name: "Day is light", pal: theme.Day, want: true},
		{name: "Night is not light", pal: theme.Night, want: false},
		{name: "the zero value is not light", pal: theme.Palette{}, want: false},
		{name: "a palette differing from Day in one channel is not light", pal: almostDay, want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := testCase.pal.Light(); got != testCase.want {
				t.Errorf("%s: Light() = %v, want %v", testCase.name, got, testCase.want)
			}
		})
	}
}
